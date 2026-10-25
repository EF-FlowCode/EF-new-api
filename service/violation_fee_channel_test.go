package service

import (
	"errors"
	"net/http"
	"testing"

	"eflow.local/new-api/constant"
	relaycommon "eflow.local/new-api/relay/common"
	"eflow.local/new-api/types"
	"github.com/stretchr/testify/require"
)

func TestViolationFeeOnlyAppliesToXaiChannels(t *testing.T) {
	for _, message := range []string{"Content violates usage guidelines", "Failed check: SAFETY_CHECK_TYPE_CSAM"} {
		upstream := types.NewOpenAIError(errors.New(message), types.ErrorCodeBadResponse, http.StatusBadRequest)

		xai := NormalizeViolationFeeError(upstream, constant.ChannelTypeXai)
		require.Equal(t, types.ErrorCodeViolationFeeGrokCSAM, xai.GetErrorCode(), message)
		require.True(t, shouldChargeViolationFee(xai, constant.ChannelTypeXai), message)

		// Same wording from other providers is neither rewritten nor charged.
		other := NormalizeViolationFeeError(upstream, constant.ChannelTypeOpenAI)
		require.NotEqual(t, types.ErrorCodeViolationFeeGrokCSAM, other.GetErrorCode(), message)
		require.False(t, shouldChargeViolationFee(other, constant.ChannelTypeOpenAI), message)
	}

	// A violation_fee code passed through by a non-xAI upstream is not charged either.
	forwarded := WrapAsViolationFeeGrokCSAM(types.NewOpenAIError(errors.New("x"), types.ErrorCodeBadResponse, http.StatusBadRequest))
	require.False(t, shouldChargeViolationFee(forwarded, constant.ChannelTypeOpenAI))

	// Failures before any channel was selected carry no channel metadata.
	require.Zero(t, relayChannelType(&relaycommon.RelayInfo{}))
	require.Zero(t, relayChannelType(nil))
}
