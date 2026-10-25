package model

import (
	"testing"

	"eflow.local/new-api/common"
	"eflow.local/new-api/constant"
	"eflow.local/new-api/dto"
	"eflow.local/new-api/internal/testconfig"
	"eflow.local/new-api/setting/model_setting"
	"github.com/stretchr/testify/require"
)

func TestChannelSupportsRequestPathForAdvancedCustom(t *testing.T) {
	settings, err := common.Marshal(dto.ChannelOtherSettings{
		AdvancedCustom: &dto.AdvancedCustomConfig{Routes: []dto.AdvancedCustomRoute{
			{IncomingPath: "/v1/chat/completions", UpstreamPath: "/chat", Models: []string{"gpt-test"}},
		}},
	})
	require.NoError(t, err)
	channel := &Channel{Type: constant.ChannelTypeCustom, OtherSettings: string(settings)}

	require.True(t, ChannelSupportsRequestPath(channel, "/v1/chat/completions", "gpt-test"))
	require.False(t, ChannelSupportsRequestPath(channel, "/v1/responses", "gpt-test"))
	require.False(t, ChannelSupportsRequestPath(channel, "/v1/chat/completions", "other-model"))
}

func TestChannelSupportsRequestPathKeepsLegacyAndManagementOnlyCustom(t *testing.T) {
	for _, config := range []*dto.AdvancedCustomConfig{
		nil,
		{},
		{Routes: []dto.AdvancedCustomRoute{{IncomingPath: dto.AdvancedCustomModelListPath, UpstreamPath: "/models"}}},
	} {
		settings, err := common.Marshal(dto.ChannelOtherSettings{AdvancedCustom: config})
		require.NoError(t, err)
		channel := &Channel{Type: constant.ChannelTypeCustom, OtherSettings: string(settings)}
		require.True(t, ChannelSupportsRequestPath(channel, "/v1/chat/completions", "any-model"))
	}
}

func TestChannelSupportsRequestPathNormalizesPlaygroundPathAndQuery(t *testing.T) {
	settings, err := common.Marshal(dto.ChannelOtherSettings{
		AdvancedCustom: &dto.AdvancedCustomConfig{Routes: []dto.AdvancedCustomRoute{
			{IncomingPath: "/v1/chat/completions", UpstreamPath: "/chat", Models: []string{"gpt-test"}},
		}},
	})
	require.NoError(t, err)
	channel := &Channel{Type: constant.ChannelTypeCustom, OtherSettings: string(settings)}

	require.True(t, ChannelSupportsRequestPath(channel, "/pg/chat/completions?stream=true", "gpt-test"))
	require.False(t, ChannelSupportsRequestPath(channel, "/pg/responses", "gpt-test"))
}

func TestChannelSupportsRequestPathIgnoresAdvancedSettingsOnOtherTypes(t *testing.T) {
	settings, err := common.Marshal(dto.ChannelOtherSettings{
		AdvancedCustom: &dto.AdvancedCustomConfig{Routes: []dto.AdvancedCustomRoute{
			{IncomingPath: "/v1/chat/completions", UpstreamPath: "/chat", Models: []string{"gpt-test"}},
		}},
	})
	require.NoError(t, err)
	channel := &Channel{Type: constant.ChannelTypeOpenAI, OtherSettings: string(settings)}
	require.True(t, ChannelSupportsRequestPath(channel, "/v1/responses", "gpt-test"))
}

func TestChannelSupportsRequestPathUsesOptionalEndpointAllowList(t *testing.T) {
	settings, err := common.Marshal(dto.ChannelOtherSettings{
		SupportedEndpointTypes: []string{"openai", "openai-response"},
	})
	require.NoError(t, err)
	channel := &Channel{Type: constant.ChannelTypeAnthropic, OtherSettings: string(settings)}

	require.True(t, ChannelSupportsRequestPath(channel, "/v1/chat/completions", "shared-model"))
	require.True(t, ChannelSupportsRequestPath(channel, "/v1/responses", "shared-model"))
	require.False(t, ChannelSupportsRequestPath(channel, "/v1/messages", "shared-model"))
}

func TestChannelSupportsRequestPathUsesChannelTypeCapability(t *testing.T) {
	anthropic := &Channel{Type: constant.ChannelTypeAnthropic}
	require.True(t, ChannelSupportsRequestPath(anthropic, "/v1/chat/completions", "shared-model"))
	require.True(t, ChannelSupportsRequestPath(anthropic, "/v1/messages", "shared-model"))
	require.False(t, ChannelSupportsRequestPath(anthropic, "/v1/responses", "shared-model"))
	require.False(t, ChannelSupportsRequestPath(anthropic, "/v1beta/models/shared-model:generateContent", "shared-model"))

	gemini := &Channel{Type: constant.ChannelTypeGemini}
	require.False(t, ChannelSupportsRequestPath(gemini, "/v1/responses", "shared-model"))
	require.True(t, ChannelSupportsRequestPath(gemini, "/v1beta/models/shared-model:generateContent", "shared-model"))

	// Types without a capability entry and ungoverned paths stay allow-all.
	openAI := &Channel{Type: constant.ChannelTypeOpenAI}
	require.True(t, ChannelSupportsRequestPath(openAI, "/v1/responses", "shared-model"))
	require.True(t, ChannelSupportsRequestPath(anthropic, "/v1/audio/speech", "shared-model"))
}

func TestChannelSupportsRequestPathOverrideReplacesTypeCapability(t *testing.T) {
	settings, err := common.Marshal(dto.ChannelOtherSettings{
		SupportedEndpointTypes: []string{"openai-response"},
	})
	require.NoError(t, err)
	channel := &Channel{Type: constant.ChannelTypeOpenAI, OtherSettings: string(settings)}

	require.True(t, ChannelSupportsRequestPath(channel, "/v1/responses", "shared-model"))
	require.False(t, ChannelSupportsRequestPath(channel, "/v1/chat/completions", "shared-model"))
}

func TestChannelSupportsRequestPathFollowsChatToResponsesPolicy(t *testing.T) {
	codex := &Channel{Id: 7, Type: constant.ChannelTypeCodex}
	require.False(t, ChannelSupportsRequestPath(codex, "/v1/chat/completions", "gpt-test"))
	require.True(t, ChannelSupportsRequestPath(codex, "/v1/responses", "gpt-test"))

	testconfig.Set(t, "global", map[string]string{
		"chat_completions_to_responses_policy": testconfig.JSON(t, model_setting.ChatCompletionsToResponsesPolicy{
			Enabled:       true,
			ChannelIDs:    []int{7},
			ModelPatterns: []string{"^gpt-"},
		}),
	})
	require.True(t, ChannelSupportsRequestPath(codex, "/v1/chat/completions", "gpt-test"))
	require.True(t, ChannelSupportsRequestPath(codex, "/v1/messages", "gpt-test"))
	require.False(t, ChannelSupportsRequestPath(codex, "/v1/chat/completions", "claude-test"))
}

func TestChannelSupportsRequestPathKeepsAdvancedCustomRouteSemantics(t *testing.T) {
	settings, err := common.Marshal(dto.ChannelOtherSettings{
		SupportedEndpointTypes: []string{"anthropic"},
		AdvancedCustom: &dto.AdvancedCustomConfig{Routes: []dto.AdvancedCustomRoute{
			{IncomingPath: "/v1/chat/completions", UpstreamPath: "/chat", Models: []string{"shared-model"}},
		}},
	})
	require.NoError(t, err)
	channel := &Channel{Type: constant.ChannelTypeCustom, OtherSettings: string(settings)}

	require.True(t, ChannelSupportsRequestPath(channel, "/v1/chat/completions", "shared-model"))
	require.False(t, ChannelSupportsRequestPath(channel, "/v1/responses", "shared-model"))
}

func TestSelectAbilitiesAtRetryPriorityUsesFilteredPriorityOrder(t *testing.T) {
	high := int64(100)
	low := int64(10)
	abilities := []Ability{
		{ChannelId: 1, Priority: &high},
		{ChannelId: 2, Priority: &low},
		{ChannelId: 3, Priority: &low},
	}

	selected := selectAbilitiesAtRetryPriority(abilities, 0)
	require.Len(t, selected, 1)
	require.Equal(t, 1, selected[0].ChannelId)

	selected = selectAbilitiesAtRetryPriority(abilities, 1)
	require.Len(t, selected, 2)
	require.ElementsMatch(t, []int{2, 3}, []int{selected[0].ChannelId, selected[1].ChannelId})

	selected = selectAbilitiesAtRetryPriority(abilities, 99)
	require.Len(t, selected, 2)
}

func TestGetPricingEndpointTypesForAbilityUsesAdvancedCustomRoutes(t *testing.T) {
	settings, err := common.Marshal(dto.ChannelOtherSettings{AdvancedCustom: &dto.AdvancedCustomConfig{Routes: []dto.AdvancedCustomRoute{
		{IncomingPath: "/v1/messages", UpstreamPath: "/messages", Models: []string{"claude-test"}},
	}}})
	require.NoError(t, err)
	channel := &Channel{Id: 42, Type: constant.ChannelTypeCustom, OtherSettings: string(settings)}
	endpoints := getPricingEndpointTypesForAbility(
		AbilityWithChannel{Ability: Ability{ChannelId: channel.Id, Model: "claude-test"}, ChannelType: constant.ChannelTypeCustom},
		map[int]*Channel{channel.Id: channel},
	)
	require.Equal(t, []constant.EndpointType{constant.EndpointTypeAnthropic}, endpoints)
}

func TestGetPricingEndpointTypesForAbilityKeepsLegacyCustomDefaults(t *testing.T) {
	endpoints := getPricingEndpointTypesForAbility(
		AbilityWithChannel{Ability: Ability{ChannelId: 43, Model: "gpt-test"}, ChannelType: constant.ChannelTypeCustom},
		map[int]*Channel{43: {Id: 43, Type: constant.ChannelTypeCustom}},
	)
	require.Equal(t, []constant.EndpointType{constant.EndpointTypeOpenAI}, endpoints)
}
