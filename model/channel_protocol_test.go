package model

import (
	"testing"

	"eflow.local/new-api/common"
	"eflow.local/new-api/constant"
	"eflow.local/new-api/dto"
	"eflow.local/new-api/internal/testconfig"
	"eflow.local/new-api/setting/model_setting"
	"github.com/stretchr/testify/require"
)

func protocolTestChannel(t *testing.T, id int, channelType int, priority int64, protocols []string, passThrough bool) *Channel {
	t.Helper()
	settings, err := common.Marshal(dto.ChannelOtherSettings{UpstreamProtocols: protocols})
	require.NoError(t, err)
	setting, err := common.Marshal(dto.ChannelSettings{PassThroughBodyEnabled: passThrough})
	require.NoError(t, err)
	return &Channel{
		Id:            id,
		Type:          channelType,
		Priority:      common.GetPointer(priority),
		OtherSettings: string(settings),
		Setting:       common.GetPointer(string(setting)),
	}
}

func TestChannelRequestPathSupportDeclaredProtocols(t *testing.T) {
	channel := protocolTestChannel(t, 1, constant.ChannelTypeOpenAI, 0, []string{"messages", "chat"}, false)

	require.Equal(t, RequestPathNative, ChannelRequestPathSupport(channel, "/v1/messages", "m"))
	require.Equal(t, RequestPathNative, ChannelRequestPathSupport(channel, "/v1/chat/completions", "m"))
	// responses is not confirmed: the channel serves it only through Chat.
	require.Equal(t, RequestPathFallback, ChannelRequestPathSupport(channel, "/v1/responses", "m"))
}

func TestChannelRequestPathSupportWithoutChatUsesAdaptorConversion(t *testing.T) {
	// A Messages-only Anthropic upstream (e.g. cx2cc) still serves Chat through
	// the Claude adaptor's own conversion; that adaptor never served Responses.
	channel := protocolTestChannel(t, 1, constant.ChannelTypeAnthropic, 0, []string{"messages"}, false)
	require.Equal(t, RequestPathNative, ChannelRequestPathSupport(channel, "/v1/messages", "m"))
	require.Equal(t, RequestPathFallback, ChannelRequestPathSupport(channel, "/v1/chat/completions", "m"))
	require.Equal(t, RequestPathUnsupported, ChannelRequestPathSupport(channel, "/v1/responses", "m"))

	passThrough := protocolTestChannel(t, 1, constant.ChannelTypeAnthropic, 0, []string{"messages"}, true)
	require.Equal(t, RequestPathUnsupported, ChannelRequestPathSupport(passThrough, "/v1/chat/completions", "m"))

	// The OpenAI adaptor converts into Chat, which this channel did not confirm.
	responsesOnly := protocolTestChannel(t, 1, constant.ChannelTypeOpenAI, 0, []string{"responses"}, false)
	require.Equal(t, RequestPathUnsupported, ChannelRequestPathSupport(responsesOnly, "/v1/messages", "m"))
}

func TestChannelRequestPathSupportPassThroughHasNoFallback(t *testing.T) {
	channel := protocolTestChannel(t, 1, constant.ChannelTypeAnthropic, 0, []string{"messages", "chat"}, true)

	require.Equal(t, RequestPathNative, ChannelRequestPathSupport(channel, "/v1/messages", "m"))
	require.Equal(t, RequestPathUnsupported, ChannelRequestPathSupport(channel, "/v1/responses", "m"))
}

func TestChannelRequestPathSupportLegacyOrdering(t *testing.T) {
	openai := protocolTestChannel(t, 1, constant.ChannelTypeOpenAI, 0, nil, false)
	anthropic := protocolTestChannel(t, 2, constant.ChannelTypeAnthropic, 0, nil, false)

	// Undeclared channels keep their accepted paths; conversions order as fallback.
	require.Equal(t, RequestPathFallback, ChannelRequestPathSupport(openai, "/v1/messages", "m"))
	require.Equal(t, RequestPathNative, ChannelRequestPathSupport(openai, "/v1/responses", "m"))
	require.Equal(t, RequestPathNative, ChannelRequestPathSupport(anthropic, "/v1/messages", "m"))
	require.Equal(t, RequestPathFallback, ChannelRequestPathSupport(anthropic, "/v1/chat/completions", "m"))
	require.Equal(t, RequestPathUnsupported, ChannelRequestPathSupport(anthropic, "/v1/responses", "m"))
}

func TestChannelRequestPathSupportOutOfScopeTypeIgnoresDeclaration(t *testing.T) {
	channel := protocolTestChannel(t, 1, constant.ChannelTypeCodex, 0, []string{"chat"}, false)
	require.Equal(t, RequestPathNative, ChannelRequestPathSupport(channel, "/v1/responses", "m"))
}

func TestChannelRequestPathSupportSwitchOffRestoresLegacy(t *testing.T) {
	routing := model_setting.DefaultProtocolRoutingSettings()
	routing.Enabled = false
	testconfig.Set(t, "global", map[string]string{"protocol_routing": testconfig.JSON(t, routing)})

	declared := protocolTestChannel(t, 1, constant.ChannelTypeAnthropic, 0, []string{"messages"}, false)
	// Legacy Anthropic accepts Chat by converting it, even though chat is not declared.
	require.Equal(t, RequestPathNative, ChannelRequestPathSupport(declared, "/v1/chat/completions", "m"))
	require.Equal(t, RequestPathNative, orderingSupport(declared, "/v1/chat/completions", "m"))
}

func TestOrderChannelTiersPutsNativeBeforeFallback(t *testing.T) {
	nativeLow := &Channel{Id: 1, Priority: common.GetPointer[int64](2)}
	fallbackHigh := &Channel{Id: 2, Priority: common.GetPointer[int64](5)}
	nativeHigh := &Channel{Id: 3, Priority: common.GetPointer[int64](5)}

	tiers := orderChannelTiers([]supportedChannel{
		{channel: nativeLow, support: RequestPathNative},
		{channel: fallbackHigh, support: RequestPathFallback},
		{channel: nativeHigh, support: RequestPathNative},
	})
	require.Equal(t, [][]*Channel{{nativeHigh}, {nativeLow}, {fallbackHigh}}, tiers)
}

func TestSelectRetryTierSkipsTriedChannels(t *testing.T) {
	a, b, c, d := &Channel{Id: 1}, &Channel{Id: 2}, &Channel{Id: 3}, &Channel{Id: 4}
	tiers := [][]*Channel{{a, b, c}, {d}}

	// Each retry still moves one tier down.
	selected, ok := selectRetryTier(tiers, 1, map[int]struct{}{1: {}})
	require.True(t, ok)
	require.Equal(t, []*Channel{d}, selected)

	// The clamped last tier is used up: untried higher-tier channels are next.
	selected, ok = selectRetryTier(tiers, 2, map[int]struct{}{1: {}, 4: {}})
	require.True(t, ok)
	require.Equal(t, []*Channel{b, c}, selected)

	_, ok = selectRetryTier(tiers, 2, map[int]struct{}{1: {}, 2: {}, 3: {}, 4: {}})
	require.False(t, ok)
}
