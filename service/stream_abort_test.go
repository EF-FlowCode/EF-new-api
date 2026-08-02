package service

import (
	"net/http"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/require"
)

func TestStreamAbortRetryError_NonStream(t *testing.T) {
	info := &relaycommon.RelayInfo{IsStream: false}
	require.Nil(t, StreamAbortRetryError(info))
}

func TestStreamAbortRetryError_NormalEnd(t *testing.T) {
	info := &relaycommon.RelayInfo{IsStream: true}
	info.StreamStatus = relaycommon.NewStreamStatus()
	info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonDone, nil)
	require.Nil(t, StreamAbortRetryError(info))
}

func TestStreamAbortRetryError_ServerErrorZeroChunks(t *testing.T) {
	info := &relaycommon.RelayInfo{IsStream: true, SendResponseCount: 0}
	info.StreamStatus = relaycommon.NewStreamStatus()
	info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonTimeout, nil)
	err := StreamAbortRetryError(info)
	require.NotNil(t, err)
	require.Equal(t, http.StatusServiceUnavailable, err.StatusCode)
}

func TestStreamAbortRetryError_ServerErrorWithChunks(t *testing.T) {
	info := &relaycommon.RelayInfo{IsStream: true, SendResponseCount: 5}
	info.StreamStatus = relaycommon.NewStreamStatus()
	info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonTimeout, nil)
	require.Nil(t, StreamAbortRetryError(info))
}

func TestStreamAbortRetryError_ClientGone(t *testing.T) {
	info := &relaycommon.RelayInfo{IsStream: true, SendResponseCount: 0}
	info.StreamStatus = relaycommon.NewStreamStatus()
	info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonClientGone, nil)
	require.Nil(t, StreamAbortRetryError(info))
}

func TestStreamAbortRetryError_AllServerReasons(t *testing.T) {
	serverReasons := []relaycommon.StreamEndReason{
		relaycommon.StreamEndReasonTimeout,
		relaycommon.StreamEndReasonScannerErr,
		relaycommon.StreamEndReasonPanic,
		relaycommon.StreamEndReasonPingFail,
		relaycommon.StreamEndReasonNone,
	}
	for _, reason := range serverReasons {
		info := &relaycommon.RelayInfo{IsStream: true, SendResponseCount: 0}
		info.StreamStatus = relaycommon.NewStreamStatus()
		info.StreamStatus.SetEndReason(reason, nil)
		err := StreamAbortRetryError(info)
		require.NotNil(t, err, "reason=%s should trigger retry", reason)
		require.Equal(t, http.StatusServiceUnavailable, err.StatusCode, "reason=%s", reason)
	}
}
