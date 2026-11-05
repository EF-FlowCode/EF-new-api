package common

import (
	"strings"

	"eflow.local/new-api/common"
	"eflow.local/new-api/constant"
	"eflow.local/new-api/dto"
	"eflow.local/new-api/setting/model_setting"
	"github.com/gin-gonic/gin"
)

// eflow: protocol routing.

const (
	ProtocolAttemptNative          = "native"
	ProtocolAttemptChatFallback    = "chat_fallback"
	ProtocolAttemptAdaptorFallback = "adaptor_fallback"
)

// applyProtocolRoute decides how this attempt reaches the upstream when the
// channel has a confirmed protocol set. A native protocol the legacy adaptor
// already forwards unchanged keeps that adaptor; any other native protocol and
// every Chat fallback run through a synthesized Advanced Custom route. A channel
// without Chat falls back to its adaptor's own conversion (Anthropic: Messages).
func (info *RelayInfo) applyProtocolRoute(c *gin.Context, meta *ChannelMeta) {
	info.ProtocolAttemptMode = ""
	if !model_setting.GetGlobalSettings().ProtocolRouting.Enabled ||
		!constant.SupportsProtocolRouting(meta.ChannelType) ||
		!meta.ChannelOtherSettings.DeclaresUpstreamProtocols() {
		return
	}
	requestPath := strings.TrimSpace(strings.Split(info.RequestURLPath, "?")[0])
	if strings.HasPrefix(requestPath, "/pg/") {
		requestPath = "/v1" + strings.TrimPrefix(requestPath, "/pg")
	}
	protocol := constant.RequestPathUpstreamProtocol(requestPath)
	if protocol == "" {
		return
	}

	settings := &meta.ChannelOtherSettings
	passThrough := model_setting.GetGlobalSettings().PassThroughRequestEnabled || meta.ChannelSetting.PassThroughBodyEnabled
	wantFallback := common.GetContextKeyBool(c, constant.ContextKeyProtocolFallback)

	var route dto.AdvancedCustomRoute
	switch {
	case !wantFallback && settings.AllowsUpstreamProtocol(protocol):
		info.ProtocolAttemptMode = ProtocolAttemptNative
		if native, _ := constant.LegacyNativeUpstreamProtocol(meta.ChannelType, protocol); native {
			return
		}
		route = protocolNativeRoute(requestPath, protocol)
	case protocol != constant.UpstreamProtocolChat && settings.AllowsUpstreamProtocol(constant.UpstreamProtocolChat) && !passThrough:
		info.ProtocolAttemptMode = ProtocolAttemptChatFallback
		route = protocolChatFallbackRoute(requestPath, protocol)
	case !wantFallback && !passThrough && settings.AllowsUpstreamProtocol(constant.LegacyConversionUpstreamProtocol(meta.ChannelType)):
		info.ProtocolAttemptMode = ProtocolAttemptAdaptorFallback
		return
	default:
		// Selection never picks such a channel; a pinned channel keeps the
		// legacy adaptor and its error.
		return
	}
	settings.AdvancedCustom = &dto.AdvancedCustomConfig{Routes: []dto.AdvancedCustomRoute{route}}
	meta.SupportStreamOptions = true
}

// ProtocolRouteSynthesized reports whether this attempt uses a synthesized
// Advanced Custom route instead of the channel type's adaptor.
func (info *RelayInfo) ProtocolRouteSynthesized() bool {
	return info != nil && info.ProtocolAttemptMode != "" && info.ChannelMeta != nil &&
		info.ChannelOtherSettings.AdvancedCustom != nil &&
		!constant.SupportsAdvancedCustomRoutes(info.ChannelType)
}

func protocolNativeRoute(requestPath string, protocol string) dto.AdvancedCustomRoute {
	return dto.AdvancedCustomRoute{
		IncomingPath: requestPath,
		UpstreamPath: requestPath,
		Converter:    dto.AdvancedCustomConverterNone,
		Auth:         protocolAuth(protocol),
	}
}

func protocolChatFallbackRoute(requestPath string, protocol string) dto.AdvancedCustomRoute {
	converter := dto.AdvancedCustomConverterOpenAIResponsesToOpenAIChatCompletions
	if protocol == constant.UpstreamProtocolMessages {
		converter = dto.AdvancedCustomConverterAnthropicMessagesToOpenAIChatCompletions
	}
	return dto.AdvancedCustomRoute{
		IncomingPath: requestPath,
		UpstreamPath: "/v1/chat/completions",
		Converter:    converter,
		Auth:         protocolAuth(constant.UpstreamProtocolChat),
	}
}

func protocolAuth(protocol string) *dto.AdvancedCustomRouteAuth {
	if protocol == constant.UpstreamProtocolMessages {
		return &dto.AdvancedCustomRouteAuth{Type: dto.AdvancedCustomAuthTypeHeader, Name: "x-api-key", Value: "{api_key}"}
	}
	return &dto.AdvancedCustomRouteAuth{Type: dto.AdvancedCustomAuthTypeHeader, Name: "Authorization", Value: "Bearer {api_key}"}
}
