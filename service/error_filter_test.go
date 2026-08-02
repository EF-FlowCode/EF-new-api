package service

import (
	"testing"

	"github.com/QuantumNous/new-api/dto"
	"github.com/stretchr/testify/require"
)

func TestMatchErrorFilter(t *testing.T) {
	t.Parallel()

	t.Run("match status code only", func(t *testing.T) {
		t.Parallel()

		rules := []dto.ErrorFilterRule{
			{
				StatusCodes: []int{429, 503},
				Action:      "retry",
			},
		}

		matched, rule := MatchErrorFilter(rules, 429, "", "")
		require.True(t, matched)
		require.NotNil(t, rule)
		require.Equal(t, "retry", rule.Action)
	})

	t.Run("all non empty conditions use and", func(t *testing.T) {
		t.Parallel()

		rules := []dto.ErrorFilterRule{
			{
				StatusCodes:     []int{429},
				MessageContains: []string{"rate limit", "quota"},
				ErrorCodes:      []string{"rate_limit_exceeded"},
				Action:          "rewrite",
			},
		}

		matched, rule := MatchErrorFilter(
			rules,
			429,
			"rate_limit_exceeded",
			"Request failed because of RATE LIMIT pressure",
		)
		require.True(t, matched)
		require.NotNil(t, rule)

		matched, rule = MatchErrorFilter(
			rules,
			503,
			"rate_limit_exceeded",
			"Request failed because of RATE LIMIT pressure",
		)
		require.False(t, matched)
		require.Nil(t, rule)
	})

	t.Run("same condition group uses or", func(t *testing.T) {
		t.Parallel()

		rules := []dto.ErrorFilterRule{
			{
				MessageContains: []string{"rate limit", "temporarily unavailable"},
				Action:          "rewrite",
			},
		}

		matched, rule := MatchErrorFilter(rules, 503, "", "Service is temporarily unavailable")
		require.True(t, matched)
		require.NotNil(t, rule)
	})

	t.Run("returns first matched rule", func(t *testing.T) {
		t.Parallel()

		rules := []dto.ErrorFilterRule{
			{
				StatusCodes: []int{503},
				Action:      "retry",
			},
			{
				StatusCodes: []int{503},
				Action:      "replace",
			},
		}

		matched, rule := MatchErrorFilter(rules, 503, "", "")
		require.True(t, matched)
		require.NotNil(t, rule)
		require.Equal(t, "retry", rule.Action)
	})

	t.Run("empty rules do not match all errors", func(t *testing.T) {
		t.Parallel()

		rules := []dto.ErrorFilterRule{
			{
				Action: "retry",
			},
		}

		matched, rule := MatchErrorFilter(rules, 500, "internal_error", "boom")
		require.False(t, matched)
		require.Nil(t, rule)
	})

	t.Run("error code match is case insensitive", func(t *testing.T) {
		t.Parallel()

		rules := []dto.ErrorFilterRule{
			{
				ErrorCodes: []string{"RATE_LIMIT_EXCEEDED"},
				Action:     "rewrite",
			},
		}

		matched, rule := MatchErrorFilter(rules, 429, "rate_limit_exceeded", "")
		require.True(t, matched)
		require.NotNil(t, rule)
	})
}
