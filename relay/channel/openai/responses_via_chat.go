package openai

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/service/openaicompat"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
)

// OaiChatToResponsesHandler reads a non-stream chat/completions response
// from the upstream and re-shapes it into an OpenAI Responses API JSON
// payload (eflow ali-channel reverse bridge).
func OaiChatToResponsesHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	if resp == nil || resp.Body == nil {
		return nil, types.NewOpenAIError(fmt.Errorf("invalid response"), types.ErrorCodeBadResponse, http.StatusInternalServerError)
	}
	defer service.CloseResponseBodyGracefully(resp)

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeReadResponseBodyFailed, http.StatusInternalServerError)
	}
	logger.LogDebug(c, "upstream chat response body: %s", body)

	var chatResp dto.OpenAITextResponse
	if err := common.Unmarshal(body, &chatResp); err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}
	if oaiError := chatResp.GetOpenAIError(); oaiError != nil && oaiError.Type != "" {
		return nil, types.WithOpenAIError(*oaiError, resp.StatusCode)
	}

	for _, choice := range chatResp.Choices {
		if choice.FinishReason == constant.FinishReasonContentFilter {
			common.SetContextKey(c, constant.ContextKeyAdminRejectReason, "openai_finish_reason=content_filter")
			break
		}
	}

	// Backfill usage if upstream omitted it (mirrors OpenaiHandler).
	if chatResp.Usage.PromptTokens == 0 {
		completionTokens := chatResp.Usage.CompletionTokens
		if completionTokens == 0 {
			for _, choice := range chatResp.Choices {
				completionTokens += service.CountTextToken(choice.Message.StringContent()+choice.Message.GetReasoningContent(), info.UpstreamModelName)
			}
		}
		chatResp.Usage = dto.Usage{
			PromptTokens:     info.GetEstimatePromptTokens(),
			CompletionTokens: completionTokens,
			TotalTokens:      info.GetEstimatePromptTokens() + completionTokens,
		}
	}

	respID := "resp_" + common.GetRandomString(24)
	bodyBytes, usage, err := openaicompat.ChatCompletionsResponseToResponsesResponse(&chatResp, respID, info.OriginModelName)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}

	applyUsagePostProcessing(info, usage, body)

	c.Writer.Header().Set("Content-Type", "application/json")
	c.Writer.WriteHeader(http.StatusOK)
	if _, err := c.Writer.Write(bodyBytes); err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponse, http.StatusInternalServerError)
	}

	return usage, nil
}

// chatToResponsesStreamState tracks across-chunk state needed to emit
// well-formed Responses-API SSE events from a chat-completions stream.
type chatToResponsesStreamState struct {
	respID    string
	createdAt int64
	model     string

	outputIndex int

	// reasoning item
	reasoningItemID    string
	reasoningOpened    bool
	reasoningClosed    bool
	reasoningOutputIdx int
	reasoningText      strings.Builder

	// message item
	messageItemID    string
	messageOpened    bool
	messageClosed    bool
	messageOutputIdx int
	messageText      strings.Builder

	// tool calls keyed by chat-side delta index
	toolCallsByIdx map[int]*toolCallStreamState
	toolOrder      []int

	usage *dto.Usage
}

type toolCallStreamState struct {
	itemID      string
	callID      string
	name        string
	args        strings.Builder
	outputIdx   int
	itemEmitted bool
	doneEmitted bool
}

// OaiChatToResponsesStreamHandler reads the chat/completions SSE stream from
// the upstream and re-emits it as the OpenAI Responses API SSE event sequence
// (response.created → output_item.added/delta/done … → response.completed).
//
// This mirrors openai/chat_via_responses.go OaiResponsesToChatStreamHandler
// in the opposite direction.
func OaiChatToResponsesStreamHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	if resp == nil || resp.Body == nil {
		return nil, types.NewOpenAIError(fmt.Errorf("invalid response"), types.ErrorCodeBadResponse, http.StatusInternalServerError)
	}

	state := &chatToResponsesStreamState{
		respID:         "resp_" + common.GetRandomString(24),
		createdAt:      time.Now().Unix(),
		model:          info.OriginModelName,
		toolCallsByIdx: make(map[int]*toolCallStreamState),
		usage:          &dto.Usage{},
	}

	var streamErr *types.NewAPIError
	createdSent := false

	emitCreated := func() bool {
		if createdSent {
			return true
		}
		ev := buildResponsesStreamEvent("response.created", map[string]any{
			"response": state.snapshotResponse(false, ""),
		})
		if err := writeResponsesEvent(c, ev); err != nil {
			streamErr = types.NewOpenAIError(err, types.ErrorCodeBadResponse, http.StatusInternalServerError)
			return false
		}
		createdSent = true
		return true
	}

	helper.StreamScannerHandler(c, resp, info, func(data string, sr *helper.StreamResult) {
		if streamErr != nil {
			sr.Stop(streamErr)
			return
		}

		if !emitCreated() {
			sr.Stop(streamErr)
			return
		}

		var chunk dto.ChatCompletionsStreamResponse
		if err := common.UnmarshalJsonStr(data, &chunk); err != nil {
			logger.LogError(c, "failed to unmarshal chat stream chunk: "+err.Error())
			sr.Error(err)
			return
		}

		// Pick up usage if present (commonly in the last chunk for stream_options.include_usage).
		if chunk.Usage != nil && service.ValidUsage(chunk.Usage) {
			state.usage = chunk.Usage
		}

		if len(chunk.Choices) == 0 {
			return
		}
		choice := chunk.Choices[0]
		delta := choice.Delta

		// 1. reasoning delta — must be emitted before any message events.
		if rs := delta.GetReasoningContent(); rs != "" {
			if !state.openReasoningIfNeeded(c, &streamErr) {
				sr.Stop(streamErr)
				return
			}
			state.reasoningText.WriteString(rs)
			ev := buildResponsesStreamEvent("response.reasoning_summary_text.delta", map[string]any{
				"item_id":       state.reasoningItemID,
				"output_index":  state.reasoningOutputIdx,
				"summary_index": 0,
				"delta":         rs,
			})
			if err := writeResponsesEvent(c, ev); err != nil {
				streamErr = types.NewOpenAIError(err, types.ErrorCodeBadResponse, http.StatusInternalServerError)
				sr.Stop(streamErr)
				return
			}
		}

		// 2. assistant content delta.
		if delta.Content != nil && *delta.Content != "" {
			if !state.closeReasoningIfNeeded(c, &streamErr) {
				sr.Stop(streamErr)
				return
			}
			if !state.openMessageIfNeeded(c, &streamErr) {
				sr.Stop(streamErr)
				return
			}
			text := *delta.Content
			state.messageText.WriteString(text)
			ev := buildResponsesStreamEvent("response.output_text.delta", map[string]any{
				"item_id":       state.messageItemID,
				"output_index":  state.messageOutputIdx,
				"content_index": 0,
				"delta":         text,
			})
			if err := writeResponsesEvent(c, ev); err != nil {
				streamErr = types.NewOpenAIError(err, types.ErrorCodeBadResponse, http.StatusInternalServerError)
				sr.Stop(streamErr)
				return
			}
		}

		// 3. tool call deltas. chat protocol indexes them by `delta.tool_calls[i].index`.
		for _, tc := range delta.ToolCalls {
			idx := 0
			if tc.Index != nil {
				idx = *tc.Index
			}
			tcs, ok := state.toolCallsByIdx[idx]
			if !ok {
				// new tool call — close the message stream first if open
				if !state.closeMessageIfNeeded(c, &streamErr) {
					sr.Stop(streamErr)
					return
				}
				if !state.closeReasoningIfNeeded(c, &streamErr) {
					sr.Stop(streamErr)
					return
				}
				tcs = &toolCallStreamState{
					itemID:    "fc_" + common.GetRandomString(24),
					callID:    tc.ID,
					name:      tc.Function.Name,
					outputIdx: state.outputIndex,
				}
				state.outputIndex++
				state.toolCallsByIdx[idx] = tcs
				state.toolOrder = append(state.toolOrder, idx)
			}
			// upstream may send id / name lazily; capture once available
			if tcs.callID == "" && tc.ID != "" {
				tcs.callID = tc.ID
			}
			if tcs.name == "" && tc.Function.Name != "" {
				tcs.name = tc.Function.Name
			}
			if !tcs.itemEmitted && tcs.name != "" {
				callID := tcs.callID
				if callID == "" {
					callID = tcs.itemID
					tcs.callID = callID
				}
				ev := buildResponsesStreamEvent("response.output_item.added", map[string]any{
					"output_index": tcs.outputIdx,
					"item": map[string]any{
						"type":      "function_call",
						"id":        tcs.itemID,
						"call_id":   callID,
						"name":      tcs.name,
						"arguments": "",
						"status":    "in_progress",
					},
				})
				if err := writeResponsesEvent(c, ev); err != nil {
					streamErr = types.NewOpenAIError(err, types.ErrorCodeBadResponse, http.StatusInternalServerError)
					sr.Stop(streamErr)
					return
				}
				tcs.itemEmitted = true
			}
			if argsDelta := tc.Function.Arguments; argsDelta != "" {
				tcs.args.WriteString(argsDelta)
				if tcs.itemEmitted {
					ev := buildResponsesStreamEvent("response.function_call_arguments.delta", map[string]any{
						"item_id":      tcs.itemID,
						"output_index": tcs.outputIdx,
						"delta":        argsDelta,
					})
					if err := writeResponsesEvent(c, ev); err != nil {
						streamErr = types.NewOpenAIError(err, types.ErrorCodeBadResponse, http.StatusInternalServerError)
						sr.Stop(streamErr)
						return
					}
				}
			}
		}

		// 4. finish_reason — flush close events for whatever's open
		if choice.FinishReason != nil && *choice.FinishReason != "" {
			if !state.closeReasoningIfNeeded(c, &streamErr) ||
				!state.closeMessageIfNeeded(c, &streamErr) ||
				!state.closeAllToolCalls(c, &streamErr) {
				sr.Stop(streamErr)
				return
			}
		}
	})

	if streamErr != nil {
		return nil, streamErr
	}

	// In case the upstream stream ended without a finish_reason chunk, still
	// close anything that's open before sending response.completed.
	state.closeReasoningIfNeeded(c, &streamErr)
	state.closeMessageIfNeeded(c, &streamErr)
	state.closeAllToolCalls(c, &streamErr)
	if streamErr != nil {
		return nil, streamErr
	}

	// Estimate usage if upstream didn't include it.
	if state.usage == nil || state.usage.TotalTokens == 0 {
		text := state.messageText.String()
		if state.reasoningText.Len() > 0 {
			text = state.reasoningText.String() + text
		}
		state.usage = service.ResponseText2Usage(c, text, info.UpstreamModelName, info.GetEstimatePromptTokens())
	}

	finalResponse := state.snapshotResponse(true, "completed")
	completedEv := buildResponsesStreamEvent("response.completed", map[string]any{
		"response": finalResponse,
	})
	if err := writeResponsesEvent(c, completedEv); err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponse, http.StatusInternalServerError)
	}

	applyUsagePostProcessing(info, state.usage, nil)
	return state.usage, nil
}

// --- state helpers ----------------------------------------------------------

func (s *chatToResponsesStreamState) openReasoningIfNeeded(c *gin.Context, errSink **types.NewAPIError) bool {
	if s.reasoningOpened {
		return true
	}
	if s.messageOpened {
		// reasoning must precede message; if message is already open we cannot
		// rewind. Skip emitting a reasoning item to avoid out-of-order events.
		s.reasoningOpened = true
		s.reasoningClosed = true
		return true
	}
	s.reasoningItemID = "rs_" + common.GetRandomString(24)
	s.reasoningOutputIdx = s.outputIndex
	s.outputIndex++

	addItem := buildResponsesStreamEvent("response.output_item.added", map[string]any{
		"output_index": s.reasoningOutputIdx,
		"item": map[string]any{
			"type":    "reasoning",
			"id":      s.reasoningItemID,
			"summary": []any{},
			"status":  "in_progress",
		},
	})
	if err := writeResponsesEvent(c, addItem); err != nil {
		*errSink = types.NewOpenAIError(err, types.ErrorCodeBadResponse, http.StatusInternalServerError)
		return false
	}
	addPart := buildResponsesStreamEvent("response.reasoning_summary_part.added", map[string]any{
		"item_id":       s.reasoningItemID,
		"output_index":  s.reasoningOutputIdx,
		"summary_index": 0,
		"part":          map[string]any{"type": "summary_text", "text": ""},
	})
	if err := writeResponsesEvent(c, addPart); err != nil {
		*errSink = types.NewOpenAIError(err, types.ErrorCodeBadResponse, http.StatusInternalServerError)
		return false
	}
	s.reasoningOpened = true
	return true
}

func (s *chatToResponsesStreamState) closeReasoningIfNeeded(c *gin.Context, errSink **types.NewAPIError) bool {
	if !s.reasoningOpened || s.reasoningClosed {
		return true
	}
	text := s.reasoningText.String()
	donePart := buildResponsesStreamEvent("response.reasoning_summary_text.done", map[string]any{
		"item_id":       s.reasoningItemID,
		"output_index":  s.reasoningOutputIdx,
		"summary_index": 0,
		"text":          text,
	})
	if err := writeResponsesEvent(c, donePart); err != nil {
		*errSink = types.NewOpenAIError(err, types.ErrorCodeBadResponse, http.StatusInternalServerError)
		return false
	}
	donePartObj := buildResponsesStreamEvent("response.reasoning_summary_part.done", map[string]any{
		"item_id":       s.reasoningItemID,
		"output_index":  s.reasoningOutputIdx,
		"summary_index": 0,
		"part":          map[string]any{"type": "summary_text", "text": text},
	})
	if err := writeResponsesEvent(c, donePartObj); err != nil {
		*errSink = types.NewOpenAIError(err, types.ErrorCodeBadResponse, http.StatusInternalServerError)
		return false
	}
	doneItem := buildResponsesStreamEvent("response.output_item.done", map[string]any{
		"output_index": s.reasoningOutputIdx,
		"item": map[string]any{
			"type":   "reasoning",
			"id":     s.reasoningItemID,
			"status": "completed",
			"summary": []map[string]any{
				{"type": "summary_text", "text": text},
			},
		},
	})
	if err := writeResponsesEvent(c, doneItem); err != nil {
		*errSink = types.NewOpenAIError(err, types.ErrorCodeBadResponse, http.StatusInternalServerError)
		return false
	}
	s.reasoningClosed = true
	return true
}

func (s *chatToResponsesStreamState) openMessageIfNeeded(c *gin.Context, errSink **types.NewAPIError) bool {
	if s.messageOpened {
		return true
	}
	s.messageItemID = "msg_" + common.GetRandomString(24)
	s.messageOutputIdx = s.outputIndex
	s.outputIndex++

	addItem := buildResponsesStreamEvent("response.output_item.added", map[string]any{
		"output_index": s.messageOutputIdx,
		"item": map[string]any{
			"type":    "message",
			"id":      s.messageItemID,
			"role":    "assistant",
			"status":  "in_progress",
			"content": []any{},
		},
	})
	if err := writeResponsesEvent(c, addItem); err != nil {
		*errSink = types.NewOpenAIError(err, types.ErrorCodeBadResponse, http.StatusInternalServerError)
		return false
	}
	addPart := buildResponsesStreamEvent("response.content_part.added", map[string]any{
		"item_id":       s.messageItemID,
		"output_index":  s.messageOutputIdx,
		"content_index": 0,
		"part":          map[string]any{"type": "output_text", "text": "", "annotations": []any{}},
	})
	if err := writeResponsesEvent(c, addPart); err != nil {
		*errSink = types.NewOpenAIError(err, types.ErrorCodeBadResponse, http.StatusInternalServerError)
		return false
	}
	s.messageOpened = true
	return true
}

func (s *chatToResponsesStreamState) closeMessageIfNeeded(c *gin.Context, errSink **types.NewAPIError) bool {
	if !s.messageOpened || s.messageClosed {
		return true
	}
	text := s.messageText.String()
	doneText := buildResponsesStreamEvent("response.output_text.done", map[string]any{
		"item_id":       s.messageItemID,
		"output_index":  s.messageOutputIdx,
		"content_index": 0,
		"text":          text,
	})
	if err := writeResponsesEvent(c, doneText); err != nil {
		*errSink = types.NewOpenAIError(err, types.ErrorCodeBadResponse, http.StatusInternalServerError)
		return false
	}
	donePart := buildResponsesStreamEvent("response.content_part.done", map[string]any{
		"item_id":       s.messageItemID,
		"output_index":  s.messageOutputIdx,
		"content_index": 0,
		"part":          map[string]any{"type": "output_text", "text": text, "annotations": []any{}},
	})
	if err := writeResponsesEvent(c, donePart); err != nil {
		*errSink = types.NewOpenAIError(err, types.ErrorCodeBadResponse, http.StatusInternalServerError)
		return false
	}
	doneItem := buildResponsesStreamEvent("response.output_item.done", map[string]any{
		"output_index": s.messageOutputIdx,
		"item": map[string]any{
			"type":   "message",
			"id":     s.messageItemID,
			"role":   "assistant",
			"status": "completed",
			"content": []map[string]any{
				{"type": "output_text", "text": text, "annotations": []any{}},
			},
		},
	})
	if err := writeResponsesEvent(c, doneItem); err != nil {
		*errSink = types.NewOpenAIError(err, types.ErrorCodeBadResponse, http.StatusInternalServerError)
		return false
	}
	s.messageClosed = true
	return true
}

func (s *chatToResponsesStreamState) closeAllToolCalls(c *gin.Context, errSink **types.NewAPIError) bool {
	for _, idx := range s.toolOrder {
		tcs := s.toolCallsByIdx[idx]
		if tcs == nil || tcs.doneEmitted {
			continue
		}
		// safety: emit added if we never got a name (defensive)
		if !tcs.itemEmitted {
			callID := tcs.callID
			if callID == "" {
				callID = tcs.itemID
			}
			added := buildResponsesStreamEvent("response.output_item.added", map[string]any{
				"output_index": tcs.outputIdx,
				"item": map[string]any{
					"type":      "function_call",
					"id":        tcs.itemID,
					"call_id":   callID,
					"name":      tcs.name,
					"arguments": "",
					"status":    "in_progress",
				},
			})
			if err := writeResponsesEvent(c, added); err != nil {
				*errSink = types.NewOpenAIError(err, types.ErrorCodeBadResponse, http.StatusInternalServerError)
				return false
			}
			tcs.itemEmitted = true
		}
		argsStr := tcs.args.String()
		argsDone := buildResponsesStreamEvent("response.function_call_arguments.done", map[string]any{
			"item_id":      tcs.itemID,
			"output_index": tcs.outputIdx,
			"arguments":    argsStr,
		})
		if err := writeResponsesEvent(c, argsDone); err != nil {
			*errSink = types.NewOpenAIError(err, types.ErrorCodeBadResponse, http.StatusInternalServerError)
			return false
		}
		callID := tcs.callID
		if callID == "" {
			callID = tcs.itemID
		}
		doneItem := buildResponsesStreamEvent("response.output_item.done", map[string]any{
			"output_index": tcs.outputIdx,
			"item": map[string]any{
				"type":      "function_call",
				"id":        tcs.itemID,
				"call_id":   callID,
				"name":      tcs.name,
				"arguments": argsStr,
				"status":    "completed",
			},
		})
		if err := writeResponsesEvent(c, doneItem); err != nil {
			*errSink = types.NewOpenAIError(err, types.ErrorCodeBadResponse, http.StatusInternalServerError)
			return false
		}
		tcs.doneEmitted = true
	}
	return true
}

// snapshotResponse builds the `response` envelope for response.created /
// response.completed. Pass final=true at the end to include full output.
func (s *chatToResponsesStreamState) snapshotResponse(final bool, status string) map[string]any {
	envelope := map[string]any{
		"id":         s.respID,
		"object":     "response",
		"created_at": s.createdAt,
		"status":     "in_progress",
		"model":      s.model,
		"output":     []any{},
	}
	if final {
		envelope["status"] = status
		envelope["output"] = s.finalOutputArray()
		if s.usage != nil {
			envelope["usage"] = openaicompat.BuildResponsesUsage(s.usage)
		}
	}
	return envelope
}

func (s *chatToResponsesStreamState) finalOutputArray() []map[string]any {
	out := make([]map[string]any, 0, 3)
	if s.reasoningOpened {
		text := s.reasoningText.String()
		out = append(out, map[string]any{
			"type":   "reasoning",
			"id":     s.reasoningItemID,
			"status": "completed",
			"summary": []map[string]any{
				{"type": "summary_text", "text": text},
			},
		})
	}
	if s.messageOpened {
		text := s.messageText.String()
		out = append(out, map[string]any{
			"type":   "message",
			"id":     s.messageItemID,
			"status": "completed",
			"role":   "assistant",
			"content": []map[string]any{
				{"type": "output_text", "text": text, "annotations": []any{}},
			},
		})
	}
	for _, idx := range s.toolOrder {
		tcs := s.toolCallsByIdx[idx]
		if tcs == nil {
			continue
		}
		callID := tcs.callID
		if callID == "" {
			callID = tcs.itemID
		}
		out = append(out, map[string]any{
			"type":      "function_call",
			"id":        tcs.itemID,
			"call_id":   callID,
			"name":      tcs.name,
			"arguments": tcs.args.String(),
			"status":    "completed",
		})
	}
	return out
}

// --- low-level event writer -------------------------------------------------

type responsesStreamEvent struct {
	Type    string
	Payload map[string]any
}

func buildResponsesStreamEvent(evType string, payload map[string]any) responsesStreamEvent {
	if payload == nil {
		payload = map[string]any{}
	}
	payload["type"] = evType
	return responsesStreamEvent{Type: evType, Payload: payload}
}

// writeResponsesEvent emits an SSE chunk in the OpenAI Responses API format:
//
//	event: <type>
//	data: <json>
//
// followed by a flush. Mirrors helper.ResponseChunkData but takes a raw map
// payload so callers can include fields not on dto.ResponsesStreamResponse
// (e.g. reasoning summary parts, content_part.added).
func writeResponsesEvent(c *gin.Context, ev responsesStreamEvent) error {
	helper.SetEventStreamHeaders(c)
	dataBytes, err := common.Marshal(ev.Payload)
	if err != nil {
		return err
	}
	c.Render(-1, common.CustomEvent{Data: fmt.Sprintf("event: %s\n", ev.Type)})
	c.Render(-1, common.CustomEvent{Data: fmt.Sprintf("data: %s\n\n", string(dataBytes))})
	return helper.FlushWriter(c)
}
