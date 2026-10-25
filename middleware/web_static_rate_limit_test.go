package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"eflow.local/new-api/common"
	"github.com/alicebob/miniredis/v2"
	"github.com/gin-contrib/static"
	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/require"
)

// 前端构建里存在的文件走独立的静态限流桶（默认关闭），不再消耗 Web 页面额度；
// 页面与不存在的路径（含缺失的哈希资源）仍计入 Web 限流。
func TestGlobalWebRateLimitCountsFrontendFilesSeparately(t *testing.T) {
	gin.SetMode(gin.TestMode)
	prevWeb := []any{common.GlobalWebRateLimitEnable, common.GlobalWebRateLimitNum, common.GlobalWebRateLimitDuration}
	prevStatic := []any{common.GlobalStaticRateLimitEnable, common.GlobalStaticRateLimitNum, common.GlobalStaticRateLimitDuration}
	prevRedisEnabled, prevRDB, prevLimiter := common.RedisEnabled, common.RDB, inMemoryRateLimiter
	t.Cleanup(func() {
		common.GlobalWebRateLimitEnable, common.GlobalWebRateLimitNum, common.GlobalWebRateLimitDuration = prevWeb[0].(bool), prevWeb[1].(int), prevWeb[2].(int64)
		common.GlobalStaticRateLimitEnable, common.GlobalStaticRateLimitNum, common.GlobalStaticRateLimitDuration = prevStatic[0].(bool), prevStatic[1].(int), prevStatic[2].(int64)
		common.RedisEnabled, common.RDB, inMemoryRateLimiter = prevRedisEnabled, prevRDB, prevLimiter
	})
	common.GlobalWebRateLimitEnable, common.GlobalWebRateLimitNum, common.GlobalWebRateLimitDuration = true, 2, 41
	common.GlobalStaticRateLimitNum, common.GlobalStaticRateLimitDuration = 2, 43

	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "static", "js"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "static", "js", "app.js"), []byte("app"), 0o644))
	frontendFS := static.LocalFile(dir, false)

	const clientIP = "192.0.2.70"
	useBackend := func(t *testing.T, backend string) *redis.Client {
		inMemoryRateLimiter = &common.InMemoryRateLimiter{}
		common.RedisEnabled = backend == "redis"
		if backend != "redis" {
			return nil
		}
		server := miniredis.RunT(t)
		client := redis.NewClient(&redis.Options{Addr: server.Addr()})
		common.RDB = client
		t.Cleanup(func() { _ = client.Close() })
		return client
	}
	newRouter := func() *gin.Engine {
		router := gin.New()
		require.NoError(t, router.SetTrustedProxies(nil))
		router.NoRoute(GlobalWebRateLimit(frontendFS), func(c *gin.Context) { c.Status(http.StatusNoContent) })
		return router
	}
	do := func(router *gin.Engine, path string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.RemoteAddr = clientIP + ":12345"
		router.ServeHTTP(w, req)
		return w
	}
	zcard := func(client *redis.Client, mark string) int64 {
		return client.ZCard(context.Background(), "rateLimit:v2:"+mark+clientIP).Val()
	}

	for _, backend := range []string{"memory", "redis"} {
		t.Run(backend+"/static_limit_disabled", func(t *testing.T) {
			client := useBackend(t, backend)
			common.GlobalStaticRateLimitEnable = false
			router := newRouter()
			for range 5 {
				require.Equal(t, http.StatusNoContent, do(router, "/static/js/app.js").Code)
			}
			require.Equal(t, http.StatusNoContent, do(router, "/channels").Code)
			require.Equal(t, http.StatusNoContent, do(router, "/static/js/missing.js").Code)
			limited := do(router, "/channels")
			require.Equal(t, http.StatusTooManyRequests, limited.Code)
			require.Equal(t, "41", limited.Header().Get("Retry-After"))
			if client != nil {
				require.EqualValues(t, 2, zcard(client, "GW"))
				require.EqualValues(t, 0, zcard(client, "GS"))
			}
		})
		t.Run(backend+"/static_limit_enabled", func(t *testing.T) {
			client := useBackend(t, backend)
			common.GlobalStaticRateLimitEnable = true
			router := newRouter()
			require.Equal(t, http.StatusNoContent, do(router, "/static/js/app.js").Code)
			require.Equal(t, http.StatusNoContent, do(router, "/static/js/app.js").Code)
			limited := do(router, "/static/js/app.js")
			require.Equal(t, http.StatusTooManyRequests, limited.Code)
			require.Equal(t, "43", limited.Header().Get("Retry-After"))
			// 静态桶用尽不影响页面。
			require.Equal(t, http.StatusNoContent, do(router, "/channels").Code)
			if client != nil {
				require.EqualValues(t, 2, zcard(client, "GS"))
				require.EqualValues(t, 1, zcard(client, "GW"))
			}
		})
	}
}
