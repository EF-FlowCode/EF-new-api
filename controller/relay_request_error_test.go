package controller

import (
	"errors"
	"net/http"
	"testing"

	"eflow.local/new-api/common"
	"eflow.local/new-api/relay/helper"
	"eflow.local/new-api/types"
	"github.com/stretchr/testify/require"
)

func TestNewRelayRequestErrorMapsValidationErrorsToBadRequest(t *testing.T) {
	err := newRelayRequestError(helper.NewRequestValidationError(errors.New("model is required")))

	require.Equal(t, http.StatusBadRequest, err.StatusCode)
	require.Equal(t, types.ErrorCodeInvalidRequest, err.GetErrorCode())
	require.True(t, err.IsSkipRetry())
}

func TestNewRelayRequestErrorKeepsInternalErrorsRetryable(t *testing.T) {
	err := newRelayRequestError(errors.New("read failed"))

	require.Equal(t, http.StatusInternalServerError, err.StatusCode)
	require.Equal(t, types.ErrorCodeInvalidRequest, err.GetErrorCode())
	require.False(t, err.IsSkipRetry())
}

func TestNewRelayRequestErrorMapsOversizedBodiesToPayloadTooLarge(t *testing.T) {
	err := newRelayRequestError(common.ErrRequestBodyTooLarge)

	require.Equal(t, http.StatusRequestEntityTooLarge, err.StatusCode)
	require.Equal(t, types.ErrorCodeReadRequestBodyFailed, err.GetErrorCode())
	require.True(t, err.IsSkipRetry())
}
