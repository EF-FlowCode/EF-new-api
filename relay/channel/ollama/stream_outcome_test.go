package ollama

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"eflow.local/new-api/constant"
	relaycommon "eflow.local/new-api/relay/common"
	"eflow.local/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type brokenOllamaReader struct{}

func (brokenOllamaReader) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }

func TestOllamaStreamOutcome(t *testing.T) {
	for _, tc := range []struct {
		name, body            string
		cut, failed, apiError bool
	}{
		{name: "normal", body: `{"done":true,"prompt_eval_count":5,"eval_count":7}`},
		{name: "premature_eof", body: `{"message":{"content":"partial"}}`, failed: true},
		{name: "empty", failed: true},
		{name: "read_error", body: "{\"message\":{\"content\":\"partial\"}}\n", cut: true, failed: true},
		{name: "malformed", body: `{broken`, failed: true, apiError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
			info := &relaycommon.RelayInfo{IsStream: true, ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "model"}}
			var reader io.Reader = strings.NewReader(tc.body)
			if tc.cut {
				reader = io.MultiReader(reader, brokenOllamaReader{})
			}
			usage, apiErr := ollamaStreamHandler(c, info, &http.Response{Body: io.NopCloser(reader)})
			require.Equal(t, tc.apiError, apiErr != nil)
			require.NotNil(t, usage, "received usage stays on the settlement path")
			require.Equal(t, tc.failed, info.StreamStatus.ResponseFailed())
			shared, exists := c.Get(string(constant.ContextKeyRelayStreamStatus))
			require.True(t, exists)
			require.Same(t, info.StreamStatus, shared)
			require.Nil(t, service.StreamAbortRetryError(c, info), "committed stream must not replay")
			if tc.failed {
				require.NotContains(t, w.Body.String(), "data: [DONE]")
			} else {
				require.Equal(t, 12, usage.TotalTokens)
				require.Contains(t, w.Body.String(), "data: [DONE]")
			}
		})
	}
}

type blockingOllamaBody struct {
	read, closed        chan struct{}
	readOnce, closeOnce sync.Once
}

func (b *blockingOllamaBody) Read([]byte) (int, error) {
	b.readOnce.Do(func() { close(b.read) })
	<-b.closed
	return 0, io.ErrClosedPipe
}
func (b *blockingOllamaBody) Close() error {
	b.closeOnce.Do(func() { close(b.closed) })
	return nil
}

func TestOllamaStreamCancellationClosesBlockedUpstream(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	body := &blockingOllamaBody{read: make(chan struct{}), closed: make(chan struct{})}
	defer body.Close()
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil).WithContext(ctx)
	info := &relaycommon.RelayInfo{IsStream: true, ChannelMeta: &relaycommon.ChannelMeta{}}
	done := make(chan struct{})
	go func() { defer close(done); _, _ = ollamaStreamHandler(c, info, &http.Response{Body: body}) }()
	select {
	case <-body.read:
	case <-time.After(2 * time.Second):
		t.Fatal("upstream read did not start")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("cancellation did not unblock upstream")
	}
	require.True(t, info.StreamStatus.ResponseFailed())
	require.Equal(t, relaycommon.StreamEndReasonClientGone, info.StreamStatus.EndReason)
}

type failingOllamaWriter struct {
	gin.ResponseWriter
	marker string
}

func (w failingOllamaWriter) Write(data []byte) (int, error) {
	if strings.Contains(string(data), w.marker) {
		return 0, errors.New("downstream write failed")
	}
	return w.ResponseWriter.Write(data)
}

func TestOllamaStreamWriteFailurePreservesTerminalUsage(t *testing.T) {
	for _, marker := range []string{"terminal text", "finish_reason", "[DONE]"} {
		t.Run(marker, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
			// finish_reason also appears as null in earlier chunks; match its terminal value.
			if marker == "finish_reason" {
				marker = `"finish_reason":"stop"`
			}
			c.Writer = failingOllamaWriter{ResponseWriter: c.Writer, marker: marker}
			info := &relaycommon.RelayInfo{IsStream: true, ChannelMeta: &relaycommon.ChannelMeta{}}
			body := `{"message":{"content":"terminal text"},"done":true,"prompt_eval_count":5,"eval_count":7}`
			usage, apiErr := ollamaStreamHandler(c, info, &http.Response{Body: io.NopCloser(strings.NewReader(body))})
			require.Nil(t, apiErr)
			require.Equal(t, 12, usage.TotalTokens)
			require.True(t, info.StreamStatus.ResponseFailed())
			require.Nil(t, service.StreamAbortRetryError(c, info))
			require.NotContains(t, w.Body.String(), "data: [DONE]")
		})
	}
}

func TestOllamaStreamUpstreamError(t *testing.T) {
	t.Run("before_output_is_retryable", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
		info := &relaycommon.RelayInfo{IsStream: true, ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "model"}}
		usage, apiErr := ollamaStreamHandler(c, info, &http.Response{Body: io.NopCloser(strings.NewReader(`{"error":"provider failed","done":true}`))})
		require.Nil(t, usage)
		require.NotNil(t, apiErr)
		require.Contains(t, apiErr.Error(), "provider failed")
		require.False(t, c.Writer.Written(), "nothing committed, so the relay may retry")
		require.True(t, info.StreamStatus.ResponseFailed())
		require.Contains(t, info.StreamStatus.Errors[0].Message, "provider failed")
	})
	t.Run("after_output_writes_error_frame", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
		info := &relaycommon.RelayInfo{IsStream: true, ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "model"}}
		info.SetEstimatePromptTokens(3)
		body := "{\"message\":{\"content\":\"partial answer\"}}\n{\"error\":\"provider failed\"}\n"
		usage, apiErr := ollamaStreamHandler(c, info, &http.Response{Body: io.NopCloser(strings.NewReader(body))})
		require.Nil(t, apiErr)
		require.Equal(t, 3, usage.PromptTokens)
		require.Positive(t, usage.CompletionTokens, "delivered output is settled")
		out := w.Body.String()
		require.Contains(t, out, "partial answer")
		require.Contains(t, out, `data: {"error":{"message":"ollama upstream error: provider failed"`)
		require.NotContains(t, out, "data: [DONE]")
		require.True(t, info.StreamStatus.ResponseFailed())
		require.Nil(t, service.StreamAbortRetryError(c, info), "committed stream must not replay")
	})
}
