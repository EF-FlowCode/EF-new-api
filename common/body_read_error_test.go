package common

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"eflow.local/new-api/constant"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type failingBodyReader struct{ err error }

func (r failingBodyReader) Read([]byte) (int, error) { return 0, r.err }

// TestGetRequestBodyKeepsReadErrorForLaterReaders covers a body read that
// fails: the body is consumed and closed, so later readers in the same request
// must get the original cause instead of "invalid Read on closed Body".
func TestGetRequestBodyKeepsReadErrorForLaterReaders(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cause := errors.New("connection reset mid-upload")
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", io.NopCloser(failingBodyReader{err: cause}))

	_, err := GetRequestBody(c)
	require.ErrorIs(t, err, cause)
	_, err = GetBodyStorage(c)
	require.ErrorIs(t, err, cause)
}

func TestGetRequestBodyKeepsTooLargeErrorForLaterReaders(t *testing.T) {
	gin.SetMode(gin.TestMode)
	old := constant.MaxRequestBodyMB
	t.Cleanup(func() { constant.MaxRequestBodyMB = old })
	constant.MaxRequestBodyMB = 1

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(strings.Repeat("a", 2<<20)))

	for range 2 {
		_, err := GetRequestBody(c)
		require.Error(t, err)
		require.True(t, IsRequestBodyTooLargeError(err))
		require.Contains(t, err.Error(), "request body exceeds 1 MB")
	}
}
