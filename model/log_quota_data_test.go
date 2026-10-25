package model

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestQuotaDataTokenUsed(t *testing.T) {
	t.Run("uses normalized total when provided", func(t *testing.T) {
		params := RecordConsumeLogParams{
			PromptTokens:     100,
			CompletionTokens: 20,
			QuotaDataTokens:  1350,
		}

		require.Equal(t, 1350, quotaDataTokenUsed(params))
	})

	t.Run("preserves legacy caller fallback", func(t *testing.T) {
		params := RecordConsumeLogParams{
			PromptTokens:     100,
			CompletionTokens: 20,
		}

		require.Equal(t, 120, quotaDataTokenUsed(params))
	})
}
