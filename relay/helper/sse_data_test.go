package helper

import (
	"strings"
	"testing"

	"eflow.local/new-api/constant"
	relaycommon "eflow.local/new-api/relay/common"
	"github.com/stretchr/testify/require"
)

func TestStreamScannerMultilineData(t *testing.T) {
	for _, end := range []string{"\n\n", "\n\ndata: [DONE]\n", "\n[DONE]\n", ""} {
		t.Run(end, func(t *testing.T) {
			c, resp, info := setupStreamTest(t, strings.NewReader("event: message\r\ndata: {\r\n: heartbeat\r\ndata: \"value\": 42\r\ndata: }"+end))
			info.DisablePing = true
			var got []string
			StreamScannerHandler(c, resp, info, func(data string, sr *StreamResult) { got = append(got, data) })
			require.Equal(t, []string{"{\n\"value\": 42\n}"}, got)
			require.Equal(t, 1, info.ReceivedResponseCount)
		})
	}
}

func TestStreamScannerMultilineLimit(t *testing.T) {
	old := constant.StreamScannerMaxBufferMB
	constant.StreamScannerMaxBufferMB = 1
	t.Cleanup(func() { constant.StreamScannerMaxBufferMB = old })
	body := "data: {\n" + strings.Repeat("data: "+strings.Repeat("x", 32<<10)+"\n", 34)
	c, resp, info := setupStreamTest(t, strings.NewReader(body))
	info.DisablePing = true
	StreamScannerHandler(c, resp, info, func(data string, sr *StreamResult) { t.Error("oversized event dispatched") })
	require.Equal(t, relaycommon.StreamEndReasonScannerErr, info.StreamStatus.EndReason)
	require.ErrorContains(t, info.StreamStatus.EndError, "SSE event exceeds")
}

func TestSSEDataAssemblerBoundaries(t *testing.T) {
	a := sseDataAssembler{limit: 7}
	data, _, err := a.push("data: {")
	require.NoError(t, err)
	require.Empty(t, data)
	_, _, err = a.push("data: \"x\":0")
	require.NoError(t, err)
	_, _, err = a.push("data: }")
	require.Error(t, err)
	a = sseDataAssembler{limit: 9}
	for _, line := range []string{"data: {", "data: \"x\":0", "data: }"} {
		_, _, err = a.push(line)
		require.NoError(t, err)
	}
	data, done, err := a.push("data: [DONE]")
	require.NoError(t, err)
	require.True(t, done)
	require.Equal(t, "{\n\"x\":0\n}", data)
}
