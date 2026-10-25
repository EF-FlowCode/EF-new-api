package openai

import (
	"fmt"
	"io"
	"net/http"
	"strings"

	"eflow.local/new-api/common"
	"eflow.local/new-api/dto"
	"eflow.local/new-api/logger"
	relaycommon "eflow.local/new-api/relay/common"
	"eflow.local/new-api/relay/helper"
	"eflow.local/new-api/service"
	"eflow.local/new-api/types"

	"github.com/gin-gonic/gin"
)

// mergeResponsesUsage normalizes the Responses API's input_tokens_details
// into the prompt_tokens_details shape consumed by the billing code. Some
// providers put cache and modality counts only in the nested input object.
func mergeResponsesUsage(dst *dto.Usage, src *dto.Usage) {
	if dst == nil || src == nil {
		return
	}
	if src.InputTokens != 0 {
		dst.PromptTokens = src.InputTokens
		dst.InputTokens = src.InputTokens
	}
	if src.OutputTokens != 0 {
		dst.CompletionTokens = src.OutputTokens
		dst.OutputTokens = src.OutputTokens
	}
	if src.TotalTokens != 0 {
		dst.TotalTokens = src.TotalTokens
	}
	if src.PromptCacheHitTokens != 0 {
		dst.PromptCacheHitTokens = src.PromptCacheHitTokens
	}
	if src.PromptTokensDetails.CachedTokens != 0 {
		dst.PromptTokensDetails.CachedTokens = src.PromptTokensDetails.CachedTokens
	}
	if src.PromptTokensDetails.CachedCreationTokens != 0 {
		dst.PromptTokensDetails.CachedCreationTokens = src.PromptTokensDetails.CachedCreationTokens
	}
	if src.PromptTokensDetails.CacheWriteTokens != 0 {
		dst.PromptTokensDetails.CacheWriteTokens = src.PromptTokensDetails.CacheWriteTokens
	}
	if src.PromptTokensDetails.TextTokens != 0 {
		dst.PromptTokensDetails.TextTokens = src.PromptTokensDetails.TextTokens
	}
	if src.PromptTokensDetails.ImageTokens != 0 {
		dst.PromptTokensDetails.ImageTokens = src.PromptTokensDetails.ImageTokens
	}
	if src.PromptTokensDetails.AudioTokens != 0 {
		dst.PromptTokensDetails.AudioTokens = src.PromptTokensDetails.AudioTokens
	}
	if src.PromptTokensDetails.VideoTokens != 0 {
		dst.PromptTokensDetails.VideoTokens = src.PromptTokensDetails.VideoTokens
	}
	if src.CompletionTokenDetails != (dto.OutputTokenDetails{}) {
		dst.CompletionTokenDetails = src.CompletionTokenDetails
	}
	if src.InputTokensDetails != nil {
		details := *src.InputTokensDetails
		dst.InputTokensDetails = &details
		if dst.PromptTokensDetails.CachedTokens == 0 {
			dst.PromptTokensDetails.CachedTokens = details.CachedTokens
		}
		if dst.PromptTokensDetails.CachedCreationTokens == 0 {
			dst.PromptTokensDetails.CachedCreationTokens = details.CachedCreationTokens
		}
		if dst.PromptTokensDetails.CacheWriteTokens == 0 {
			dst.PromptTokensDetails.CacheWriteTokens = details.CacheWriteTokens
		}
		if dst.PromptTokensDetails.TextTokens == 0 {
			dst.PromptTokensDetails.TextTokens = details.TextTokens
		}
		if dst.PromptTokensDetails.ImageTokens == 0 {
			dst.PromptTokensDetails.ImageTokens = details.ImageTokens
		}
		if dst.PromptTokensDetails.AudioTokens == 0 {
			dst.PromptTokensDetails.AudioTokens = details.AudioTokens
		}
		if dst.PromptTokensDetails.VideoTokens == 0 {
			dst.PromptTokensDetails.VideoTokens = details.VideoTokens
		}
	}
	if dst.PromptTokensDetails.CachedTokens == 0 && dst.PromptCacheHitTokens != 0 {
		dst.PromptTokensDetails.CachedTokens = dst.PromptCacheHitTokens
	}
	if dst.TotalTokens == 0 {
		dst.TotalTokens = dst.PromptTokens + dst.CompletionTokens
	}
}

func OaiResponsesHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	defer service.CloseResponseBodyGracefully(resp)

	// read response body
	var responsesResponse dto.OpenAIResponsesResponse
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeReadResponseBodyFailed, http.StatusInternalServerError)
	}
	err = common.Unmarshal(responseBody, &responsesResponse)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}
	if oaiError := responsesResponse.GetOpenAIError(); oaiError != nil && oaiError.Type != "" {
		return nil, types.WithOpenAIError(*oaiError, resp.StatusCode)
	}
	info.ObserveResponseModel(responsesResponse.Model)

	if responsesResponse.HasImageGenerationCall() {
		c.Set("image_generation_call", true)
		c.Set("image_generation_call_quality", responsesResponse.GetQuality())
		c.Set("image_generation_call_size", responsesResponse.GetSize())
	}

	// 写入新的 response body
	service.IOCopyBytesGracefully(c, resp, responseBody)

	// compute usage
	usage := dto.Usage{}
	if responsesResponse.Usage != nil {
		mergeResponsesUsage(&usage, responsesResponse.Usage)
	}
	if info == nil || info.ResponsesUsageInfo == nil || info.ResponsesUsageInfo.BuiltInTools == nil {
		return &usage, nil
	}
	// Bill the built-in tool calls the model actually made (response.Tools only
	// echoes what the request declared).
	for _, output := range responsesResponse.Output {
		countResponsesBuiltInToolCall(info, output.Type)
	}
	return &usage, nil
}

// countResponsesBuiltInToolCall credits one output item to the declared
// built-in tool it belongs to. web_search_call covers both the GA
// "web_search" tool and the legacy "web_search_preview".
func countResponsesBuiltInToolCall(info *relaycommon.RelayInfo, itemType string) {
	if info == nil || info.ResponsesUsageInfo == nil || info.ResponsesUsageInfo.BuiltInTools == nil {
		return
	}
	var declared []string
	switch itemType {
	case dto.BuildInCallWebSearchCall:
		declared = []string{dto.BuildInToolWebSearch, dto.BuildInToolWebSearchPreview}
	case dto.BuildInCallFileSearchCall:
		declared = []string{dto.BuildInToolFileSearch}
	default:
		return
	}
	for _, name := range declared {
		if tool := info.ResponsesUsageInfo.BuiltInTools[name]; tool != nil {
			tool.CallCount++
			return
		}
	}
}

func OaiResponsesStreamHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	if resp == nil || resp.Body == nil {
		logger.LogError(c, "invalid response or response body")
		return nil, types.NewError(fmt.Errorf("invalid response"), types.ErrorCodeBadResponse)
	}

	defer service.CloseResponseBodyGracefully(resp)

	var usage = &dto.Usage{}
	var responseTextBuilder strings.Builder
	var terminal, hasUsage bool
	var streamErr *types.NewAPIError
	fail := func(err error, sr *helper.StreamResult) {
		streamErr = types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusBadGateway)
		sr.Stop(err)
	}

	helper.StreamScannerHandler(c, resp, info, func(data string, sr *helper.StreamResult) {

		var streamResponse dto.ResponsesStreamResponse
		if err := common.UnmarshalJsonStr(data, &streamResponse); err != nil {
			fail(fmt.Errorf("invalid responses stream event: %w", err), sr)
			return
		}
		if streamResponse.Response != nil {
			info.ObserveResponseModel(streamResponse.Response.Model)
		}
		switch streamResponse.Type {
		case "error", "response.error", "response.failed":
			var envelope struct {
				Error *types.OpenAIError `json:"error"`
				types.OpenAIError
			}
			_ = common.UnmarshalJsonStr(data, &envelope)
			upstreamErr := envelope.Error
			if streamResponse.Response != nil {
				if nestedErr := streamResponse.Response.GetOpenAIError(); nestedErr != nil {
					upstreamErr = nestedErr
				}
			}
			if upstreamErr == nil && envelope.Message != "" {
				upstreamErr = &envelope.OpenAIError
			}
			if upstreamErr != nil && upstreamErr.Message != "" {
				streamErr = types.WithOpenAIError(*upstreamErr, http.StatusBadGateway)
				sr.Stop(streamErr)
			} else {
				fail(fmt.Errorf("responses stream reported %s", streamResponse.Type), sr)
			}
			return
		case "":
			fail(fmt.Errorf("responses stream event has no type"), sr)
			return
		case "response.completed", "response.incomplete":
			if streamResponse.Response == nil {
				fail(fmt.Errorf("responses terminal event has no response"), sr)
				return
			}
			if streamResponse.Response.GetOpenAIError() != nil {
				fail(fmt.Errorf("responses terminal event contains an error"), sr)
				return
			}
			if len(streamResponse.Response.Status) > 0 {
				var status string
				if err := common.Unmarshal(streamResponse.Response.Status, &status); err != nil || status != strings.TrimPrefix(streamResponse.Type, "response.") {
					fail(fmt.Errorf("responses terminal status does not match event type"), sr)
					return
				}
			}
			terminal = true
			if streamResponse.Response.Usage != nil {
				hasUsage = true
				mergeResponsesUsage(usage, streamResponse.Response.Usage)
			}
			if responseTextBuilder.Len() == 0 {
				// 部分上游只在终态事件里带输出，没有增量；供省略 usage 时估算输出。
				responseTextBuilder.WriteString(service.ExtractOutputTextFromResponses(streamResponse.Response))
			}
			if streamResponse.Response.HasImageGenerationCall() {
				c.Set("image_generation_call", true)
				c.Set("image_generation_call_quality", streamResponse.Response.GetQuality())
				c.Set("image_generation_call_size", streamResponse.Response.GetSize())
			}
		case "response.output_text.delta", "response.function_call_arguments.delta",
			"response.reasoning_summary_text.delta", "response.reasoning_text.delta", "response.refusal.delta":
			// 这些增量都是上游按输出 token 计费的生成内容，终态省略 usage 时一并用于估算输出。
			responseTextBuilder.WriteString(streamResponse.Delta)
		case dto.ResponsesOutputTypeItemDone:
			// 函数调用处理
			if streamResponse.Item != nil {
				countResponsesBuiltInToolCall(info, streamResponse.Item.Type)
			}
		}
		if err := sendResponsesStreamData(c, streamResponse, data); err != nil {
			if c.Request.Context().Err() != nil {
				sr.Stop(nil)
			} else {
				fail(fmt.Errorf("write responses stream: %w", err), sr)
			}
			return
		}
		info.SendResponseCount++
		if terminal {
			sr.Done()
		}
	})

	if streamErr == nil && !terminal && c.Request.Context().Err() == nil {
		err := fmt.Errorf("responses stream ended without a terminal event (%s)", info.StreamStatus.EndReason)
		info.StreamStatus.RecordError(err.Error())
		streamErr = types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusBadGateway)
	}
	if streamErr != nil {
		streamErr.SetSkipRetry(relaycommon.ResponseOutputWritten(c) || c.Request.Context().Err() != nil)
		return nil, streamErr
	}

	if !hasUsage && usage.CompletionTokens == 0 {
		// 计算输出文本的 token 数量
		tempStr := responseTextBuilder.String()
		if len(tempStr) > 0 {
			// Compatibility fallback for providers omitting terminal usage, or client cancellation.
			completionTokens := service.CountTextToken(tempStr, info.UpstreamModelName)
			usage.CompletionTokens = completionTokens
		}
	}

	if !hasUsage && usage.PromptTokens == 0 && usage.CompletionTokens != 0 {
		usage.PromptTokens = info.GetEstimatePromptTokens()
	}

	usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens

	return usage, nil
}
