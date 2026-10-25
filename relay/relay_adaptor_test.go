package relay

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"eflow.local/new-api/common"
	"eflow.local/new-api/constant"
	"eflow.local/new-api/dto"
	"eflow.local/new-api/relay/channel/advancedcustom"
	relaycommon "eflow.local/new-api/relay/common"
	relayconstant "eflow.local/new-api/relay/constant"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestGetAdaptorForInfoKeepsLegacyCustomForEmptyAdvancedConfig(t *testing.T) {
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType: constant.ChannelTypeCustom,
			ApiType:     constant.APITypeOpenAI,
			ChannelOtherSettings: dto.ChannelOtherSettings{
				AdvancedCustom: &dto.AdvancedCustomConfig{},
			},
		},
	}

	_, ok := GetAdaptorForInfo(info).(*advancedcustom.Adaptor)
	require.False(t, ok)
}

func TestGetAdaptorForInfoReturnsNilWithoutChannelMeta(t *testing.T) {
	require.Nil(t, GetAdaptorForInfo(&relaycommon.RelayInfo{}))
}

func TestGetAdaptorForInfoKeepsLegacyCustomForManagementOnlyConfig(t *testing.T) {
	info := &relaycommon.RelayInfo{
		RequestURLPath:  "/v1/chat/completions",
		OriginModelName: "gpt-test",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType: constant.ChannelTypeCustom,
			ApiType:     constant.APITypeOpenAI,
			ChannelOtherSettings: dto.ChannelOtherSettings{
				AdvancedCustom: &dto.AdvancedCustomConfig{Routes: []dto.AdvancedCustomRoute{
					{IncomingPath: dto.AdvancedCustomModelListPath, UpstreamPath: "/models"},
					{IncomingPath: dto.AdvancedCustomBalancePath, UpstreamPath: "/balance"},
				}},
			},
		},
	}

	_, ok := GetAdaptorForInfo(info).(*advancedcustom.Adaptor)
	require.False(t, ok)
}

func TestGetAdaptorForInfoKeepsAdvancedAdaptorWhenRelayRouteExists(t *testing.T) {
	info := &relaycommon.RelayInfo{
		RequestURLPath:  "/v1/chat/completions",
		OriginModelName: "gpt-test",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType: constant.ChannelTypeCustom,
			ApiType:     constant.APITypeOpenAI,
			ChannelOtherSettings: dto.ChannelOtherSettings{
				AdvancedCustom: &dto.AdvancedCustomConfig{Routes: []dto.AdvancedCustomRoute{
					{IncomingPath: "/v1/chat/completions", UpstreamPath: "/chat"},
				}},
			},
		},
	}

	_, ok := GetAdaptorForInfo(info).(*advancedcustom.Adaptor)
	require.True(t, ok)
}

func TestAdvancedCustomResponsesCompactUsesConfiguredRoute(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses/compact", nil)
	common.SetContextKey(c, constant.ContextKeyChannelType, constant.ChannelTypeCustom)
	common.SetContextKey(c, constant.ContextKeyChannelBaseUrl, "https://gateway.example")
	common.SetContextKey(c, constant.ContextKeyOriginalModel, "gpt-test")
	common.SetContextKey(c, constant.ContextKeyChannelOtherSetting, dto.ChannelOtherSettings{
		AdvancedCustom: &dto.AdvancedCustomConfig{Routes: []dto.AdvancedCustomRoute{{
			IncomingPath: "/v1/responses/compact",
			UpstreamPath: "/provider/responses/compact",
		}}},
	})

	info := relaycommon.GenRelayInfoResponsesCompaction(c, &dto.OpenAIResponsesCompactionRequest{Model: "gpt-test"})
	info.InitChannelMeta(c)

	require.Equal(t, relayconstant.RelayModeResponsesCompact, info.RelayMode)
	require.Equal(t, constant.APITypeOpenAI, info.ApiType)
	adaptor, ok := GetAdaptorForInfo(info).(*advancedcustom.Adaptor)
	require.True(t, ok)
	requestURL, err := adaptor.GetRequestURL(info)
	require.NoError(t, err)
	require.Equal(t, "https://gateway.example/provider/responses/compact", requestURL)
}
