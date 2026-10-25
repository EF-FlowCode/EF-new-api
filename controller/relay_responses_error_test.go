package controller

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"eflow.local/new-api/relay/helper"
	"eflow.local/new-api/setting/operation_setting"
	"eflow.local/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestResponsesRelayErrorFraming(t *testing.T) {
	for _, started := range []bool{false, true} {
		t.Run(map[bool]string{false: "before_stream", true: "after_stream"}[started], func(t *testing.T) {
			writer := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(writer)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
			helper.SetEventStreamHeaders(c)
			if started {
				require.NoError(t, helper.StringData(c, `{"type":"response.created"}`))
			}
			apiErr := types.NewOpenAIError(errors.New("terminal event missing"), types.ErrorCodeBadResponseBody, http.StatusBadGateway)
			respondResponsesRelayError(c, apiErr)
			if !started {
				require.Equal(t, http.StatusBadGateway, writer.Code)
				require.Contains(t, writer.Header().Get("Content-Type"), "application/json")
				require.True(t, json.Valid(writer.Body.Bytes()))
				return
			}
			require.Equal(t, http.StatusOK, writer.Code)
			require.Contains(t, writer.Header().Get("Content-Type"), "text/event-stream")
			frames := strings.Split(strings.TrimSpace(writer.Body.String()), "\n\n")
			require.Len(t, frames, 2)
			require.True(t, strings.HasPrefix(frames[1], "event: error\ndata: "))
			var payload map[string]any
			require.NoError(t, json.Unmarshal([]byte(strings.TrimPrefix(frames[1], "event: error\ndata: ")), &payload))
			require.Equal(t, "error", payload["type"])
			require.Contains(t, payload["message"], "terminal event missing")
		})
	}
}

func TestResponsesRelayErrorCancelled(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil).WithContext(ctx)
	respondResponsesRelayError(c, types.NewOpenAIError(errors.New("cut"), types.ErrorCodeBadResponseBody, http.StatusBadGateway))
	require.Empty(t, w.Body.String())
}

func TestShouldRetryCommittedStream(t *testing.T) {
	for _, code := range []types.ErrorCode{types.ErrorCodeBadResponseBody, types.ErrorCodeChannelNoAvailableKey} {
		t.Run(string(code), func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
			apiErr := types.NewErrorWithStatusCode(errors.New("failed"), code, http.StatusBadGateway)
			apiErr.SetForceRetry(true)
			policy := operation_setting.GetChannelFailurePolicy()
			helper.SetEventStreamHeaders(c)
			require.True(t, shouldRetry(c, apiErr, 1, policy), "headers alone must not prevent retry")
			require.NoError(t, helper.StringData(c, `{"type":"response.created"}`))
			require.False(t, shouldRetry(c, apiErr, 1, policy), "neither force nor channel error may retry a committed stream")
		})
	}
}
