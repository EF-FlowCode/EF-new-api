package middleware

import (
	"fmt"
	"math"
	"net/http"
	"strconv"
	"time"

	"eflow.local/new-api/common"
	"eflow.local/new-api/common/limiter"
	"eflow.local/new-api/constant"
	relaycommon "eflow.local/new-api/relay/common"
	"eflow.local/new-api/setting"
	"eflow.local/new-api/types"

	"github.com/gin-gonic/gin"
)

const modelRPMWindowSeconds int64 = 60

// CheckUserModelRPM limits each user and original request model independently.
func CheckUserModelRPM(c *gin.Context, modelName string) *types.NewAPIError {
	additionalTiers, _ := common.GetContextKeyType[[]string](c, constant.ContextKeyUserAdditionalCustomerTiers)
	rpm := setting.GetUserModelRPM(
		modelName,
		common.GetContextKeyString(c, constant.ContextKeyUserGroup),
		additionalTiers,
	)
	if rpm == 0 {
		return nil
	}

	userID := c.GetInt(string(constant.ContextKeyUserId))
	if userID <= 0 {
		return types.NewErrorWithStatusCode(
			fmt.Errorf("missing authenticated user for model RPM limit"),
			types.ErrorCodeAccessDenied,
			http.StatusUnauthorized,
			types.ErrOptionWithSkipRetry(),
		)
	}
	key := fmt.Sprintf("rateLimit:v2:UMR:user:%d:model:%s", userID, modelName)
	allowed := false
	if common.RedisEnabled {
		var err error
		allowed, err = redisWindowLimitScript.Run(
			c.Request.Context(),
			common.RDB,
			[]string{key},
			time.Now().UnixMilli(),
			modelRPMWindowSeconds*1000,
			rpm,
		).Bool()
		if err != nil {
			return types.NewError(err, types.ErrorCodeUserModelRateLimited)
		}
	} else {
		inMemoryRateLimiter.Init(common.RateLimitKeyExpirationDuration)
		allowed = inMemoryRateLimiter.Request(key, rpm, modelRPMWindowSeconds)
	}

	if allowed {
		return nil
	}
	return types.NewErrorWithStatusCode(
		fmt.Errorf("模型 %s 每分钟最多请求 %d 次", modelName, rpm),
		types.ErrorCodeUserModelRateLimited,
		http.StatusTooManyRequests,
		types.ErrOptionWithSkipRetry(),
	)
}

const (
	ModelRequestRateLimitCountMark        = "MRRL"
	ModelRequestRateLimitSuccessCountMark = "MRRLS"
)

// modelRateLimitSucceeded is evaluated only after the entire handler returned.
// Relay marks its final attempt explicitly; ordinary handlers retain HTTP fallback.
func modelRateLimitSucceeded(c *gin.Context) bool {
	if c.Writer.Status() >= 400 || c.Request.Context().Err() != nil {
		return false
	}
	if succeeded, exists := common.GetContextKeyType[bool](c, constant.ContextKeyRelaySucceeded); exists && !succeeded {
		return false
	}
	status, _ := common.GetContextKeyType[*relaycommon.StreamStatus](c, constant.ContextKeyRelayStreamStatus)
	return !status.ResponseFailed()
}

// Redis 与内存限流共用同一套拒绝响应：OpenAI 错误体说明撞到的是哪个限制，
// 并按用户写一条错误日志，客户端可与上游 429 区分，运维也能查到是谁被限。
func abortModelTotalRateLimited(c *gin.Context, durationMinutes, totalMaxCount int) {
	abortWithOpenAiMessage(c, http.StatusTooManyRequests, fmt.Sprintf("您已达到总请求数限制：%d分钟内最多请求%d次，包括失败次数，请检查您的请求是否正确", durationMinutes, totalMaxCount))
}

func abortModelSuccessRateLimited(c *gin.Context, durationMinutes, successMaxCount int) {
	abortWithOpenAiMessage(c, http.StatusTooManyRequests, fmt.Sprintf("您已达到请求数限制：%d分钟内最多请求%d次", durationMinutes, successMaxCount))
}

// modelRateLimitSubject 决定计数主体：配置了独立限流的分组按“用户ID:分组”
// 单独计数，其余请求共用该用户的全局计数，避免多个分组的令牌叠加放大全局上限。
// 用户 ID 是整数，不含冒号，分组主体不会与用户主体或其他用户的分组主体重名。
func modelRateLimitSubject(userID int, group string, groupLimited bool) string {
	subject := strconv.Itoa(userID)
	if groupLimited {
		subject += ":" + group
	}
	return subject
}

// Redis限流处理器
func redisRateLimitHandler(subject string, duration int64, durationMinutes, totalMaxCount, successMaxCount int) gin.HandlerFunc {
	return func(c *gin.Context) {
		if duration <= 0 {
			c.Next()
			return
		}
		ctx := c.Request.Context()
		rdb := common.RDB

		//2.检查总请求数限制并记录总请求（当totalMaxCount为0时会自动跳过，使用令牌桶限流器
		if totalMaxCount > 0 {
			totalKey := "rateLimit:" + subject
			// 初始化
			tb := limiter.New(ctx, rdb)
			allowed, err := tb.Allow(
				ctx,
				totalKey,
				limiter.WithCapacity(rateLimitCapacity(totalMaxCount, duration)),
				limiter.WithRate(int64(totalMaxCount)),
				limiter.WithRequested(duration),
			)

			if err != nil {
				fmt.Println("检查总请求数限制失败:", err.Error())
				abortWithOpenAiMessage(c, http.StatusInternalServerError, "rate_limit_check_failed")
				return
			}

			if !allowed {
				abortModelTotalRateLimited(c, durationMinutes, totalMaxCount)
				return
			}
		}

		var reservation *redisModelSuccessReservation
		completed := false
		defer func() { reservation.Complete(completed && modelRateLimitSucceeded(c)) }()
		if successMaxCount > 0 {
			var err error
			reservation, ctx, err = reserveRedisModelSuccess(ctx, rdb, subject, successMaxCount, duration)
			if err != nil {
				common.SysError("检查成功请求数限制失败: " + err.Error())
				abortWithOpenAiMessage(c, http.StatusInternalServerError, "rate_limit_check_failed")
				return
			}
			if reservation == nil {
				abortModelSuccessRateLimited(c, durationMinutes, successMaxCount)
				return
			}
			c.Request = c.Request.WithContext(ctx)
		}
		c.Next()
		completed = true
	}
}

// 内存限流处理器
func memoryRateLimitHandler(subject string, duration int64, durationMinutes, totalMaxCount, successMaxCount int) gin.HandlerFunc {
	inMemoryRateLimiter.Init(rateLimitExpiration(duration))

	return func(c *gin.Context) {
		if duration <= 0 {
			c.Next()
			return
		}
		totalKey := ModelRequestRateLimitCountMark + subject
		successKey := ModelRequestRateLimitSuccessCountMark + subject

		// 1. 检查总请求数限制（当totalMaxCount为0时跳过）
		if totalMaxCount > 0 && !inMemoryRateLimiter.Request(totalKey, totalMaxCount, duration) {
			abortModelTotalRateLimited(c, durationMinutes, totalMaxCount)
			return
		}

		var reservation *common.RateLimitReservation
		completed := false
		defer func() { reservation.Complete(completed && modelRateLimitSucceeded(c)) }()
		if successMaxCount > 0 {
			reservation = inMemoryRateLimiter.Reserve(successKey, successMaxCount, duration)
			if reservation == nil {
				abortModelSuccessRateLimited(c, durationMinutes, successMaxCount)
				return
			}
		}
		c.Next()
		completed = true
	}
}

// ModelRequestRateLimit 模型请求限流中间件
func ModelRequestRateLimit() func(c *gin.Context) {
	return func(c *gin.Context) {
		settings := setting.GetModelRequestRateLimitSettings()
		if !settings.Enabled {
			c.Next()
			return
		}

		duration := rateLimitDurationSeconds(settings.DurationMinutes)
		totalMaxCount := settings.TotalCount
		successMaxCount := settings.SuccessCount

		// 获取分组
		group := common.GetContextKeyString(c, constant.ContextKeyTokenGroup)
		if group == "" {
			group = common.GetContextKeyString(c, constant.ContextKeyUserGroup)
		}

		// 获取分组的限流配置。配置了独立限流的分组按用户和分组单独计数，
		// 其余分组共用该用户的全局计数
		groupTotalCount, groupSuccessCount, found := setting.GetGroupRateLimit(group)
		if found {
			totalMaxCount = groupTotalCount
			successMaxCount = groupSuccessCount
		}
		subject := modelRateLimitSubject(c.GetInt("id"), group, found)

		// 根据存储类型选择并执行限流处理器
		if common.RedisEnabled {
			redisRateLimitHandler(subject, duration, settings.DurationMinutes, totalMaxCount, successMaxCount)(c)
		} else {
			memoryRateLimitHandler(subject, duration, settings.DurationMinutes, totalMaxCount, successMaxCount)(c)
		}
	}
}

func rateLimitDurationSeconds(durationMinutes int) int64 {
	if durationMinutes <= 0 {
		return 0
	}
	minutes := int64(durationMinutes)
	if minutes > math.MaxInt64/60 {
		return math.MaxInt64
	}
	return minutes * 60
}

func rateLimitCapacity(count int, durationSeconds int64) int64 {
	if count <= 0 || durationSeconds <= 0 {
		return 0
	}
	c := int64(count)
	if c > math.MaxInt64/durationSeconds {
		return math.MaxInt64
	}
	return c * durationSeconds
}

func rateLimitExpiration(durationSeconds int64) time.Duration {
	if durationSeconds <= 0 {
		return 0
	}
	maxSeconds := int64(math.MaxInt64) / int64(time.Second)
	if durationSeconds > maxSeconds {
		return time.Duration(math.MaxInt64)
	}
	return time.Duration(durationSeconds) * time.Second
}
