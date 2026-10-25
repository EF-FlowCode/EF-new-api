package moonshot

import (
	"testing"

	"eflow.local/new-api/common"
	"eflow.local/new-api/dto"
	relaycommon "eflow.local/new-api/relay/common"
	"github.com/stretchr/testify/require"
)

func TestConvertOpenAIRequestKimiK26UsesOnlyAllowedTemperature(t *testing.T) {
	request := &dto.GeneralOpenAIRequest{
		Model:       "mapped-kimi",
		Temperature: common.GetPointer[float64](0.7),
	}
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "kimi-k2.6"},
	}

	converted, err := (&Adaptor{}).ConvertOpenAIRequest(nil, info, request)

	require.NoError(t, err)
	convertedRequest := converted.(*dto.GeneralOpenAIRequest)
	require.Equal(t, 1.0, *convertedRequest.Temperature)
}

func TestConvertOpenAIRequestKimiK26KeepsOmittedTemperatureOmitted(t *testing.T) {
	request := &dto.GeneralOpenAIRequest{Model: "kimi-k2.6"}

	converted, err := (&Adaptor{}).ConvertOpenAIRequest(nil, nil, request)

	require.NoError(t, err)
	require.Nil(t, converted.(*dto.GeneralOpenAIRequest).Temperature)
}

func TestConvertOpenAIRequestOtherMoonshotModelKeepsTemperature(t *testing.T) {
	request := &dto.GeneralOpenAIRequest{
		Model:       "kimi-k2.5",
		Temperature: common.GetPointer[float64](0.7),
	}

	converted, err := (&Adaptor{}).ConvertOpenAIRequest(nil, nil, request)

	require.NoError(t, err)
	require.Equal(t, 0.7, *converted.(*dto.GeneralOpenAIRequest).Temperature)
}
