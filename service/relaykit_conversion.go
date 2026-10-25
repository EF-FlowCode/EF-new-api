package service

import (
	"context"
	"errors"
	"fmt"

	"eflow.local/new-api/constant"
	"eflow.local/new-api/dto"
	relaycommon "eflow.local/new-api/relay/common"
	"eflow.local/new-api/relaykit/relayconvert"
	"eflow.local/new-api/relaykit/relayconvert/convmeta"
	relaytypes "eflow.local/new-api/relaykit/types"
	"eflow.local/new-api/setting/model_setting"
	hosttypes "eflow.local/new-api/types"

	"github.com/gin-gonic/gin"
)

type RelayResponseStreamOptions = relayconvert.ResponseStreamOptions

type RelayResponseStream struct {
	state *relayconvert.ResponseStreamState
	meta  *convmeta.Values
}

func NormalizeCacheCreationSplit(totalTokens int, tokens5m int, tokens1h int) (int, int) {
	return relayconvert.NormalizeCacheCreationSplit(totalTokens, tokens5m, tokens1h)
}

func init() {
	relayconvert.SetMediaResolver(relayconvert.MediaResolver{
		GetBase64Data: func(ctx context.Context, source relaytypes.FileSource, reason ...string) (string, string, error) {
			ginCtx, _ := ctx.(*gin.Context)
			return GetBase64Data(ginCtx, source, reason...)
		},
		DecodeBase64FileData: DecodeBase64FileData,
	})
}

func ConvertClaudeRequestToOpenAI(ctx context.Context, info *relaycommon.RelayInfo, request *dto.ClaudeRequest) (*dto.GeneralOpenAIRequest, error) {
	return convertRelayRequest[dto.GeneralOpenAIRequest](ctx, info, relaytypes.RelayFormatOpenAI, request)
}

func ConvertOpenAIRequestToClaude(ctx context.Context, info *relaycommon.RelayInfo, request *dto.GeneralOpenAIRequest) (*dto.ClaudeRequest, error) {
	return convertRelayRequest[dto.ClaudeRequest](ctx, info, relaytypes.RelayFormatClaude, request)
}

func ConvertGeminiRequestToOpenAI(ctx context.Context, info *relaycommon.RelayInfo, request *dto.GeminiChatRequest) (*dto.GeneralOpenAIRequest, error) {
	return convertRelayRequest[dto.GeneralOpenAIRequest](ctx, info, relaytypes.RelayFormatOpenAI, request)
}

func ConvertOpenAIRequestToGemini(ctx context.Context, info *relaycommon.RelayInfo, request *dto.GeneralOpenAIRequest) (*dto.GeminiChatRequest, error) {
	return convertRelayRequest[dto.GeminiChatRequest](ctx, info, relaytypes.RelayFormatGemini, request)
}

// ConvertOpenAIResponsesRequestToGemini adapts an OpenAI Responses request to
// the Gemini generateContent shape for Advanced Custom routes.
func ConvertOpenAIResponsesRequestToGemini(ctx context.Context, info *relaycommon.RelayInfo, request *dto.OpenAIResponsesRequest) (*dto.GeminiChatRequest, error) {
	return convertRelayRequest[dto.GeminiChatRequest](ctx, info, relaytypes.RelayFormatGemini, request)
}

func ConvertClaudeResponseToOpenAI(ctx context.Context, info *relaycommon.RelayInfo, response *dto.ClaudeResponse) (*dto.OpenAITextResponse, error) {
	return convertRelayResponse[dto.OpenAITextResponse](ctx, info, relaytypes.RelayFormatOpenAI, response)
}

func ConvertOpenAIResponseToClaude(ctx context.Context, info *relaycommon.RelayInfo, response *dto.OpenAITextResponse) (*dto.ClaudeResponse, error) {
	return convertRelayResponse[dto.ClaudeResponse](ctx, info, relaytypes.RelayFormatClaude, response)
}

func ConvertGeminiResponseToOpenAI(ctx context.Context, info *relaycommon.RelayInfo, response *dto.GeminiChatResponse) (*dto.OpenAITextResponse, error) {
	return convertRelayResponse[dto.OpenAITextResponse](ctx, info, relaytypes.RelayFormatOpenAI, response)
}

func UsageFromGeminiMetadata(metadata *dto.GeminiUsageMetadata, fallbackPromptTokens int) (*dto.Usage, error) {
	return relayconvert.UsageFromGeminiMetadata(metadata, fallbackPromptTokens), nil
}

// ConvertGeminiResponseToOpenAIResponses adapts a non-stream Gemini response
// to the OpenAI Responses shape for an Advanced Custom route.
func ConvertGeminiResponseToOpenAIResponses(ctx context.Context, info *relaycommon.RelayInfo, response *dto.GeminiChatResponse) (*dto.OpenAIResponsesResponse, error) {
	return convertRelayResponse[dto.OpenAIResponsesResponse](ctx, info, relaytypes.RelayFormatOpenAIResponses, response)
}

func ConvertOpenAIResponseToGemini(ctx context.Context, info *relaycommon.RelayInfo, response *dto.OpenAITextResponse) (*dto.GeminiChatResponse, error) {
	return convertRelayResponse[dto.GeminiChatResponse](ctx, info, relaytypes.RelayFormatGemini, response)
}

func NewRelayResponseStream(info *relaycommon.RelayInfo, from hosttypes.RelayFormat, target hosttypes.RelayFormat, options RelayResponseStreamOptions) (*RelayResponseStream, error) {
	state, err := relayconvert.NewResponseStreamState(from, target, options)
	if err != nil {
		return nil, err
	}
	meta := relayConversionMeta(info)
	if info != nil {
		if info.ClaudeConvertInfo != nil {
			meta.ClaudeConvertInfo = info.ClaudeConvertInfo
		}
		meta.SendResponseCount = info.SendResponseCount
	}
	return &RelayResponseStream{state: state, meta: meta}, nil
}

func ConvertRelayStreamChunk[RelayResponse any, HostResponse any](ctx context.Context, info *relaycommon.RelayInfo, stream *RelayResponseStream, response *RelayResponse) ([]*HostResponse, error) {
	if stream == nil || stream.state == nil {
		return nil, errors.New("relay response stream is required")
	}
	stream.refreshMeta(info)
	results, err := relayconvert.ConvertStreamResponseChunk(ctx, stream.meta, stream.state, response)
	if err != nil {
		return nil, err
	}
	stream.syncMeta(info)
	return relayStreamResults[HostResponse](results)
}

func FinalizeRelayResponseStream[HostResponse any](ctx context.Context, info *relaycommon.RelayInfo, stream *RelayResponseStream) ([]*HostResponse, error) {
	if stream == nil || stream.state == nil {
		return nil, errors.New("relay response stream is required")
	}
	stream.refreshMeta(info)
	results, err := relayconvert.FinalizeStreamResponse(ctx, stream.meta, stream.state)
	if err != nil {
		return nil, err
	}
	stream.syncMeta(info)
	return relayStreamResults[HostResponse](results)
}

func SetRelayResponseStreamUsage(stream *RelayResponseStream, usage *dto.Usage) error {
	if stream == nil || stream.state == nil || usage == nil {
		return nil
	}
	stream.state.SetUsage(usage)
	return nil
}

// RelayResponseStreamDroppedToolDeltas returns the number of late tool-call
// deltas the conversion ignored instead of failing the stream.
func RelayResponseStreamDroppedToolDeltas(stream *RelayResponseStream) int {
	if stream == nil || stream.state == nil {
		return 0
	}
	return stream.state.DroppedToolDeltas()
}

// MarkRelayResponseStreamIncomplete makes finalization report an interrupted
// response (e.g. response.incomplete) instead of a completed one.
func MarkRelayResponseStreamIncomplete(stream *RelayResponseStream, reason string) {
	if stream == nil || stream.state == nil {
		return
	}
	stream.state.MarkIncomplete(reason)
}

func convertRelayRequest[HostResponse any](ctx context.Context, info *relaycommon.RelayInfo, target relaytypes.RelayFormat, request any) (*HostResponse, error) {
	if _, fromResponses := request.(*dto.OpenAIResponsesRequest); fromResponses && info != nil {
		info.ResponsesTools = nil
	}
	meta := relayConversionMeta(info)
	if _, fromGemini := request.(*dto.GeminiChatRequest); fromGemini || target == relaytypes.RelayFormatGemini {
		if err := prepareGeminiReasoningMeta(info, meta); err != nil {
			return nil, err
		}
	}
	result, err := relayconvert.ConvertRequest(ctx, meta, target, request)
	if err != nil {
		return nil, err
	}
	syncRelayConversionMeta(info, meta)
	converted, ok := result.Value.(*HostResponse)
	if !ok {
		return nil, fmt.Errorf("unexpected converted request type %T", result.Value)
	}
	return converted, nil
}

func convertRelayResponse[HostResponse any](ctx context.Context, info *relaycommon.RelayInfo, target relaytypes.RelayFormat, response any) (*HostResponse, error) {
	result, err := relayconvert.ConvertResponse(ctx, relayConversionMeta(info), target, response)
	if err != nil {
		return nil, err
	}
	converted, ok := result.Value.(*HostResponse)
	if !ok {
		return nil, fmt.Errorf("unexpected converted response type %T", result.Value)
	}
	return converted, nil
}

func relayStreamResults[T any](results []relayconvert.ResponseResult) ([]*T, error) {
	converted := make([]*T, 0, len(results))
	for _, result := range results {
		if event, ok := result.Value.(relayconvert.ChatToResponsesStreamEvent); ok {
			payload, ok := any(event.Payload).(T)
			if !ok {
				return nil, fmt.Errorf("unexpected Responses stream payload type %T", event.Payload)
			}
			converted = append(converted, &payload)
			continue
		}
		if value, ok := result.Value.(*T); ok {
			converted = append(converted, value)
			continue
		}
		if value, ok := result.Value.(T); ok {
			converted = append(converted, &value)
			continue
		}
		return nil, fmt.Errorf("unexpected converted stream response type %T", result.Value)
	}
	return converted, nil
}

func (stream *RelayResponseStream) refreshMeta(info *relaycommon.RelayInfo) {
	if stream == nil || info == nil {
		return
	}
	stream.meta.OriginModelName = info.OriginModelName
	stream.meta.IsStream = info.IsStream
	stream.meta.ReasoningEffort = info.ReasoningEffort
	stream.meta.ReasoningConversion = info.GeminiReasoningState
	stream.meta.ResponsesTools = info.ResponsesTools
	stream.meta.EstimatePromptTokens = info.GetEstimatePromptTokens()
	if info.SendResponseCount > stream.meta.SendResponseCount {
		stream.meta.SendResponseCount = info.SendResponseCount
	}
	if info.ChannelMeta != nil {
		stream.meta.UpstreamModelName = info.UpstreamModelName
		stream.meta.ChannelMetaAttached = true
		stream.meta.ChannelID = info.ChannelId
		stream.meta.ChannelType = info.ChannelType
		stream.meta.Options.OpenRouterDialect = info.ChannelType == constant.ChannelTypeOpenRouter
	}
	if info.ClaudeConvertInfo != nil {
		stream.meta.ClaudeConvertInfo = info.ClaudeConvertInfo
	}
}

func (stream *RelayResponseStream) syncMeta(info *relaycommon.RelayInfo) {
	if stream == nil || info == nil {
		return
	}
	info.ReasoningEffort = stream.meta.ReasoningEffort
	info.SendResponseCount = stream.meta.SendResponseCount
	if stream.meta.ClaudeConvertInfo != nil {
		info.ClaudeConvertInfo = stream.meta.ClaudeConvertInfo
	}
}

func relayConversionMeta(info *relaycommon.RelayInfo) *convmeta.Values {
	claudeSettings := model_setting.GetClaudeSettings()
	geminiSettings := model_setting.GetGeminiSettings()
	meta := &convmeta.Values{
		Options: &convmeta.Options{
			Claude: convmeta.ClaudeOptions{
				ThinkingAdapterEnabled:                claudeSettings.ThinkingAdapterEnabled,
				ThinkingAdapterBudgetTokensPercentage: claudeSettings.ThinkingAdapterBudgetTokensPercentage,
				DefaultMaxTokens:                      claudeSettings.GetDefaultMaxTokens,
			},
			Gemini: convmeta.GeminiOptions{
				ThinkingAdapterEnabled:                geminiSettings.ThinkingAdapterEnabled,
				ThinkingAdapterBudgetTokensPercentage: geminiSettings.ThinkingAdapterBudgetTokensPercentage,
				FunctionCallThoughtSignatureEnabled:   geminiSettings.FunctionCallThoughtSignatureEnabled,
				SupportsImagine:                       model_setting.IsGeminiModelSupportImagine,
				SafetySetting:                         model_setting.GetGeminiSafetySetting,
				SupportsJSONSchema: func(modelName string) bool {
					return geminiUpstreamSupportsJSONSchema(info, modelName)
				},
			},
			PreserveThinkingSuffix: model_setting.ShouldPreserveThinkingSuffix,
		},
	}
	if info == nil {
		return meta
	}
	meta.OriginModelName = info.OriginModelName
	meta.IsStream = info.IsStream
	meta.ReasoningEffort = info.ReasoningEffort
	meta.ReasoningConversion = info.GeminiReasoningState
	meta.ResponsesTools = info.ResponsesTools
	meta.EstimatePromptTokens = info.GetEstimatePromptTokens()
	if info.ChannelMeta != nil {
		meta.UpstreamModelName = info.UpstreamModelName
		meta.ChannelMetaAttached = true
		meta.ChannelID = info.ChannelId
		meta.ChannelType = info.ChannelType
		meta.Options.OpenRouterDialect = info.ChannelType == constant.ChannelTypeOpenRouter
	}
	meta.ConversionChain = append(meta.ConversionChain, info.RequestConversionChain...)
	return meta
}

func syncRelayConversionMeta(info *relaycommon.RelayInfo, meta *convmeta.Values) {
	if info == nil || meta == nil {
		return
	}
	info.ResponsesTools = meta.ResponsesTools
	info.ReasoningEffort = meta.ReasoningEffort
	info.RequestConversionChain = append([]hosttypes.RelayFormat(nil), meta.ConversionChain...)
}

func ConvertResponsesRequestToChat(ctx context.Context, info *relaycommon.RelayInfo, request *dto.OpenAIResponsesRequest) (*dto.GeneralOpenAIRequest, error) {
	return convertRelayRequest[dto.GeneralOpenAIRequest](ctx, info, relaytypes.RelayFormatOpenAI, request)
}

// geminiUpstreamSupportsJSONSchema reports whether a Gemini upstream accepts
// parametersJsonSchema and responseJsonSchema: Vertex AI and Gemini API v1beta
// do, Gemini API v1 (set per model in the Gemini version settings) does not.
func geminiUpstreamSupportsJSONSchema(info *relaycommon.RelayInfo, modelName string) bool {
	if info != nil && info.ChannelMeta != nil && info.ChannelType == constant.ChannelTypeVertexAi {
		return true
	}
	return model_setting.GetGeminiVersionSetting(modelName) != "v1"
}
