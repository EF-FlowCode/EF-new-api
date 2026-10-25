package controller

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"eflow.local/new-api/relay/helper"
	"eflow.local/new-api/setting/operation_setting"
	"eflow.local/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func newKeepAliveRelayContext(t *testing.T) (*gin.Context, *httptest.ResponseRecorder, func()) {
	t.Helper()
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	stop := helper.StartStreamKeepAlive(c, time.Millisecond)
	require.Eventually(t, c.Writer.Written, time.Second, time.Millisecond)
	stop()
	return c, recorder, stop
}

func TestShouldRetryAfterKeepAliveOnly(t *testing.T) {
	c, _, stop := newKeepAliveRelayContext(t)
	defer stop()
	apiErr := types.NewErrorWithStatusCode(errors.New("upstream down"), types.ErrorCodeBadResponseBody, http.StatusBadGateway)
	apiErr.SetForceRetry(true)
	policy := operation_setting.GetChannelFailurePolicy()

	require.True(t, shouldRetry(c, apiErr, 1, policy), "keepalive pings alone must not block retry")
	require.NoError(t, helper.StringData(c, `{"type":"message_start"}`))
	require.False(t, shouldRetry(c, apiErr, 1, policy), "a stream with output cannot be retried")
}

func TestClaudeRelayErrorAfterKeepAliveIsStreamEvent(t *testing.T) {
	c, recorder, stop := newKeepAliveRelayContext(t)
	defer stop()
	apiErr := types.NewErrorWithStatusCode(errors.New("no channel"), types.ErrorCodeGetChannelFailed, http.StatusServiceUnavailable)

	writeRelayStreamError(c, types.RelayFormatClaude, apiErr)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, "text/event-stream", recorder.Header().Get("Content-Type"))
	frames := strings.Split(strings.TrimSpace(recorder.Body.String()), "\n\n")
	require.Equal(t, ": PING", frames[0])
	last := frames[len(frames)-1]
	require.True(t, strings.HasPrefix(last, "event: error\ndata: "), last)
	var payload map[string]any
	require.NoError(t, json.Unmarshal([]byte(strings.TrimPrefix(last, "event: error\ndata: ")), &payload))
	require.Equal(t, "error", payload["type"])
	require.Contains(t, payload["error"].(map[string]any)["message"], "no channel")
}
