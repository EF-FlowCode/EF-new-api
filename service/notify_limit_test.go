package service

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"eflow.local/new-api/common"
	"eflow.local/new-api/constant"

	"github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/require"
)

func useNotifyLimitRedis(t *testing.T, limit int, minutes int) *miniredis.Miniredis {
	t.Helper()
	server := miniredis.RunT(t)
	previousRDB, previousLimit, previousMinutes := common.RDB, constant.NotifyLimitCount, constant.NotificationLimitDurationMinute
	common.RDB = redis.NewClient(&redis.Options{Addr: server.Addr()})
	constant.NotifyLimitCount = limit
	constant.NotificationLimitDurationMinute = minutes
	t.Cleanup(func() {
		_ = common.RDB.Close()
		common.RDB = previousRDB
		constant.NotifyLimitCount = previousLimit
		constant.NotificationLimitDurationMinute = previousMinutes
	})
	return server
}

func TestRedisNotifyLimitConcurrentRequestsRespectLimit(t *testing.T) {
	useNotifyLimitRedis(t, 3, 10)

	var allowed int
	var mu sync.Mutex
	var wg sync.WaitGroup
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ok, err := checkRedisLimit(7, "quota")
			require.NoError(t, err)
			if ok {
				mu.Lock()
				allowed++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	require.Equal(t, 3, allowed)
}

func TestRedisNotifyLimitZeroDurationStillCountsAndExpires(t *testing.T) {
	server := useNotifyLimitRedis(t, 2, 0)

	for range 2 {
		ok, err := checkRedisLimit(8, "quota")
		require.NoError(t, err)
		require.True(t, ok)
	}
	ok, err := checkRedisLimit(8, "quota")
	require.NoError(t, err)
	require.False(t, ok, "a zero duration used to create a permanent key that never counted")

	for _, key := range server.Keys() {
		require.Greater(t, server.TTL(key), time.Duration(0), key)
	}
}

// The window is set by NOTIFICATION_LIMIT_DURATION_MINUTE and must not reset
// at the top of each clock hour.
func TestRedisNotifyLimitWindowSpansClockHours(t *testing.T) {
	server := useNotifyLimitRedis(t, 2, 720)

	key := "notify_limit:9:quota"
	require.NoError(t, server.Set(key, "2"))
	server.SetTTL(key, 10*time.Hour)

	steps := []struct {
		name     string
		elapse   time.Duration
		expected bool
	}{
		{name: "limit reached two hours ago still blocks", expected: false},
		{name: "new window opens after the old one ends", elapse: 10*time.Hour + time.Minute, expected: true},
		{name: "second notification in the new window", expected: true},
		{name: "third notification in the new window is blocked", expected: false},
		{name: "new window ends after the configured duration", elapse: 720 * time.Minute, expected: true},
	}
	for _, step := range steps {
		server.FastForward(step.elapse)
		ok, err := checkRedisLimit(9, "quota")
		require.NoError(t, err, step.name)
		require.Equal(t, step.expected, ok, step.name)
	}
}

func TestMemoryNotifyLimitWindowSpansClockHours(t *testing.T) {
	previousLimit, previousMinutes := constant.NotifyLimitCount, constant.NotificationLimitDurationMinute
	constant.NotifyLimitCount, constant.NotificationLimitDurationMinute = 2, 720
	t.Cleanup(func() {
		constant.NotifyLimitCount, constant.NotificationLimitDurationMinute = previousLimit, previousMinutes
	})

	for i, test := range []struct {
		windowStart time.Duration
		expected    bool
	}{
		{windowStart: 2 * time.Hour, expected: false},
		{windowStart: 721 * time.Minute, expected: true},
	} {
		userID := 900001 + i
		key := fmt.Sprintf("%d:%s", userID, "quota")
		notifyLimitStore.Store(key, limitCount{Count: 2, Timestamp: time.Now().Add(-test.windowStart)})
		t.Cleanup(func() { notifyLimitStore.Delete(key) })

		ok, err := checkMemoryLimit(userID, "quota")
		require.NoError(t, err)
		require.Equal(t, test.expected, ok, test.windowStart)
	}
}
