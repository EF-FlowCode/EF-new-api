package relay

import (
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"eflow.local/new-api/common"
	"eflow.local/new-api/constant"
	"eflow.local/new-api/dto"
	"eflow.local/new-api/relay/channel"
	relaycommon "eflow.local/new-api/relay/common"
	relayconstant "eflow.local/new-api/relay/constant"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// The capability table in constant/channel_endpoint_capability.go must not
// claim less than an adaptor's static rejections: every endpoint it excludes
// has to be one the adaptor's converter refuses (error or nil request).
func TestChannelTypeRequestEndpointsMatchAdaptorRejections(t *testing.T) {
	governed := []constant.EndpointType{
		constant.EndpointTypeOpenAI,
		constant.EndpointTypeOpenAIResponse,
		constant.EndpointTypeOpenAIResponseCompact,
		constant.EndpointTypeAnthropic,
		constant.EndpointTypeGemini,
		constant.EndpointTypeJinaRerank,
		constant.EndpointTypeImageGeneration,
		constant.EndpointTypeEmbeddings,
	}
	checked := 0
	for channelType := 1; channelType <= 1004; channelType++ {
		endpoints, ok := constant.ChannelTypeRequestEndpoints(channelType)
		if !ok {
			continue
		}
		checked++
		apiType, ok := common.ChannelType2APIType(channelType)
		require.True(t, ok, "channel type %d has no api type", channelType)
		adaptor := GetAdaptor(apiType)
		require.NotNil(t, adaptor, "channel type %d has no adaptor", channelType)
		for _, endpoint := range governed {
			if slices.Contains(endpoints, endpoint) {
				continue
			}
			converted, err := convertForEndpoint(t, adaptor, channelType, apiType, endpoint)
			require.True(t, err != nil || converted == nil,
				"channel type %d: capability table excludes %s but the adaptor converts it", channelType, endpoint)
		}
	}
	require.Greater(t, checked, 0)
}

func convertForEndpoint(t *testing.T, adaptor channel.Adaptor, channelType, apiType int, endpoint constant.EndpointType) (any, error) {
	t.Helper()
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/", nil)
	info := &relaycommon.RelayInfo{
		OriginModelName: "capability-test",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:       channelType,
			ApiType:           apiType,
			UpstreamModelName: "capability-test",
		},
	}
	adaptor.Init(info)
	switch endpoint {
	case constant.EndpointTypeOpenAI:
		return adaptor.ConvertOpenAIRequest(c, info, &dto.GeneralOpenAIRequest{Model: "capability-test"})
	case constant.EndpointTypeOpenAIResponse, constant.EndpointTypeOpenAIResponseCompact:
		return adaptor.ConvertOpenAIResponsesRequest(c, info, dto.OpenAIResponsesRequest{Model: "capability-test"})
	case constant.EndpointTypeAnthropic:
		return adaptor.ConvertClaudeRequest(c, info, &dto.ClaudeRequest{Model: "capability-test"})
	case constant.EndpointTypeGemini:
		return adaptor.ConvertGeminiRequest(c, info, &dto.GeminiChatRequest{})
	case constant.EndpointTypeJinaRerank:
		return adaptor.ConvertRerankRequest(c, relayconstant.RelayModeRerank, dto.RerankRequest{Model: "capability-test"})
	case constant.EndpointTypeImageGeneration:
		return adaptor.ConvertImageRequest(c, info, dto.ImageRequest{Model: "capability-test"})
	case constant.EndpointTypeEmbeddings:
		return adaptor.ConvertEmbeddingRequest(c, info, dto.EmbeddingRequest{Model: "capability-test"})
	default:
		t.Fatalf("unhandled endpoint %s", endpoint)
		return nil, nil
	}
}
