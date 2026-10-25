package helper

import (
	"errors"
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"eflow.local/new-api/common"
	"eflow.local/new-api/constant"
	"eflow.local/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type failingRequestBody struct{}

func (failingRequestBody) Read([]byte) (int, error) { return 0, errors.New("read failed") }
func (failingRequestBody) Close() error             { return nil }

func newValidationContext(t *testing.T, path string, body io.Reader) *gin.Context {
	t.Helper()
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest("POST", path, body)
	c.Request.Header.Set("Content-Type", "application/json")
	t.Cleanup(func() { common.CleanupBodyStorage(c) })
	return c
}

func TestGetAndValidateRequestClassifiesClientErrors(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "invalid JSON", body: `{"model":`},
		{name: "missing model and messages", body: `{}`},
		{name: "invalid max_tokens range", body: `{"model":"test","messages":[{"role":"user","content":"hi"}],"max_tokens":2147483648}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := newValidationContext(t, "/v1/chat/completions", strings.NewReader(tt.body))
			_, err := GetAndValidateRequest(c, types.RelayFormatOpenAI)

			require.Error(t, err)
			require.True(t, IsRequestValidationError(err))
		})
	}
}

func TestGetAndValidateRequestKeepsInternalErrorsUnclassified(t *testing.T) {
	tests := []struct {
		name   string
		path   string
		format types.RelayFormat
	}{
		{name: "chat", path: "/v1/chat/completions", format: types.RelayFormatOpenAI},
		{name: "embedding", path: "/v1/embeddings", format: types.RelayFormatEmbedding},
		{name: "rerank", path: "/v1/rerank", format: types.RelayFormatRerank},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := newValidationContext(t, tt.path, failingRequestBody{})
			_, err := GetAndValidateRequest(c, tt.format)

			require.Error(t, err)
			require.False(t, IsRequestValidationError(err))
		})
	}
}

func TestGetAndValidateRequestKeepsUnsupportedFormatUnclassified(t *testing.T) {
	c := newValidationContext(t, "/v1/chat/completions", strings.NewReader(`{}`))
	_, err := GetAndValidateRequest(c, types.RelayFormat("unknown"))

	require.Error(t, err)
	require.False(t, IsRequestValidationError(err))
}

func TestGetAndValidateRequestPreservesBodyTooLargeError(t *testing.T) {
	previousLimit := constant.MaxRequestBodyMB
	constant.MaxRequestBodyMB = 1
	t.Cleanup(func() { constant.MaxRequestBodyMB = previousLimit })

	c := newValidationContext(t, "/v1/chat/completions", strings.NewReader(strings.Repeat("x", 1<<20+1)))
	_, err := GetAndValidateRequest(c, types.RelayFormatOpenAI)

	require.Error(t, err)
	require.ErrorIs(t, err, common.ErrRequestBodyTooLarge)
	require.False(t, IsRequestValidationError(err))
}
