package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func newRecoveryTestServer(handler gin.HandlerFunc) *gin.Engine {
	gin.SetMode(gin.TestMode)
	server := gin.New()
	server.Use(gin.CustomRecovery(recoverFromPanic))
	server.GET("/", handler)
	return server
}

func TestRecoveryHidesPanicDetails(t *testing.T) {
	server := newRecoveryTestServer(func(*gin.Context) { panic("secret-db-password") })
	w := httptest.NewRecorder()
	server.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
	require.Equal(t, http.StatusInternalServerError, w.Code)
	require.Contains(t, w.Body.String(), "new_api_panic")
	require.NotContains(t, w.Body.String(), "secret-db-password")
	require.NotContains(t, w.Body.String(), "github.com")
}

func TestRecoveryDoesNotAppendToStartedStream(t *testing.T) {
	server := newRecoveryTestServer(func(c *gin.Context) {
		c.Writer.Header().Set("Content-Type", "text/event-stream")
		_, _ = c.Writer.WriteString("data: partial\n\n")
		c.Writer.Flush()
		panic("mid-stream failure")
	})
	w := httptest.NewRecorder()
	server.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, "data: partial\n\n", w.Body.String())
	require.False(t, strings.Contains(w.Body.String(), "new_api_panic"))
}
