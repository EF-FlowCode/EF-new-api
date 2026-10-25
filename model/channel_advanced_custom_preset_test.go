package model

import (
	"testing"

	"eflow.local/new-api/constant"
	"eflow.local/new-api/dto"
	"github.com/stretchr/testify/require"
)

func TestAdvancedCustomPresetOnlyForVLLMAndSGLang(t *testing.T) {
	for _, channelType := range []int{constant.ChannelTypeVLLM, constant.ChannelTypeSGLang} {
		preset := AdvancedCustomPreset(channelType)
		require.NotNil(t, preset)
		require.NoError(t, preset.Validate())
		require.True(t, preset.HasRelayRoutes())
		_, ok := preset.ModelListRoute()
		require.True(t, ok)
		// Each call returns a fresh value so callers cannot mutate a shared preset.
		preset.Routes = nil
		require.NotEmpty(t, AdvancedCustomPreset(channelType).Routes)
	}
	for _, channelType := range []int{constant.ChannelTypeCustom, constant.ChannelTypeOpenAI, constant.ChannelTypeTaskPlugin, constant.ChannelTypeAliTTS} {
		require.Nil(t, AdvancedCustomPreset(channelType))
	}
}

func TestVLLMChannelUsesPresetForRoutingAndPricing(t *testing.T) {
	channel := &Channel{
		Id:            7,
		Type:          constant.ChannelTypeVLLM,
		OtherSettings: `{"advanced_custom":{"advanced_routes":[{"incoming_path":"/v1/images/generations","upstream_path":"/img"}]}}`,
	}
	settings := channel.GetOtherSettings()
	require.Equal(t, AdvancedCustomPreset(constant.ChannelTypeVLLM), settings.AdvancedCustom)

	for _, path := range []string{"/v1/chat/completions", "/v1/completions", "/v1/responses", "/v1/embeddings", "/v1/messages"} {
		require.True(t, ChannelSupportsRequestPath(channel, path, "qwen3"), path)
	}
	require.False(t, ChannelSupportsRequestPath(channel, "/v1/images/generations", "qwen3"))
	require.False(t, ChannelSupportsRequestPath(channel, "/v1/rerank", "qwen3"))

	endpoints := getPricingEndpointTypesForAbility(
		AbilityWithChannel{Ability: Ability{ChannelId: channel.Id, Model: "qwen3"}, ChannelType: constant.ChannelTypeVLLM},
		map[int]*Channel{channel.Id: channel},
	)
	require.Contains(t, endpoints, constant.EndpointTypeOpenAI)
	require.Contains(t, endpoints, constant.EndpointTypeOpenAIResponse)
	require.Contains(t, endpoints, constant.EndpointTypeAnthropic)
	require.Contains(t, endpoints, constant.EndpointTypeEmbeddings)
}

func TestAdvancedCustomPresetKeepsOtherSettings(t *testing.T) {
	channel := &Channel{Type: constant.ChannelTypeSGLang, OtherSettings: `{"upstream_model_update_check_enabled":true}`}
	settings := channel.GetOtherSettings()
	require.True(t, settings.UpstreamModelUpdateCheckEnabled)
	require.NotNil(t, settings.AdvancedCustom)
	require.Equal(t, dto.AdvancedCustomConverterNone, settings.AdvancedCustom.Routes[0].Converter)
}
