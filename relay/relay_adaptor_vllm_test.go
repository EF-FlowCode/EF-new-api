package relay

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"eflow.local/new-api/common"
	"eflow.local/new-api/constant"
	"eflow.local/new-api/dto"
	"eflow.local/new-api/model"
	"eflow.local/new-api/relay/channel/advancedcustom"
	relaycommon "eflow.local/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestVLLMAndSGLangRelayThroughRoutePreset(t *testing.T) {
	for _, channelType := range []int{constant.ChannelTypeVLLM, constant.ChannelTypeSGLang} {
		t.Run(constant.GetChannelTypeName(channelType), func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
			// A stored custom route must not override the named preset.
			stored := &model.Channel{
				Type:          channelType,
				OtherSettings: `{"advanced_custom":{"advanced_routes":[{"incoming_path":"/v1/chat/completions","upstream_path":"/elsewhere"}]}}`,
			}
			common.SetContextKey(c, constant.ContextKeyChannelType, channelType)
			common.SetContextKey(c, constant.ContextKeyChannelBaseUrl, "http://inference.internal:8000/")
			common.SetContextKey(c, constant.ContextKeyChannelKey, "sk-local")
			common.SetContextKey(c, constant.ContextKeyOriginalModel, "qwen3")
			common.SetContextKey(c, constant.ContextKeyChannelOtherSetting, stored.GetOtherSettings())

			info := relaycommon.GenRelayInfoOpenAI(c, &dto.GeneralOpenAIRequest{Model: "qwen3"})
			info.InitChannelMeta(c)

			require.True(t, info.SupportStreamOptions)
			adaptor, ok := GetAdaptorForInfo(info).(*advancedcustom.Adaptor)
			require.True(t, ok)
			requestURL, err := adaptor.GetRequestURL(info)
			require.NoError(t, err)
			require.Equal(t, "http://inference.internal:8000/v1/chat/completions", requestURL)

			header := http.Header{}
			require.NoError(t, adaptor.SetupRequestHeader(c, &header, info))
			require.Equal(t, "Bearer sk-local", header.Get("Authorization"))
		})
	}
}
