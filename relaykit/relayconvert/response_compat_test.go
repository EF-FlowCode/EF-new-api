package relayconvert

import (
	"testing"

	"eflow.local/new-api/relaykit/dto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUsageFromGeminiMetadataPreservesImageOnlyPromptDetails(t *testing.T) {
	t.Parallel()

	usage := UsageFromGeminiMetadata(&dto.GeminiUsageMetadata{
		PromptTokenCount:        7,
		ToolUsePromptTokenCount: 3,
		PromptTokensDetails: []dto.GeminiPromptTokensDetails{
			{Modality: "IMAGE", TokenCount: 7},
		},
		ToolUsePromptTokensDetails: []dto.GeminiPromptTokensDetails{
			{Modality: "IMAGE", TokenCount: 3},
		},
	}, 0)

	require.NotNil(t, usage)
	assert.Equal(t, 10, usage.PromptTokens)
	assert.Equal(t, 10, usage.PromptTokensDetails.ImageTokens)
	assert.Zero(t, usage.PromptTokensDetails.TextTokens)
	assert.Zero(t, usage.PromptTokensDetails.AudioTokens)
}
