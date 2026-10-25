package zhipu_4v

import (
	"testing"

	relaycommon "eflow.local/new-api/relay/common"
	relayconstant "eflow.local/new-api/relay/constant"
	"eflow.local/new-api/types"
	"github.com/stretchr/testify/require"
)

func TestGetRequestURLSupportsResponsesForStandardBase(t *testing.T) {
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelBaseUrl: "https://open.bigmodel.cn",
		},
		RelayFormat: types.RelayFormatOpenAIResponses,
		RelayMode:   relayconstant.RelayModeResponses,
	}

	url, err := (&Adaptor{}).GetRequestURL(info)
	require.NoError(t, err)
	require.Equal(t, "https://open.bigmodel.cn/api/v1/responses", url)
}

func TestGetRequestURLRejectsResponsesForSpecialPlan(t *testing.T) {
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelBaseUrl: "glm-coding-plan",
		},
		RelayFormat: types.RelayFormatOpenAIResponses,
		RelayMode:   relayconstant.RelayModeResponses,
	}

	_, err := (&Adaptor{}).GetRequestURL(info)
	require.ErrorContains(t, err, "responses")
}
