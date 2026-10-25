package service

import (
	"testing"

	"eflow.local/new-api/constant"
	relaycommon "eflow.local/new-api/relay/common"
	"eflow.local/new-api/setting/model_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGeminiUpstreamSupportsJSONSchema covers which upstreams get
// parametersJsonSchema and responseJsonSchema: every channel except one whose
// Gemini API version for the model is v1, and Vertex AI regardless of it.
func TestGeminiUpstreamSupportsJSONSchema(t *testing.T) {
	// The default version settings send gemini-1.0-pro to v1 and everything
	// else to v1beta.
	require.Equal(t, "v1", model_setting.GetGeminiVersionSetting("gemini-1.0-pro"))

	tests := []struct {
		name        string
		channelType int
		model       string
		want        bool
	}{
		{name: "Gemini on the default version", channelType: constant.ChannelTypeGemini, model: "gemini-3.7-flash", want: true},
		{name: "Gemini on v1", channelType: constant.ChannelTypeGemini, model: "gemini-1.0-pro", want: false},
		{name: "Vertex AI", channelType: constant.ChannelTypeVertexAi, model: "gemini-1.0-pro", want: true},
		{name: "other channel on v1", channelType: constant.ChannelTypeOpenAI, model: "gemini-1.0-pro", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ChannelType: tt.channelType}}
			assert.Equal(t, tt.want, relayConversionMeta(info).ConvOptions().Gemini.SupportsJSONSchemaModel(tt.model))
		})
	}
}
