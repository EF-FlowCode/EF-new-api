package controller

import (
	"testing"

	"eflow.local/new-api/constant"
	"eflow.local/new-api/model"
	"eflow.local/new-api/plugins"
	"github.com/stretchr/testify/require"
)

func TestHappyhorseModelCatalogUsesPlugin(t *testing.T) {
	models := plugins.FactoryModels("happyhorse")
	require.NotEmpty(t, models)
	require.ElementsMatch(t, models, channelId2Models[constant.ChannelTypeAliHappyhorse])
	for _, name := range models {
		require.Contains(t, openAIModelsMap, name)
	}
}

func TestHistoricalVideoReadsPersistedResults(t *testing.T) {
	gemini := &model.Task{TaskID: "old-google", Data: []byte(`{"response":{"generateVideoResponse":{"generatedSamples":[{"video":{"uri":"https://result.example/video.mp4"}}]}}}`)}
	url, err := getGeminiVideoURL(&model.Channel{Type: constant.ChannelTypeGemini}, gemini, "bound-key")
	require.NoError(t, err)
	require.Contains(t, url, "https://result.example/video.mp4")
	require.Contains(t, url, "key=bound-key")
	vertex := &model.Task{TaskID: "old-vertex", PrivateData: model.TaskPrivateData{ResultURL: "https://result.example/vertex.mp4"}}
	url, err = getVertexVideoURL(&model.Channel{Type: constant.ChannelTypeVertexAi}, vertex)
	require.NoError(t, err)
	require.Equal(t, "https://result.example/vertex.mp4", url)
	_, err = getGeminiVideoURL(&model.Channel{}, &model.Task{}, "bound-key")
	require.ErrorContains(t, err, "explicit plugin adoption")
	_, err = getVertexVideoURL(&model.Channel{}, &model.Task{})
	require.ErrorContains(t, err, "explicit plugin adoption")
}
