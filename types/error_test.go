package types

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewAPIErrorSetMessageUpdatesRelayError(t *testing.T) {
	t.Parallel()

	err := WithOpenAIError(OpenAIError{
		Message: "old message",
		Type:    "upstream_error",
		Code:    "rate_limit_exceeded",
	}, 429)

	err.SetMessage("new message")

	require.Equal(t, "new message", err.Error())
	require.Equal(t, "new message", err.ToOpenAIError().Message)
}

func TestNewAPIErrorRetryFlags(t *testing.T) {
	t.Parallel()

	err := NewError(nil, ErrorCodeBadResponse)
	require.False(t, err.IsSkipRetry())
	require.False(t, err.IsForceRetry())

	err.SetSkipRetry(true)
	err.SetForceRetry(true)

	require.True(t, err.IsSkipRetry())
	require.True(t, err.IsForceRetry())
}
