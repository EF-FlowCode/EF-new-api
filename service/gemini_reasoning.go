package service

import (
	"eflow.local/new-api/dto"
	relaycommon "eflow.local/new-api/relay/common"
	"eflow.local/new-api/relaykit/relayconvert"
	"eflow.local/new-api/relaykit/relayconvert/convmeta"
	"eflow.local/new-api/relaykit/relayconvert/reasoning"
	"eflow.local/new-api/setting/model_setting"
)

// prepareGeminiReasoningMeta bridges the host's model mapping to the upstream
// reasoning state contract. It preserves the billing model and original body.
func prepareGeminiReasoningMeta(info *relaycommon.RelayInfo, meta *convmeta.Values) error {
	if info == nil || model_setting.GetGlobalSettings().PassThroughRequestEnabled ||
		info.ChannelMeta != nil && info.ChannelSetting.PassThroughBodyEnabled {
		return nil
	}
	opts := meta.ConvOptions()
	origin, mapped := meta.OriginModelName, meta.UpstreamModelName
	if opts.ShouldPreserveThinkingSuffix(origin) || opts.ShouldPreserveThinkingSuffix(mapped) {
		return nil
	}
	originBase, originIntent, originFound, err := reasoning.ParseGeminiModelSuffix(origin, opts.Gemini.ThinkingAdapterEnabled)
	if err != nil {
		return reasoning.AsClientError(err)
	}
	mappedBase, mappedIntent, mappedFound, err := reasoning.ParseGeminiModelSuffix(mapped, opts.Gemini.ThinkingAdapterEnabled)
	if err != nil {
		return reasoning.AsClientError(err)
	}
	if opts.ShouldPreserveThinkingSuffix(originBase) || opts.ShouldPreserveThinkingSuffix(mappedBase) {
		return nil
	}
	// An explicit mapped suffix takes precedence over the origin alias.
	if mappedFound {
		meta.ReasoningConversion = reasoning.StateFromIntent(mappedIntent)
	} else if originFound && meta.ReasoningConversion == nil {
		meta.ReasoningConversion = reasoning.StateFromIntent(originIntent)
	}
	if originFound {
		meta.OriginModelName = originBase
	}
	if mappedFound && info.ChannelMeta != nil {
		meta.UpstreamModelName = mappedBase
		info.UpstreamModelName = mappedBase
	}
	info.GeminiReasoningState = meta.ReasoningConversion
	return nil
}

func ApplyGeminiThinking(request *dto.GeminiChatRequest, info *relaycommon.RelayInfo, source ...dto.GeneralOpenAIRequest) error {
	meta := relayConversionMeta(info)
	if err := prepareGeminiReasoningMeta(info, meta); err != nil {
		return err
	}
	if err := relayconvert.ApplyGeminiThinkingConfigChecked(request, meta, source...); err != nil {
		return reasoning.AsClientError(err)
	}
	syncRelayConversionMeta(info, meta)
	return nil
}
