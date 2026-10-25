package tencent

import (
	"testing"

	"eflow.local/new-api/constant"
	"eflow.local/new-api/relay/channel/openai"
	relaycommon "eflow.local/new-api/relay/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDispatchAdaptorInit(t *testing.T) {
	tests := []struct {
		name        string
		apiKey      string
		baseURL     string
		wantTC3     bool
		wantBaseURL string
	}{
		{"legacy key", "1300000000|AKIDxxxxxxxx|secretxxxxxxxx", constant.GetChannelBaseURL(constant.ChannelTypeTencent), true, constant.GetChannelBaseURL(constant.ChannelTypeTencent)},
		{"tokenhub default", "sk-xxxxxxxxxxxxxxxx", constant.GetChannelBaseURL(constant.ChannelTypeTencent), false, tokenHubBaseURL},
		{"tokenhub custom", "sk-xxxxxxxxxxxxxxxx", "https://proxy.example.com", false, "https://proxy.example.com"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{
				ChannelType:    constant.ChannelTypeTencent,
				ApiKey:         test.apiKey,
				ChannelBaseUrl: test.baseURL,
			}}
			dispatch := &DispatchAdaptor{}
			dispatch.Init(info)

			require.NotNil(t, dispatch.Adaptor)
			if test.wantTC3 {
				assert.IsType(t, &Adaptor{}, dispatch.Adaptor)
			} else {
				assert.IsType(t, &openai.Adaptor{}, dispatch.Adaptor)
			}
			assert.Equal(t, test.wantBaseURL, info.ChannelBaseUrl)
		})
	}
}
