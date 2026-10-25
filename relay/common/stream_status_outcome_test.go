package common

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestStreamResponseFailed(t *testing.T) {
	var absent *StreamStatus
	require.False(t, absent.ResponseFailed())
	require.True(t, NewStreamStatus().ResponseFailed())
	for _, reason := range []StreamEndReason{StreamEndReasonDone, StreamEndReasonEOF, StreamEndReasonHandlerStop} {
		s := NewStreamStatus()
		s.SetEndReason(reason, nil)
		require.False(t, s.ResponseFailed(), reason)
		s.SetEndReason(StreamEndReasonPanic, errors.New("late worker failure"))
		require.True(t, s.ResponseFailed(), reason)
	}
	for _, reason := range []StreamEndReason{StreamEndReasonTimeout, StreamEndReasonClientGone, StreamEndReasonScannerErr, StreamEndReasonPingFail, StreamEndReasonPanic} {
		s := NewStreamStatus()
		s.SetEndReason(reason, nil)
		require.True(t, s.ResponseFailed(), reason)
	}
	s := NewStreamStatus()
	s.SetEndReason(StreamEndReasonHandlerStop, errors.New("upstream failure"))
	require.True(t, s.ResponseFailed())
	s = NewStreamStatus()
	s.SetEndReason(StreamEndReasonDone, nil)
	s.RecordError("soft parse failure")
	require.True(t, s.ResponseFailed())
}
