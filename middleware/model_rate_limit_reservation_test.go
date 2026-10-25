package middleware

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"eflow.local/new-api/common"
	"eflow.local/new-api/constant"
	relaycommon "eflow.local/new-api/relay/common"
	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/require"
)

// Redis 令牌桶是进程级单例，只绑定第一次见到的客户端。凡是可能走到令牌桶的测试都
// 共用这个永不关闭的 miniredis，每次使用前清空数据，避免单例拿着已关闭的客户端。
var sharedModelLimitRedis struct {
	once   sync.Once
	server *miniredis.Miniredis
	client *redis.Client
}

// useModelLimitStore points the limiter at a clean store for backend.
func useModelLimitStore(t *testing.T, backend string) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	if backend == "redis" {
		shared := &sharedModelLimitRedis
		shared.once.Do(func() {
			server, err := miniredis.Run()
			if err != nil {
				panic(err)
			}
			shared.server = server
			shared.client = redis.NewClient(&redis.Options{Addr: server.Addr()})
		})
		shared.server.FlushAll()
		previous := common.RDB
		common.RDB = shared.client
		t.Cleanup(func() { common.RDB = previous })
		return
	}
	previous := inMemoryRateLimiter
	inMemoryRateLimiter = &common.InMemoryRateLimiter{}
	inMemoryRateLimiter.Init(0)
	t.Cleanup(func() { inMemoryRateLimiter = previous })
}

func modelLimitBackend(t *testing.T, backend string, duration int64, total, success int) gin.HandlerFunc {
	t.Helper()
	useModelLimitStore(t, backend)
	if backend == "redis" {
		return redisRateLimitHandler("42", duration, int(duration/60), total, success)
	}
	return memoryRateLimitHandler("42", duration, int(duration/60), total, success)
}

func modelLimitRouter(limit gin.HandlerFunc, handler gin.HandlerFunc) *gin.Engine {
	router := gin.New()
	// Recovery wraps the limiter, so a panic must release before HTTP 500 is set.
	router.Use(func(c *gin.Context) {
		defer func() {
			if recover() != nil {
				c.AbortWithStatus(500)
			}
		}()
		c.Next()
	})
	router.GET("/", func(c *gin.Context) { c.Set("id", 42) }, limit, handler)
	return router
}

func doModelLimitRequest(router *gin.Engine) int {
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
	return w.Code
}

func TestModelSuccessLimitConcurrentRequests(t *testing.T) {
	for _, backend := range []string{"memory", "redis"} {
		t.Run(backend, func(t *testing.T) {
			limit := modelLimitBackend(t, backend, 60, 0, 3)
			release := make(chan struct{})
			defer close(release)
			router := modelLimitRouter(limit, func(c *gin.Context) { <-release; c.Status(502) })
			results := make(chan int, 32)
			for range 32 {
				go func() { results <- doModelLimitRequest(router) }()
			}
			// Three requests hold slots; the other 29 must be rejected immediately.
			for range 29 {
				select {
				case status := <-results:
					require.Equal(t, 429, status)
				case <-time.After(5 * time.Second):
					t.Fatal("concurrent requests exceeded the success allowance")
				}
			}
			for range 3 {
				release <- struct{}{}
			}
			for range 3 {
				require.Equal(t, 502, <-results)
			}
			// Failures release all slots. A new successful request consumes one.
			next := modelLimitRouter(limit, func(c *gin.Context) { c.Status(200) })
			for range 3 {
				require.Equal(t, 200, doModelLimitRequest(next))
			}
			require.Equal(t, 429, doModelLimitRequest(next))
		})
	}
}

func TestModelSuccessLimitOutcomes(t *testing.T) {
	cases := map[string]gin.HandlerFunc{
		"http_error":                  func(c *gin.Context) { c.Status(502) },
		"panic":                       func(c *gin.Context) { panic("upstream panic") },
		"final_relay_error_after_200": func(c *gin.Context) { c.Status(200); common.SetContextKey(c, constant.ContextKeyRelaySucceeded, false) },
		"stream_error_after_200": func(c *gin.Context) {
			s := relaycommon.NewStreamStatus()
			s.SetEndReason(relaycommon.StreamEndReasonDone, nil)
			s.RecordError("malformed upstream event")
			common.SetContextKey(c, constant.ContextKeyRelayStreamStatus, s)
		},
		"timeout": func(c *gin.Context) {
			s := relaycommon.NewStreamStatus()
			s.SetEndReason(relaycommon.StreamEndReasonTimeout, nil)
			common.SetContextKey(c, constant.ContextKeyRelayStreamStatus, s)
		},
		"cancel": func(c *gin.Context) {
			ctx, cancel := context.WithCancel(c.Request.Context())
			cancel()
			c.Request = c.Request.WithContext(ctx)
		},
	}
	for _, backend := range []string{"memory", "redis"} {
		for name, fail := range cases {
			t.Run(backend+"/"+name, func(t *testing.T) {
				limit := modelLimitBackend(t, backend, 60, 0, 1)
				router := modelLimitRouter(limit, fail)
				doModelLimitRequest(router)
				next := modelLimitRouter(limit, func(c *gin.Context) { c.Status(200) })
				require.Equal(t, 200, doModelLimitRequest(next))
				require.Equal(t, 429, doModelLimitRequest(next))
			})
		}
	}
}

func TestModelSuccessLimitDisabledAndTotalLimit(t *testing.T) {
	for _, backend := range []string{"memory", "redis"} {
		t.Run(backend+"/zero_success", func(t *testing.T) {
			limit := modelLimitBackend(t, backend, 60, 2, 0)
			router := modelLimitRouter(limit, func(c *gin.Context) { c.Status(502) })
			require.Equal(t, 502, doModelLimitRequest(router))
			require.Equal(t, 502, doModelLimitRequest(router))
			require.Equal(t, 429, doModelLimitRequest(router))
		})
		t.Run(backend+"/zero_duration", func(t *testing.T) {
			limit := modelLimitBackend(t, backend, 0, 1, 1)
			router := modelLimitRouter(limit, func(c *gin.Context) { c.Status(200) })
			for range 3 {
				require.Equal(t, 200, doModelLimitRequest(router))
			}
		})
	}
}

func TestModelSuccessLimitRetryUsesFinalAttempt(t *testing.T) {
	for _, backend := range []string{"memory", "redis"} {
		t.Run(backend, func(t *testing.T) {
			limit := modelLimitBackend(t, backend, 60, 0, 1)
			router := modelLimitRouter(limit, func(c *gin.Context) {
				info := &relaycommon.RelayInfo{StreamStatus: relaycommon.NewStreamStatus()}
				info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonHandlerStop, errors.New("first attempt"))
				common.SetContextKey(c, constant.ContextKeyRelayStreamStatus, info.StreamStatus)
				common.SetContextKey(c, constant.ContextKeyRelaySucceeded, false)
				info.InitChannelMeta(c)
				require.Nil(t, info.StreamStatus)
				common.SetContextKey(c, constant.ContextKeyRelaySucceeded, true)
			})
			require.Equal(t, 200, doModelLimitRequest(router))
			require.Equal(t, 429, doModelLimitRequest(router))
		})
	}
}

// 无 Redis 时内存限流与 Redis 限流给出同样的拒绝：OpenAI 错误体写明撞到的是
// 总次数还是成功次数限制，而不是空的 429。
func TestModelRateLimitRejectionsExplainLimit(t *testing.T) {
	for _, backend := range []string{"memory", "redis"} {
		for _, tc := range []struct {
			name           string
			total, success int
			want           string
		}{
			{name: "total", total: 1, want: "您已达到总请求数限制：1分钟内最多请求1次，包括失败次数，请检查您的请求是否正确"},
			{name: "success", success: 1, want: "您已达到请求数限制：1分钟内最多请求1次"},
		} {
			t.Run(backend+"/"+tc.name, func(t *testing.T) {
				limit := modelLimitBackend(t, backend, 60, tc.total, tc.success)
				router := gin.New()
				router.GET("/", func(c *gin.Context) {
					c.Set("id", 42)
					c.Set(common.RequestIdKey, "req-limited")
				}, limit, func(c *gin.Context) { c.Status(http.StatusOK) })
				require.Equal(t, http.StatusOK, doModelLimitRequest(router))

				w := httptest.NewRecorder()
				router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
				require.Equal(t, http.StatusTooManyRequests, w.Code)
				want, err := common.Marshal(gin.H{"error": gin.H{
					"message": common.MessageWithRequestId(tc.want, "req-limited"),
					"type":    "new_api_error",
					"code":    "",
				}})
				require.NoError(t, err)
				require.JSONEq(t, string(want), w.Body.String())
			})
		}
	}
}
