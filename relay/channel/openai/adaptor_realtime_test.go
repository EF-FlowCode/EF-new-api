package openai

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestRealtimeHeaderOnlyUsesBetaForPreviewModels(t *testing.T) {
	tests := []struct {
		model    string
		wantBeta string
	}{
		{model: "gpt-realtime", wantBeta: ""},
		{model: "gpt-4o-realtime-preview", wantBeta: "realtime=v1"},
	}

	for _, test := range tests {
		t.Run(test.model, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodGet, "/v1/realtime", nil)
			header := http.Header{}
			info := &relaycommon.RelayInfo{
				RelayMode: relayconstant.RelayModeRealtime,
				ChannelMeta: &relaycommon.ChannelMeta{
					ChannelType:       constant.ChannelTypeOpenAI,
					ApiKey:            "test-key",
					UpstreamModelName: test.model,
				},
			}

			require.NoError(t, (&Adaptor{}).SetupRequestHeader(c, &header, info))
			require.Equal(t, test.wantBeta, header.Get("openai-beta"))
		})
	}
}
