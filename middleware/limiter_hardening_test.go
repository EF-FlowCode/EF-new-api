package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"eflow.local/new-api/common"
	"eflow.local/new-api/pkg/requestip"
	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/require"
)

func TestEmailVerificationLimiterRepairsKeyWithoutTTL(t *testing.T) {
	gin.SetMode(gin.TestMode)
	server := miniredis.RunT(t)
	previous := common.RDB
	common.RDB = redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = common.RDB.Close(); common.RDB = previous })

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	ctx.Request.RemoteAddr = "192.0.2.10:1234"
	// Simulates an earlier INCR whose EXPIRE failed: the key would otherwise block this IP forever.
	key := "emailVerification:" + EmailVerificationRateLimitMark + ":" + requestip.GetClientIP(ctx)
	require.NoError(t, common.RDB.Set(context.Background(), key, 5, 0).Err())
	redisEmailVerificationRateLimiter(ctx)
	require.True(t, ctx.IsAborted())
	ttl := server.TTL(key)
	require.Greater(t, ttl, time.Duration(0))
	require.LessOrEqual(t, ttl, EmailVerificationDuration*time.Second)
}

func TestUserConcurrencyLimitReleasesSlots(t *testing.T) {
	gin.SetMode(gin.TestMode)
	release := make(chan struct{})
	entered := make(chan struct{})
	engine := gin.New()
	engine.Use(func(c *gin.Context) { c.Set("id", 42) })
	engine.GET("/", UserConcurrencyLimit("test", 1), func(c *gin.Context) {
		entered <- struct{}{}
		<-release
		c.Status(http.StatusOK)
	})
	do := func() int {
		res := httptest.NewRecorder()
		engine.ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/", nil))
		return res.Code
	}

	var wg sync.WaitGroup
	wg.Add(1)
	var first int
	go func() { defer wg.Done(); first = do() }()
	<-entered
	require.Equal(t, http.StatusTooManyRequests, do())
	close(release)
	wg.Wait()
	require.Equal(t, http.StatusOK, first)

	// The slot is released (and the entry dropped) once the request finishes.
	go func() { <-entered }()
	require.Equal(t, http.StatusOK, do())
}
