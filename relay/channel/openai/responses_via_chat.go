package openai

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"eflow.local/new-api/common"
	"eflow.local/new-api/constant"
	"eflow.local/new-api/dto"
	"eflow.local/new-api/logger"
	relaycommon "eflow.local/new-api/relay/common"
	"eflow.local/new-api/relay/helper"
	"eflow.local/new-api/service"
	"eflow.local/new-api/service/openaicompat"
	"eflow.local/new-api/types"

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
	info.ObserveResponseModel(chatResp.Model)

	for _, choice := range chatResp.Choices {
		if choice.FinishReason == constant.FinishReasonContentFilter {
			common.SetContextKey(c, constant.ContextKeyAdminRejectReason, "openai_finish_reason=content_filter")
			break
		}
	}

	// Backfill usage if upstream omitted it (mirrors OpenaiHandler).
	var envelope struct {
		Usage json.RawMessage `json:"usage"`
	}
	_ = common.Unmarshal(body, &envelope)
	if len(envelope.Usage) == 0 || string(envelope.Usage) == "null" {
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
	bodyBytes, usage, err := openaicompat.ChatCompletionsResponseToResponsesResponseWithTools(&chatResp, respID, info.OriginModelName, info.ResponsesTools)
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

// OaiChatToResponsesStreamHandler keeps transport and usage in the host while
// RelayKit owns all item IDs, tool restoration, segmentation and event ordering.
func OaiChatToResponsesStreamHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	if resp == nil || resp.Body == nil {
		return nil, types.NewOpenAIError(fmt.Errorf("invalid response"), types.ErrorCodeBadResponse, http.StatusBadGateway)
	}
	stream, err := service.NewRelayResponseStream(info, types.RelayFormatOpenAI, types.RelayFormatOpenAIResponses, service.RelayResponseStreamOptions{
		ID: "resp_" + common.GetRandomString(24), Model: info.OriginModelName, IncludeUsage: true, EmitSequenceNumber: true,
	})
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponse, http.StatusBadGateway)
	}
	var streamErr *types.NewAPIError
	writeFailed := false
	var usage *dto.Usage
	var usageText strings.Builder
	emit := func(events []*dto.ResponsesStreamResponse) bool {
		for _, event := range events {
			data, err := common.Marshal(event)
			if err == nil {
				helper.SetEventStreamHeaders(c)
				c.Render(-1, common.CustomEvent{Data: fmt.Sprintf("event: %s\ndata: %s\n\n", event.Type, data)})
				err = helper.FlushWriter(c)
			}
			if err != nil {
				writeFailed = true
				streamErr = types.NewOpenAIError(err, types.ErrorCodeBadResponse, http.StatusBadGateway)
				return false
			}
		}
		return true
	}
	helper.StreamScannerHandler(c, resp, info, func(data string, sr *helper.StreamResult) {
		if streamErr != nil {
			sr.Stop(streamErr)
			return
		}
		var chunk dto.ChatCompletionsStreamResponse
		if err := common.UnmarshalJsonStr(data, &chunk); err != nil {
			streamErr = types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusBadGateway)
			sr.Stop(streamErr)
			return
		}
		info.ObserveResponseModel(chunk.Model)
		var errorEnvelope dto.SimpleResponse
		_ = common.UnmarshalJsonStr(data, &errorEnvelope)
		if upstreamErr := dto.GetOpenAIError(errorEnvelope.Error); upstreamErr != nil && upstreamErr.Type != "" {
			streamErr = types.WithOpenAIError(*upstreamErr, http.StatusBadGateway)
			sr.Stop(streamErr)
			return
		}
		// All-zero usage frames (some upstreams send them mid-stream or as the
		// tail) must not displace the text-based estimate below.
		if chunk.Usage != nil && service.ValidUsage(chunk.Usage) {
			usage = chunk.Usage
		}
		for _, choice := range chunk.Choices {
			usageText.WriteString(choice.Delta.GetReasoningContent())
			usageText.WriteString(choice.Delta.GetContentString())
			for _, call := range choice.Delta.ToolCalls {
				usageText.WriteString(call.Function.Arguments)
			}
			if choice.FinishReason != nil && *choice.FinishReason == constant.FinishReasonContentFilter {
				common.SetContextKey(c, constant.ContextKeyAdminRejectReason, "openai_finish_reason=content_filter")
			}
		}
		events, err := service.ConvertRelayStreamChunk[dto.ChatCompletionsStreamResponse, dto.ResponsesStreamResponse](c, info, stream, &chunk)
		if err != nil {
			streamErr = types.NewOpenAIError(err, types.ErrorCodeBadResponse, http.StatusBadGateway)
			sr.Stop(streamErr)
			return
		}
		if !emit(events) {
			sr.Stop(streamErr)
		}
	})
	aborted := streamErr
	if aborted == nil && info.StreamStatus != nil && info.StreamStatus.IsServerSideError() {
		aborted = types.NewOpenAIError(fmt.Errorf("upstream chat stream ended: %s", info.StreamStatus.Summary()), types.ErrorCodeBadResponse, http.StatusBadGateway)
	}
	if aborted != nil {
		// Nothing reached the client yet: return the error so the relay can
		// retry another channel.
		if !info.ClientReceivedOutput(c) {
			return nil, aborted
		}
		// The response is committed and cannot be retried; settle what was
		// delivered instead of refunding it (see service/stream_abort.go).
		logger.LogWarn(c, "responses-via-chat stream interrupted after output, settling delivered part: "+aborted.Error())
		service.MarkRelayResponseStreamIncomplete(stream, "interrupted")
	}
	if usage == nil {
		usage = service.ResponseText2Usage(c, usageText.String(), info.UpstreamModelName, info.GetEstimatePromptTokens())
	}
	if err := service.SetRelayResponseStreamUsage(stream, usage); err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponse, http.StatusBadGateway)
	}
	events, err := service.FinalizeRelayResponseStream[dto.ResponsesStreamResponse](c, info, stream)
	if err != nil {
		if !info.ClientReceivedOutput(c) {
			return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponse, http.StatusBadGateway)
		}
		logger.LogError(c, "responses-via-chat stream finalize failed after output: "+err.Error())
	} else if !writeFailed && !emit(events) && !info.ClientReceivedOutput(c) {
		return nil, streamErr
	}
	if dropped := service.RelayResponseStreamDroppedToolDeltas(stream); dropped > 0 {
		logger.LogWarn(c, fmt.Sprintf("responses-via-chat stream ignored %d tool-call deltas that arrived after their call completed", dropped))
	}
	applyUsagePostProcessing(info, usage, nil)
	return usage, nil
}
