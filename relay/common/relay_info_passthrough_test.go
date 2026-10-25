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

func TestInitChannelMetaAppliesAdvancedCustomRoutePassThrough(t *testing.T) {
	gin.SetMode(gin.TestMode)
	advancedCustom := &dto.AdvancedCustomConfig{Routes: []dto.AdvancedCustomRoute{
		{IncomingPath: "/v1/chat/completions", UpstreamPath: "/v1/chat/completions", Models: []string{"native-model"}, PassThroughBodyEnabled: true},
		{IncomingPath: "/v1/chat/completions", UpstreamPath: "/v1/messages", Converter: "openai_chat_completions_to_anthropic_messages"},
	}}
	require.NoError(t, advancedCustom.Validate())

	for _, test := range []struct {
		name           string
		channelType    int
		channelSetting dto.ChannelSettings
		path           string
		model          string
		want           bool
	}{
		{name: "matched native route enables pass-through", channelType: constant.ChannelTypeCustom, path: "/v1/chat/completions", model: "native-model", want: true},
		{name: "playground path matches the same route", channelType: constant.ChannelTypeCustom, path: "/pg/chat/completions", model: "native-model", want: true},
		{name: "converter route keeps conversion", channelType: constant.ChannelTypeCustom, path: "/v1/chat/completions", model: "other-model", want: false},
		{name: "channel-level pass-through still applies", channelType: constant.ChannelTypeCustom, channelSetting: dto.ChannelSettings{PassThroughBodyEnabled: true}, path: "/v1/chat/completions", model: "other-model", want: true},
		{name: "route flag is ignored on other channel types", channelType: constant.ChannelTypeOpenAI, path: "/v1/chat/completions", model: "native-model", want: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest("POST", test.path, nil)
			common.SetContextKey(c, constant.ContextKeyChannelType, test.channelType)
			common.SetContextKey(c, constant.ContextKeyChannelSetting, test.channelSetting)
			common.SetContextKey(c, constant.ContextKeyChannelOtherSetting, dto.ChannelOtherSettings{AdvancedCustom: advancedCustom})
			info := &RelayInfo{RequestURLPath: test.path, OriginModelName: test.model}

			info.InitChannelMeta(c)

			require.Equal(t, test.want, info.ChannelSetting.PassThroughBodyEnabled)
		})
	}
}
