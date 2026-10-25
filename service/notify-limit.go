package service

import (
	"context"
	"fmt"
	"sync"
	"time"

	"eflow.local/new-api/common"
	"eflow.local/new-api/constant"
	gopool "eflow.local/new-api/internal/drainpool"

	"github.com/go-redis/redis/v8"
)

// notifyLimitStore is used for in-memory rate limiting when Redis is disabled
var (
	notifyLimitStore sync.Map
	cleanupOnce      sync.Once
)

type limitCount struct {
	Count     int
	Timestamp time.Time
}

func getDuration() time.Duration {
	minute := constant.NotificationLimitDurationMinute
	return time.Duration(minute) * time.Minute
}

// startCleanupTask starts a background task to clean up expired entries
func startCleanupTask() {
	gopool.Go(func() {
		for {
			time.Sleep(time.Hour)
			now := time.Now()
			notifyLimitStore.Range(func(key, value any) bool {
				if limit, ok := value.(limitCount); ok {
					if now.Sub(limit.Timestamp) >= getDuration() {
						notifyLimitStore.Delete(key)
					}
				}
				return true
			})
		}
	})
}

// CheckNotificationLimit checks if the user has exceeded their notification limit
// Returns true if the user can send notification, false if limit exceeded
func CheckNotificationLimit(userId int, notifyType string) (bool, error) {
	if common.RedisEnabled {
		return checkRedisLimit(userId, notifyType)
	}
	return checkMemoryLimit(userId, notifyType)
}

// notifyLimitScript counts one notification only while under the limit, and
// gives a new bucket its expiry in the same step so no key is left permanent.
var notifyLimitScript = redis.NewScript(`
local current = tonumber(redis.call('GET', KEYS[1]) or '0')
if current >= tonumber(ARGV[1]) then
	return 0
end
if redis.call('INCR', KEYS[1]) == 1 then
	redis.call('PEXPIRE', KEYS[1], ARGV[2])
end
return 1
`)

func checkRedisLimit(userId int, notifyType string) (bool, error) {
	// The window starts at the first notification and lasts getDuration();
	// keys are not bucketed by clock hour, which reset the count every hour.
	key := fmt.Sprintf("notify_limit:%d:%s", userId, notifyType)
	expiry := getDuration()
	if expiry <= 0 {
		// Never leave a counter without an expiry.
		expiry = time.Hour
	}
	allowed, err := notifyLimitScript.Run(context.Background(), common.RDB, []string{key},
		constant.NotifyLimitCount, expiry.Milliseconds()).Int()
	if err != nil {
		return false, fmt.Errorf("failed to check notification limit: %w", err)
	}
	return allowed == 1, nil
}

func checkMemoryLimit(userId int, notifyType string) (bool, error) {
	// Ensure cleanup task is started
	cleanupOnce.Do(startCleanupTask)

	key := fmt.Sprintf("%d:%s", userId, notifyType)
	now := time.Now()

	// Get current limit count or initialize new one
	var currentLimit limitCount
	if value, ok := notifyLimitStore.Load(key); ok {
		currentLimit = value.(limitCount)
		// Check if the entry has expired
		if now.Sub(currentLimit.Timestamp) >= getDuration() {
			currentLimit = limitCount{Count: 0, Timestamp: now}
		}
	} else {
		currentLimit = limitCount{Count: 0, Timestamp: now}
	}

	// Increment count
	currentLimit.Count++

	// Check against limits
	limit := constant.NotifyLimitCount

	// Store updated count
	notifyLimitStore.Store(key, currentLimit)

	return currentLimit.Count <= limit, nil
}
