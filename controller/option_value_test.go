package controller

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOptionValueStringRejectsMissingAndSerializesJSON(t *testing.T) {
	_, err := optionValueString(nil)
	require.ErrorIs(t, err, errOptionValueMissing)

	cases := map[string]any{
		"plain":     "plain",
		"true":      true,
		"1.5":       1.5,
		`{"a":1}`:   map[string]any{"a": 1},
		`["x","y"]`: []any{"x", "y"},
	}
	for want, input := range cases {
		got, err := optionValueString(input)
		require.NoError(t, err)
		require.Equal(t, want, got)
	}
}

func TestIsSensitiveOptionKeyHidesModerationKeys(t *testing.T) {
	for _, key := range []string{"moderation.api_keys", "GitHubClientSecret", "StripeApiSecret", "TurnstileSecretKey", "WorkerValidKey"} {
		require.True(t, isSensitiveOptionKey(key), key)
	}
	for _, key := range []string{"moderation.base_url", "moderation.enabled", "SystemName"} {
		require.False(t, isSensitiveOptionKey(key), key)
	}
}
