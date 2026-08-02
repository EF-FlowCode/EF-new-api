package service

import (
	"context"
	"errors"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relaydto "github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/convmeta"
	relaytypes "github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/setting/model_setting"
	hosttypes "github.com/QuantumNous/new-api/types"

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
	return convertRelayRequest[relaydto.ClaudeRequest, dto.GeneralOpenAIRequest](ctx, info, relaytypes.RelayFormatOpenAI, request)
}

func ConvertOpenAIRequestToClaude(ctx context.Context, info *relaycommon.RelayInfo, request *dto.GeneralOpenAIRequest) (*dto.ClaudeRequest, error) {
	return convertRelayRequest[relaydto.GeneralOpenAIRequest, dto.ClaudeRequest](ctx, info, relaytypes.RelayFormatClaude, request)
}

func ConvertGeminiRequestToOpenAI(ctx context.Context, info *relaycommon.RelayInfo, request *dto.GeminiChatRequest) (*dto.GeneralOpenAIRequest, error) {
	return convertRelayRequest[relaydto.GeminiChatRequest, dto.GeneralOpenAIRequest](ctx, info, relaytypes.RelayFormatOpenAI, request)
}

func ConvertOpenAIRequestToGemini(ctx context.Context, info *relaycommon.RelayInfo, request *dto.GeneralOpenAIRequest) (*dto.GeminiChatRequest, error) {
	return convertRelayRequest[relaydto.GeneralOpenAIRequest, dto.GeminiChatRequest](ctx, info, relaytypes.RelayFormatGemini, request)
}

func ConvertClaudeResponseToOpenAI(ctx context.Context, info *relaycommon.RelayInfo, response *dto.ClaudeResponse) (*dto.OpenAITextResponse, error) {
	return convertRelayResponse[relaydto.ClaudeResponse, dto.OpenAITextResponse](ctx, info, relaytypes.RelayFormatOpenAI, response)
}

func ConvertOpenAIResponseToClaude(ctx context.Context, info *relaycommon.RelayInfo, response *dto.OpenAITextResponse) (*dto.ClaudeResponse, error) {
	return convertRelayResponse[relaydto.OpenAITextResponse, dto.ClaudeResponse](ctx, info, relaytypes.RelayFormatClaude, response)
}

func ConvertGeminiResponseToOpenAI(ctx context.Context, info *relaycommon.RelayInfo, response *dto.GeminiChatResponse) (*dto.OpenAITextResponse, error) {
	return convertRelayResponse[relaydto.GeminiChatResponse, dto.OpenAITextResponse](ctx, info, relaytypes.RelayFormatOpenAI, response)
}

func ConvertOpenAIResponseToGemini(ctx context.Context, info *relaycommon.RelayInfo, response *dto.OpenAITextResponse) (*dto.GeminiChatResponse, error) {
	return convertRelayResponse[relaydto.OpenAITextResponse, dto.GeminiChatResponse](ctx, info, relaytypes.RelayFormatGemini, response)
}

func NewRelayResponseStream(info *relaycommon.RelayInfo, from hosttypes.RelayFormat, target hosttypes.RelayFormat, options RelayResponseStreamOptions) (*RelayResponseStream, error) {
	state, err := relayconvert.NewResponseStreamState(relaytypes.RelayFormat(from), relaytypes.RelayFormat(target), options)
	if err != nil {
		return nil, err
	}
	meta := relayConversionMeta(info)
	if info != nil {
		if info.ClaudeConvertInfo != nil {
			meta.ClaudeConvertInfo, err = bridgeRelayDTO[convmeta.ClaudeConvertInfo](info.ClaudeConvertInfo)
			if err != nil {
				return nil, err
			}
		}
		meta.SendResponseCount = info.SendResponseCount
	}
	return &RelayResponseStream{state: state, meta: meta}, nil
}

func ConvertRelayStreamChunk[RelayResponse any, HostResponse any](ctx context.Context, info *relaycommon.RelayInfo, stream *RelayResponseStream, response any) ([]*HostResponse, error) {
	if stream == nil || stream.state == nil {
		return nil, errors.New("relay response stream is required")
	}
	relayResponse, err := bridgeRelayDTO[RelayResponse](response)
	if err != nil {
		return nil, err
	}
	if err := stream.refreshMeta(info); err != nil {
		return nil, err
	}
	results, err := relayconvert.ConvertStreamResponseChunk(ctx, stream.meta, stream.state, relayResponse)
	if err != nil {
		return nil, err
	}
	if err := stream.syncMeta(info); err != nil {
		return nil, err
	}
	return bridgeRelayStreamResults[HostResponse](results)
}

func FinalizeRelayResponseStream[HostResponse any](ctx context.Context, info *relaycommon.RelayInfo, stream *RelayResponseStream) ([]*HostResponse, error) {
	if stream == nil || stream.state == nil {
		return nil, errors.New("relay response stream is required")
	}
	if err := stream.refreshMeta(info); err != nil {
		return nil, err
	}
	results, err := relayconvert.FinalizeStreamResponse(ctx, stream.meta, stream.state)
	if err != nil {
		return nil, err
	}
	if err := stream.syncMeta(info); err != nil {
		return nil, err
	}
	return bridgeRelayStreamResults[HostResponse](results)
}

func SetRelayResponseStreamUsage(stream *RelayResponseStream, usage *dto.Usage) error {
	if stream == nil || stream.state == nil || usage == nil {
		return nil
	}
	relayUsage, err := bridgeRelayDTO[relaydto.Usage](usage)
	if err != nil {
		return err
	}
	stream.state.SetUsage(relayUsage)
	return nil
}

func convertRelayRequest[RelayRequest any, HostResponse any](ctx context.Context, info *relaycommon.RelayInfo, target relaytypes.RelayFormat, request any) (*HostResponse, error) {
	relayRequest, err := bridgeRelayDTO[RelayRequest](request)
	if err != nil {
		return nil, err
	}
	meta := relayConversionMeta(info)
	result, err := relayconvert.ConvertRequest(ctx, meta, target, relayRequest)
	if err != nil {
		return nil, err
	}
	syncRelayConversionMeta(info, meta)
	return bridgeRelayDTO[HostResponse](result.Value)
}

func convertRelayResponse[RelayResponse any, HostResponse any](ctx context.Context, info *relaycommon.RelayInfo, target relaytypes.RelayFormat, response any) (*HostResponse, error) {
	relayResponse, err := bridgeRelayDTO[RelayResponse](response)
	if err != nil {
		return nil, err
	}
	result, err := relayconvert.ConvertResponse(ctx, relayConversionMeta(info), target, relayResponse)
	if err != nil {
		return nil, err
	}
	return bridgeRelayDTO[HostResponse](result.Value)
}

func bridgeRelayDTO[T any](value any) (*T, error) {
	data, err := common.Marshal(value)
	if err != nil {
		return nil, err
	}
	var out T
	if err := common.Unmarshal(data, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func bridgeRelayStreamResults[T any](results []relayconvert.ResponseResult) ([]*T, error) {
	converted := make([]*T, 0, len(results))
	for _, result := range results {
		value, err := bridgeRelayDTO[T](result.Value)
		if err != nil {
			return nil, err
		}
		converted = append(converted, value)
	}
	return converted, nil
}

func (stream *RelayResponseStream) refreshMeta(info *relaycommon.RelayInfo) error {
	if stream == nil || info == nil {
		return nil
	}
	stream.meta.OriginModelName = info.OriginModelName
	stream.meta.IsStream = info.IsStream
	stream.meta.ReasoningEffort = info.ReasoningEffort
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
		claudeInfo, err := bridgeRelayDTO[convmeta.ClaudeConvertInfo](info.ClaudeConvertInfo)
		if err != nil {
			return err
		}
		stream.meta.ClaudeConvertInfo = claudeInfo
	}
	return nil
}

func (stream *RelayResponseStream) syncMeta(info *relaycommon.RelayInfo) error {
	if stream == nil || info == nil {
		return nil
	}
	info.ReasoningEffort = stream.meta.ReasoningEffort
	info.SendResponseCount = stream.meta.SendResponseCount
	if stream.meta.ClaudeConvertInfo != nil {
		claudeInfo, err := bridgeRelayDTO[relaycommon.ClaudeConvertInfo](stream.meta.ClaudeConvertInfo)
		if err != nil {
			return err
		}
		info.ClaudeConvertInfo = claudeInfo
	}
	return nil
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
	meta.EstimatePromptTokens = info.GetEstimatePromptTokens()
	if info.ChannelMeta != nil {
		meta.UpstreamModelName = info.UpstreamModelName
		meta.ChannelMetaAttached = true
		meta.ChannelID = info.ChannelId
		meta.ChannelType = info.ChannelType
		meta.Options.OpenRouterDialect = info.ChannelType == constant.ChannelTypeOpenRouter
	}
	for _, format := range info.RequestConversionChain {
		meta.ConversionChain = append(meta.ConversionChain, relaytypes.RelayFormat(format))
	}
	return meta
}

func syncRelayConversionMeta(info *relaycommon.RelayInfo, meta *convmeta.Values) {
	if info == nil || meta == nil {
		return
	}
	info.ReasoningEffort = meta.ReasoningEffort
	info.RequestConversionChain = make([]hosttypes.RelayFormat, len(meta.ConversionChain))
	for i, format := range meta.ConversionChain {
		info.RequestConversionChain[i] = hosttypes.RelayFormat(format)
	}
}
