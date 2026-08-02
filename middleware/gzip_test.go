package middleware

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/klauspost/compress/zstd"
	"github.com/stretchr/testify/require"
)

func TestDecompressRequestMiddlewareSupportsZstd(t *testing.T) {
	gin.SetMode(gin.TestMode)

	var encoded bytes.Buffer
	writer, err := zstd.NewWriter(&encoded)
	require.NoError(t, err)
	_, err = writer.Write([]byte("zstd request body"))
	require.NoError(t, err)
	require.NoError(t, writer.Close())

	router := gin.New()
	router.Use(DecompressRequestMiddleware())
	router.POST("/", func(c *gin.Context) {
		body, readErr := io.ReadAll(c.Request.Body)
		require.NoError(t, readErr)
		c.String(http.StatusOK, "%s|%s", body, c.GetHeader("Content-Encoding"))
	})

	request := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(encoded.Bytes()))
	request.Header.Set("Content-Encoding", "zstd")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, "zstd request body|", recorder.Body.String())
}
