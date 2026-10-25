package types

import (
	"errors"
	"fmt"
	"net/http"
	"testing"

	"eflow.local/new-api/relaykit/relayconvert/reasoning"
	"github.com/stretchr/testify/require"
)

func TestReasoningClientErrorIsBadRequestWithoutRetry(t *testing.T) {
	err := fmt.Errorf("convert Gemini: %w", reasoning.AsClientError(errors.New("invalid budget")))
	converted := NewError(err, ErrorCodeConvertRequestFailed)
	require.Equal(t, http.StatusBadRequest, converted.StatusCode)
	require.True(t, converted.IsSkipRetry())
	ordinary := NewError(errors.New("adapter failed"), ErrorCodeConvertRequestFailed)
	require.Equal(t, http.StatusInternalServerError, ordinary.StatusCode)
	require.False(t, ordinary.IsSkipRetry())
}
