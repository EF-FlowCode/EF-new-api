package openai

import (
	"testing"

	"eflow.local/new-api/dto"
	"github.com/stretchr/testify/require"
)

func TestMergeResponsesUsageNormalizesNestedInputDetails(t *testing.T) {
	dst := &dto.Usage{}
	src := &dto.Usage{
		InputTokens:          120,
		OutputTokens:         30,
		PromptCacheHitTokens: 17,
		InputTokensDetails: &dto.InputTokenDetails{
			CachedTokens:         80,
			CachedCreationTokens: 12,
			CacheWriteTokens:     12,
			TextTokens:           28,
			ImageTokens:          4,
			AudioTokens:          2,
			VideoTokens:          3,
		},
		CompletionTokenDetails: dto.OutputTokenDetails{ReasoningTokens: 9},
	}

	mergeResponsesUsage(dst, src)

	require.Equal(t, 120, dst.PromptTokens)
	require.Equal(t, 30, dst.CompletionTokens)
	require.Equal(t, 150, dst.TotalTokens)
	require.Equal(t, 80, dst.PromptTokensDetails.CachedTokens)
	require.Equal(t, 12, dst.PromptTokensDetails.CachedCreationTokens)
	require.Equal(t, 28, dst.PromptTokensDetails.TextTokens)
	require.Equal(t, 4, dst.PromptTokensDetails.ImageTokens)
	require.Equal(t, 2, dst.PromptTokensDetails.AudioTokens)
	require.Equal(t, 3, dst.PromptTokensDetails.VideoTokens)
	require.Equal(t, 9, dst.CompletionTokenDetails.ReasoningTokens)
	require.NotNil(t, dst.InputTokensDetails)
	require.NotSame(t, src.InputTokensDetails, dst.InputTokensDetails)
}

func TestMergeResponsesUsageUsesPromptCacheHitFallback(t *testing.T) {
	dst := &dto.Usage{}
	mergeResponsesUsage(dst, &dto.Usage{InputTokens: 10, OutputTokens: 1, PromptCacheHitTokens: 6})
	require.Equal(t, 6, dst.PromptTokensDetails.CachedTokens)
}
