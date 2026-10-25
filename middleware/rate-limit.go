package middleware

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"eflow.local/new-api/common"
	"eflow.local/new-api/logger"
	"eflow.local/new-api/pkg/requestip"
	"github.com/gin-contrib/static"
	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
)

var timeFormat = "2006-01-02T15:04:05.000Z"

var inMemoryRateLimiter = &common.InMemoryRateLimiter{}

var redisWindowLimitScript = redis.NewScript(`
local key = KEYS[1]
local now = tonumber(ARGV[1])
local window = tonumber(ARGV[2])
local limit = tonumber(ARGV[3])
redis.call('ZREMRANGEBYSCORE', key, 0, now - window)
local count = redis.call('ZCARD', key)
if count >= limit then
  redis.call('PEXPIRE', key, window)
  return 0
end
redis.call('ZADD', key, now, tostring(now) .. ':' .. tostring(count))
redis.call('PEXPIRE', key, window)
return 1
`)

var defNext = func(c *gin.Context) {
	c.Next()
}

func redisRateLimiter(c *gin.Context, maxRequestNum int, duration int64, mark string) {
	key := "rateLimit:v2:" + mark + requestip.GetClientIP(c)
	redisWindowRateLimiter(c, maxRequestNum, duration, key)
}

func memoryRateLimiter(c *gin.Context, maxRequestNum int, duration int64, mark string) {
	key := mark + requestip.GetClientIP(c)
	if !inMemoryRateLimiter.Request(key, maxRequestNum, duration) {
		writeRateLimited(c, duration)
		return
	}
}

// writeRateLimited rejects the request with a retry hint so clients can back
// off instead of treating a temporary limit as a terminal failure.
func writeRateLimited(c *gin.Context, retryAfterSeconds int64) {
	if retryAfterSeconds > 0 {
		c.Header("Retry-After", strconv.FormatInt(retryAfterSeconds, 10))
	}
	c.Status(http.StatusTooManyRequests)
	c.Abort()
}

func rateLimitFactory(maxRequestNum int, duration int64, mark string) func(c *gin.Context) {
	if common.RedisEnabled {
		return func(c *gin.Context) {
			redisRateLimiter(c, maxRequestNum, duration, mark)
		}
	} else {
		// It's safe to call multi times.
		inMemoryRateLimiter.Init(common.RateLimitKeyExpirationDuration)
		return func(c *gin.Context) {
			memoryRateLimiter(c, maxRequestNum, duration, mark)
		}
	}
}

// GlobalWebRateLimit limits dashboard pages and other fallback paths per IP.
// Files that exist in the frontend build count against a separate static
// limiter (disabled by default) instead, so lazy-loaded chunks and icons of one
// cold page load do not use up the page budget of everyone behind the same IP.
func GlobalWebRateLimit(frontendFS static.ServeFileSystem) func(c *gin.Context) {
	webLimit := defNext
	if common.GlobalWebRateLimitEnable {
		webLimit = rateLimitFactory(common.GlobalWebRateLimitNum, common.GlobalWebRateLimitDuration, "GW")
	}
	staticLimit := defNext
	if common.GlobalStaticRateLimitEnable {
		staticLimit = rateLimitFactory(common.GlobalStaticRateLimitNum, common.GlobalStaticRateLimitDuration, "GS")
	}
	return func(c *gin.Context) {
		if frontendFS != nil && frontendFS.Exists("/", c.Request.URL.Path) {
			staticLimit(c)
			return
		}
		webLimit(c)
	}
}

func GlobalAPIRateLimit() func(c *gin.Context) {
	if common.GlobalApiRateLimitEnable {
		return rateLimitFactory(common.GlobalApiRateLimitNum, common.GlobalApiRateLimitDuration, "GA")
	}
	return defNext
}

func CriticalRateLimit() func(c *gin.Context) {
	if common.CriticalRateLimitEnable {
		return rateLimitFactory(common.CriticalRateLimitNum, common.CriticalRateLimitDuration, "CT")
	}
	return defNext
}

// UserCriticalRateLimit applies the critical limit per authenticated user.
// It is intended for sensitive self-service actions where an IP-based bucket
// can be bypassed by rotating proxy addresses.
func UserCriticalRateLimit(scope string) func(c *gin.Context) {
	if !common.CriticalRateLimitEnable {
		return defNext
	}
	return userRateLimitFactory(
		common.CriticalRateLimitNum,
		common.CriticalRateLimitDuration,
		"UC:"+scope,
	)
}

func DownloadRateLimit() func(c *gin.Context) {
	return rateLimitFactory(common.DownloadRateLimitNum, common.DownloadRateLimitDuration, "DW")
}

func UploadRateLimit() func(c *gin.Context) {
	return rateLimitFactory(common.UploadRateLimitNum, common.UploadRateLimitDuration, "UP")
}

// userRateLimitFactory creates a rate limiter keyed by authenticated user ID
// instead of client IP, making it resistant to proxy rotation attacks.
// Must be used AFTER authentication middleware (UserAuth).
func userRateLimitFactory(maxRequestNum int, duration int64, mark string) func(c *gin.Context) {
	if common.RedisEnabled {
		return func(c *gin.Context) {
			userId := c.GetInt("id")
			if userId == 0 {
				c.Status(http.StatusUnauthorized)
				c.Abort()
				return
			}
			key := fmt.Sprintf("rateLimit:v2:%s:user:%d", mark, userId)
			userRedisRateLimiter(c, maxRequestNum, duration, key)
		}
	}
	// It's safe to call multi times.
	inMemoryRateLimiter.Init(common.RateLimitKeyExpirationDuration)
	return func(c *gin.Context) {
		userId := c.GetInt("id")
		if userId == 0 {
			c.Status(http.StatusUnauthorized)
			c.Abort()
			return
		}
		key := fmt.Sprintf("%s:user:%d", mark, userId)
		if !inMemoryRateLimiter.Request(key, maxRequestNum, duration) {
			writeRateLimited(c, duration)
			return
		}
	}
}

// userRedisRateLimiter is like redisRateLimiter but accepts a pre-built key
// (to support user-ID-based keys).
func userRedisRateLimiter(c *gin.Context, maxRequestNum int, duration int64, key string) {
	redisWindowRateLimiter(c, maxRequestNum, duration, key)
}

func redisWindowRateLimiter(c *gin.Context, maxRequestNum int, duration int64, key string) {
	if maxRequestNum <= 0 || duration <= 0 {
		return
	}
	ctx := context.Background()
	if c.Request != nil {
		ctx = c.Request.Context()
	}
	if common.RDB == nil {
		logger.LogError(ctx, fmt.Sprintf("rate limit check failed (key=%s): Redis client is not initialized", key))
		c.Status(http.StatusInternalServerError)
		c.Abort()
		return
	}
	now := time.Now()
	allowed, err := redisWindowLimitScript.Run(
		ctx,
		common.RDB,
		[]string{key},
		now.UnixMilli(),
		duration*1000,
		maxRequestNum,
	).Int()
	if err != nil {
		logger.LogError(ctx, fmt.Sprintf("rate limit check failed (key=%s): %v", key, err))
		c.Status(http.StatusInternalServerError)
		c.Abort()
		return
	}
	if allowed != 1 {
		retryAfter := duration
		if ttl, ttlErr := common.RDB.PTTL(ctx, key).Result(); ttlErr == nil {
			if ttl >= 0 {
				retryAfter = max(int64((ttl+time.Second-1)/time.Second), 1)
			}
		}
		writeRateLimited(c, retryAfter)
	}
}

// SearchRateLimit returns a per-user rate limiter for search endpoints,
// keyed under the shared "SR" bucket.
// Configurable via SEARCH_RATE_LIMIT_ENABLE / SEARCH_RATE_LIMIT / SEARCH_RATE_LIMIT_DURATION.
func SearchRateLimit() func(c *gin.Context) {
	return SearchRateLimitWithMark("SR")
}

// SearchRateLimitWithMark is like SearchRateLimit but lets each endpoint own a
// distinct rate-limit bucket (mark). 之前 /log/self、/self/stat、/self/export 全
// 走同一个 "SR" 桶（10/60s/用户），列表 + 统计 + 导出互相挤占，正常翻页几下就
// 撞 429。拆 mark 后各路径各自计数，互不影响。
func SearchRateLimitWithMark(mark string) func(c *gin.Context) {
	if !common.SearchRateLimitEnable {
		return defNext
	}
	return userRateLimitFactory(common.SearchRateLimitNum, common.SearchRateLimitDuration, mark)
}

// TaskQueryRateLimit bounds per-user task status/result polling with its own
// generous bucket. It must run after token authentication and never feeds the
// model request/success counters, so polling cannot starve normal relays.
func TaskQueryRateLimit() func(c *gin.Context) {
	if !common.TaskQueryRateLimitEnable {
		return defNext
	}
	return userRateLimitFactory(common.TaskQueryRateLimitNum, common.TaskQueryRateLimitDuration, "TQ")
}
