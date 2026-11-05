package common

import (
	"net/http/httptest"
	"testing"

	"eflow.local/new-api/common"
	"eflow.local/new-api/constant"
	"eflow.local/new-api/dto"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func protocolRouteInfo(t *testing.T, path string, channelType int, protocols []string, passThrough bool, fallback bool) (*RelayInfo, *ChannelMeta) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	common.SetContextKey(c, constant.ContextKeyProtocolFallback, fallback)
	info := &RelayInfo{RequestURLPath: path}
	meta := &ChannelMeta{
		ChannelType:          channelType,
		ChannelSetting:       dto.ChannelSettings{PassThroughBodyEnabled: passThrough},
		ChannelOtherSettings: dto.ChannelOtherSettings{UpstreamProtocols: protocols},
	}
	info.applyProtocolRoute(c, meta)
	info.ChannelMeta = meta
	return info, meta
}

func TestApplyProtocolRouteNativeUsesSynthesizedRouteWhenLegacyConverts(t *testing.T) {
	info, meta := protocolRouteInfo(t, "/v1/messages", constant.ChannelTypeOpenAI, []string{"messages", "chat"}, false, false)

	require.Equal(t, ProtocolAttemptNative, info.ProtocolAttemptMode)
	require.True(t, info.ProtocolRouteSynthesized())
	route := meta.ChannelOtherSettings.AdvancedCustom.Routes[0]
	require.Equal(t, "/v1/messages", route.UpstreamPath)
	require.Equal(t, dto.AdvancedCustomConverterNone, route.Converter)
	require.Equal(t, "x-api-key", route.Auth.Name)
}

func TestApplyProtocolRouteNativeKeepsLegacyAdaptorWhenAlreadyNative(t *testing.T) {
	info, meta := protocolRouteInfo(t, "/v1/responses", constant.ChannelTypeOpenAI, []string{"responses"}, false, false)

	require.Equal(t, ProtocolAttemptNative, info.ProtocolAttemptMode)
	require.False(t, info.ProtocolRouteSynthesized())
	require.Nil(t, meta.ChannelOtherSettings.AdvancedCustom)
}

func TestApplyProtocolRouteChatFallback(t *testing.T) {
	// responses is not confirmed: Chat fallback even on the first attempt.
	info, meta := protocolRouteInfo(t, "/v1/responses", constant.ChannelTypeAnthropic, []string{"messages", "chat"}, false, false)
	require.Equal(t, ProtocolAttemptChatFallback, info.ProtocolAttemptMode)
	route := meta.ChannelOtherSettings.AdvancedCustom.Routes[0]
	require.Equal(t, "/v1/chat/completions", route.UpstreamPath)
	require.Equal(t, dto.AdvancedCustomConverterOpenAIResponsesToOpenAIChatCompletions, route.Converter)
	require.Equal(t, "Authorization", route.Auth.Name)

	// A requested fallback after a native failure converts Messages to Chat.
	info, meta = protocolRouteInfo(t, "/v1/messages", constant.ChannelTypeAnthropic, []string{"messages", "chat"}, false, true)
	require.Equal(t, ProtocolAttemptChatFallback, info.ProtocolAttemptMode)
	require.Equal(t, dto.AdvancedCustomConverterAnthropicMessagesToOpenAIChatCompletions, meta.ChannelOtherSettings.AdvancedCustom.Routes[0].Converter)
}

func TestApplyProtocolRouteNoFallbackWithoutChatOrWithPassThrough(t *testing.T) {
	info, _ := protocolRouteInfo(t, "/v1/messages", constant.ChannelTypeAnthropic, []string{"messages"}, false, true)
	require.Empty(t, info.ProtocolAttemptMode)

	info, _ = protocolRouteInfo(t, "/v1/responses", constant.ChannelTypeAnthropic, []string{"messages", "chat"}, true, false)
	require.Empty(t, info.ProtocolAttemptMode)
}

func TestApplyProtocolRouteAdaptorFallbackWithoutChat(t *testing.T) {
	info, meta := protocolRouteInfo(t, "/v1/chat/completions", constant.ChannelTypeAnthropic, []string{"messages"}, false, false)
	require.Equal(t, ProtocolAttemptAdaptorFallback, info.ProtocolAttemptMode)
	require.False(t, info.ProtocolRouteSynthesized())
	require.Nil(t, meta.ChannelOtherSettings.AdvancedCustom)

	info, _ = protocolRouteInfo(t, "/v1/chat/completions", constant.ChannelTypeAnthropic, []string{"messages"}, true, false)
	require.Empty(t, info.ProtocolAttemptMode)

	info, _ = protocolRouteInfo(t, "/v1/messages", constant.ChannelTypeOpenAI, []string{"responses"}, false, false)
	require.Empty(t, info.ProtocolAttemptMode)
}

func TestApplyProtocolRouteIgnoresUndeclaredAndOutOfScope(t *testing.T) {
	info, meta := protocolRouteInfo(t, "/v1/messages", constant.ChannelTypeOpenAI, nil, false, false)
	require.Empty(t, info.ProtocolAttemptMode)
	require.Nil(t, meta.ChannelOtherSettings.AdvancedCustom)

	info, _ = protocolRouteInfo(t, "/v1/responses", constant.ChannelTypeCodex, []string{"chat"}, false, false)
	require.Empty(t, info.ProtocolAttemptMode)

	info, _ = protocolRouteInfo(t, "/v1/embeddings", constant.ChannelTypeOpenAI, []string{"chat"}, false, false)
	require.Empty(t, info.ProtocolAttemptMode)
}
