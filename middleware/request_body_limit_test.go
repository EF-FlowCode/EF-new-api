package middleware

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"eflow.local/new-api/constant"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestAnonymousRequestBodyLimitRejectsOversizedBody(t *testing.T) {
	old := constant.AnonymousRequestBodyLimitKB
	constant.AnonymousRequestBodyLimitKB = 1
	t.Cleanup(func() { constant.AnonymousRequestBodyLimitKB = old })

	router := gin.New()
	router.Use(AnonymousRequestBodyLimit())
	router.POST("/", func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(strings.Repeat("x", 2<<10)))
	router.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusRequestEntityTooLarge, recorder.Code)
}

func TestAnonymousRequestBodyLimitReplaysBody(t *testing.T) {
	old := constant.AnonymousRequestBodyLimitKB
	constant.AnonymousRequestBodyLimitKB = 1
	t.Cleanup(func() { constant.AnonymousRequestBodyLimitKB = old })

	const body = "small body"
	router := gin.New()
	router.Use(AnonymousRequestBodyLimit())
	router.POST("/", func(c *gin.Context) {
		data, err := io.ReadAll(c.Request.Body)
		require.NoError(t, err)
		require.Equal(t, body, string(data))
		require.Equal(t, int64(len(body)), c.Request.ContentLength)
		c.Status(http.StatusNoContent)
	})

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString(body))
	router.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusNoContent, recorder.Code)
}

func TestAnonymousRequestBodyLimitCanBeDisabled(t *testing.T) {
	old := constant.AnonymousRequestBodyLimitKB
	constant.AnonymousRequestBodyLimitKB = 0
	t.Cleanup(func() { constant.AnonymousRequestBodyLimitKB = old })

	router := gin.New()
	router.Use(AnonymousRequestBodyLimit())
	router.POST("/", func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(strings.Repeat("x", 2<<20)))
	router.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusNoContent, recorder.Code)
}
