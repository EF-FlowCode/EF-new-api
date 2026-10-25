package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"eflow.local/new-api/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestMemoryModelRateLimitCountsOnlySuccessfulRequests(t *testing.T) {
	gin.SetMode(gin.TestMode)
	previousLimiter := inMemoryRateLimiter
	t.Cleanup(func() { inMemoryRateLimiter = previousLimiter })
	inMemoryRateLimiter = &common.InMemoryRateLimiter{}

	status := http.StatusBadGateway
	router := gin.New()
	router.POST("/relay", func(c *gin.Context) { c.Set("id", 41) },
		memoryRateLimitHandler("41", 60, 1, 0, 2),
		func(c *gin.Context) { c.Status(status) })
	request := func() int {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/relay", nil))
		return recorder.Code
	}

	// Failed upstream calls must not consume the success allowance.
	for range 5 {
		require.Equal(t, http.StatusBadGateway, request())
	}
	status = http.StatusOK
	require.Equal(t, http.StatusOK, request())
	require.Equal(t, http.StatusOK, request())
	require.Equal(t, http.StatusTooManyRequests, request())
}

func TestAPIKeyFromWebSocketSubprotocol(t *testing.T) {
	key, ok := apiKeyFromWebSocketSubprotocol("realtime, openai-insecure-api-key.sk-abc, openai-beta.realtime-v1")
	require.True(t, ok)
	require.Equal(t, "sk-abc", key)

	for _, protocols := range []string{"", "realtime", "realtime, openai-beta.realtime-v1", "openai-insecure-api-key."} {
		_, ok := apiKeyFromWebSocketSubprotocol(protocols)
		require.False(t, ok, protocols)
	}
}
