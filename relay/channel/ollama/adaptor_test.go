package ollama

import (
	"testing"

	"eflow.local/new-api/dto"
	relaycommon "eflow.local/new-api/relay/common"
	relayconstant "eflow.local/new-api/relay/constant"
	"github.com/stretchr/testify/require"
)

func TestGetRequestURLSupportsStandardResponses(t *testing.T) {
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelBaseUrl: "http://localhost:11434",
		},
		RelayMode: relayconstant.RelayModeResponses,
	}

	url, err := (&Adaptor{}).GetRequestURL(info)
	require.NoError(t, err)
	require.Equal(t, "http://localhost:11434/v1/responses", url)
}

func TestGetRequestURLRejectsResponsesCompact(t *testing.T) {
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelBaseUrl: "http://localhost:11434",
		},
		RelayMode: relayconstant.RelayModeResponsesCompact,
	}

	_, err := (&Adaptor{}).GetRequestURL(info)
	require.ErrorContains(t, err, "compact")
}

func TestConvertOpenAIResponsesRequestUsesOpenAICompatiblePayload(t *testing.T) {
	info := &relaycommon.RelayInfo{}
	request := dto.OpenAIResponsesRequest{Model: "test-model"}

	converted, err := (&Adaptor{}).ConvertOpenAIResponsesRequest(nil, info, request)
	require.NoError(t, err)
	require.Equal(t, request, converted)
}
