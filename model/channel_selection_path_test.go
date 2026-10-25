package model

import (
	"testing"

	"eflow.local/new-api/common"
	"eflow.local/new-api/constant"
	"eflow.local/new-api/dto"
	"github.com/stretchr/testify/require"
)

func TestGetChannelForRequestFiltersPathBeforePrioritySelection(t *testing.T) {
	db := useModelIntegrationTestDB(t, &Channel{}, &Ability{})

	highSettings, err := common.Marshal(dto.ChannelOtherSettings{
		AdvancedCustom: &dto.AdvancedCustomConfig{Routes: []dto.AdvancedCustomRoute{
			{IncomingPath: "/v1/responses", UpstreamPath: "/responses", Models: []string{"path-model"}},
		}},
	})
	require.NoError(t, err)

	lowSettings, err := common.Marshal(dto.ChannelOtherSettings{
		AdvancedCustom: &dto.AdvancedCustomConfig{Routes: []dto.AdvancedCustomRoute{
			{IncomingPath: "/v1/chat/completions", UpstreamPath: "/chat", Models: []string{"path-model"}},
		}},
	})
	require.NoError(t, err)

	highPriority := int64(100)
	lowPriority := int64(10)
	highWeight := uint(100)
	lowWeight := uint(100)
	high := Channel{
		Type:          constant.ChannelTypeCustom,
		Key:           "high-key",
		Status:        common.ChannelStatusEnabled,
		Name:          "high-path",
		Models:        "path-model",
		Group:         "path-group",
		Priority:      &highPriority,
		Weight:        &highWeight,
		OtherSettings: string(highSettings),
	}
	low := Channel{
		Type:          constant.ChannelTypeCustom,
		Key:           "low-key",
		Status:        common.ChannelStatusEnabled,
		Name:          "low-path",
		Models:        "path-model",
		Group:         "path-group",
		Priority:      &lowPriority,
		Weight:        &lowWeight,
		OtherSettings: string(lowSettings),
	}
	require.NoError(t, db.Create(&high).Error)
	require.NoError(t, db.Create(&low).Error)
	require.NoError(t, db.Create(&Ability{Group: "path-group", Model: "path-model", ChannelId: high.Id, Enabled: true, Priority: &highPriority, Weight: highWeight}).Error)
	require.NoError(t, db.Create(&Ability{Group: "path-group", Model: "path-model", ChannelId: low.Id, Enabled: true, Priority: &lowPriority, Weight: lowWeight}).Error)

	selected, err := GetChannelForRequest("path-group", "path-model", 0, "/v1/chat/completions")
	require.NoError(t, err)
	require.NotNil(t, selected)
	require.Equal(t, low.Id, selected.Id)

	selected, err = GetChannelForRequest("path-group", "path-model", -1, "/v1/chat/completions")
	require.NoError(t, err)
	require.NotNil(t, selected)
	require.Equal(t, low.Id, selected.Id)

	selected, err = GetChannelForRequest("path-group", "path-model", 0, "/v1/embeddings")
	require.NoError(t, err)
	require.Nil(t, selected)
}

func TestGetChannelForRequestFiltersConfiguredStandardEndpoints(t *testing.T) {
	db := useModelIntegrationTestDB(t, &Channel{}, &Ability{})

	responseSettings, err := common.Marshal(dto.ChannelOtherSettings{
		SupportedEndpointTypes: []string{"openai", "openai-response"},
	})
	require.NoError(t, err)
	messagesSettings, err := common.Marshal(dto.ChannelOtherSettings{
		SupportedEndpointTypes: []string{"openai", "anthropic"},
	})
	require.NoError(t, err)

	highPriority := int64(100)
	lowPriority := int64(10)
	weight := uint(100)
	responseChannel := Channel{
		Type:          constant.ChannelTypeOpenAI,
		Key:           "response-key",
		Status:        common.ChannelStatusEnabled,
		Name:          "response-channel",
		Models:        "shared-model",
		Group:         "standard-path-group",
		Priority:      &lowPriority,
		Weight:        &weight,
		OtherSettings: string(responseSettings),
	}
	messagesChannel := Channel{
		Type:          constant.ChannelTypeAnthropic,
		Key:           "messages-key",
		Status:        common.ChannelStatusEnabled,
		Name:          "messages-channel",
		Models:        "shared-model",
		Group:         "standard-path-group",
		Priority:      &highPriority,
		Weight:        &weight,
		OtherSettings: string(messagesSettings),
	}
	require.NoError(t, db.Create(&responseChannel).Error)
	require.NoError(t, db.Create(&messagesChannel).Error)
	require.NoError(t, db.Create(&Ability{Group: "standard-path-group", Model: "shared-model", ChannelId: responseChannel.Id, Enabled: true, Priority: &lowPriority, Weight: weight}).Error)
	require.NoError(t, db.Create(&Ability{Group: "standard-path-group", Model: "shared-model", ChannelId: messagesChannel.Id, Enabled: true, Priority: &highPriority, Weight: weight}).Error)

	selected, err := GetChannelForRequest("standard-path-group", "shared-model", 0, "/v1/responses")
	require.NoError(t, err)
	require.NotNil(t, selected)
	require.Equal(t, responseChannel.Id, selected.Id)

	selected, err = GetChannelForRequest("standard-path-group", "shared-model", 0, "/v1/messages")
	require.NoError(t, err)
	require.NotNil(t, selected)
	require.Equal(t, messagesChannel.Id, selected.Id)
}
