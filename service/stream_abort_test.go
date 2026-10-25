package service

import (
	"net/http"
	"net/http/httptest"
	"testing"

	relaycommon "eflow.local/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestStreamAbortRetryError_NonStream(t *testing.T) {
	info := &relaycommon.RelayInfo{IsStream: false}
	require.Nil(t, StreamAbortRetryError(nil, info))
}

func TestStreamAbortRetryError_NormalEnd(t *testing.T) {
	info := &relaycommon.RelayInfo{IsStream: true}
	info.StreamStatus = relaycommon.NewStreamStatus()
	info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonDone, nil)
	require.Nil(t, StreamAbortRetryError(nil, info))
}

func TestStreamAbortRetryError_ServerErrorZeroChunks(t *testing.T) {
	info := &relaycommon.RelayInfo{IsStream: true, SendResponseCount: 0}
	info.StreamStatus = relaycommon.NewStreamStatus()
	info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonTimeout, nil)
	err := StreamAbortRetryError(nil, info)
	require.NotNil(t, err)
	require.Equal(t, http.StatusServiceUnavailable, err.StatusCode)
}

func TestStreamAbortRetryError_ServerErrorWithChunks(t *testing.T) {
	info := &relaycommon.RelayInfo{IsStream: true, SendResponseCount: 5}
	info.StreamStatus = relaycommon.NewStreamStatus()
	info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonTimeout, nil)
	require.Nil(t, StreamAbortRetryError(nil, info))
}

func TestStreamAbortRetryError_ClientGone(t *testing.T) {
	info := &relaycommon.RelayInfo{IsStream: true, SendResponseCount: 0}
	info.StreamStatus = relaycommon.NewStreamStatus()
	info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonClientGone, nil)
	require.Nil(t, StreamAbortRetryError(nil, info))
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
		err := StreamAbortRetryError(nil, info)
		require.NotNil(t, err, "reason=%s should trigger retry", reason)
		require.Equal(t, http.StatusServiceUnavailable, err.StatusCode, "reason=%s", reason)
	}
}

func TestStreamAbortRetryError_CommittedWriterWithoutCountIsDelivered(t *testing.T) {
	// The Claude passthrough writes SSE without bumping SendResponseCount; a
	// mid-stream reset there used to refund the whole delivered answer.
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Writer.WriteHeader(http.StatusOK)
	c.Writer.WriteHeaderNow()

	info := &relaycommon.RelayInfo{IsStream: true, SendResponseCount: 0}
	info.StreamStatus = relaycommon.NewStreamStatus()
	info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonScannerErr, nil)
	require.Nil(t, StreamAbortRetryError(c, info))

	fresh, _ := gin.CreateTestContext(httptest.NewRecorder())
	require.NotNil(t, StreamAbortRetryError(fresh, info))
}
