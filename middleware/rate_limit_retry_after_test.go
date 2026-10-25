package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"eflow.local/new-api/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestMemoryRateLimiterSetsRetryAfter(t *testing.T) {
	gin.SetMode(gin.TestMode)
	previousLimiter := inMemoryRateLimiter
	t.Cleanup(func() { inMemoryRateLimiter = previousLimiter })
	inMemoryRateLimiter = &common.InMemoryRateLimiter{}
	inMemoryRateLimiter.Init(0)

	router := gin.New()
	router.GET("/limited", func(c *gin.Context) {
		memoryRateLimiter(c, 1, 42, "TEST")
		if c.IsAborted() {
			return
		}
		c.Status(http.StatusNoContent)
	})

	request := func() *httptest.ResponseRecorder {
		recorder := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/limited", nil)
		req.RemoteAddr = "192.0.2.10:12345"
		router.ServeHTTP(recorder, req)
		return recorder
	}

	assertResponse := request()
	require.Equal(t, http.StatusNoContent, assertResponse.Code)

	limitedResponse := request()
	require.Equal(t, http.StatusTooManyRequests, limitedResponse.Code)
	require.Equal(t, "42", limitedResponse.Header().Get("Retry-After"))
}
