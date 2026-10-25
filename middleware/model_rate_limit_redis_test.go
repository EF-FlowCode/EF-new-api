package middleware

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"eflow.local/new-api/common"
	"github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/require"
)

// Optional integration check against an isolated Redis, never the application's
// configured client. Unique keys are removed without flushing the server.
func TestRedisModelSuccessRealServer(t *testing.T) {
	addr := os.Getenv("MODEL_RATE_LIMIT_TEST_REDIS")
	if addr == "" {
		t.Skip("isolated Redis address not configured")
	}
	clients := []*redis.Client{
		redis.NewClient(&redis.Options{Addr: addr}),
		redis.NewClient(&redis.Options{Addr: addr}),
	}
	user := "rate-limit-test-" + common.GetUUID()
	ctx := context.Background()
	t.Cleanup(func() {
		_ = clients[0].Del(ctx, "rateLimit:v3:MRRLS:{"+user+"}:success", "rateLimit:v3:MRRLS:{"+user+"}:pending").Err()
		for _, c := range clients {
			_ = c.Close()
		}
	})
	type result struct {
		r   *redisModelSuccessReservation
		err error
	}
	results := make(chan result, 32)
	for i := range 32 {
		go func() {
			r, _, err := reserveRedisModelSuccess(ctx, clients[i%2], user, 3, 1)
			results <- result{r, err}
		}()
	}
	var admitted []*redisModelSuccessReservation
	for range 32 {
		result := <-results
		require.NoError(t, result.err)
		if result.r != nil {
			admitted = append(admitted, result.r)
			t.Cleanup(func() { result.r.Complete(false) })
		}
	}
	require.Len(t, admitted, 3)
	for _, r := range admitted {
		require.True(t, r.renewOrCancel(r.requestCtx))
		r.Complete(true)
		// Also exercise script-level idempotence, as on a transport retry.
		before := clients[0].ZScore(ctx, r.keys[0], r.token).Val()
		require.NoError(t, completeModelSuccessScript.Run(ctx, clients[1], r.keys[:2], r.token, true, r.window.Milliseconds()).Err())
		require.Equal(t, before, clients[0].ZScore(ctx, r.keys[0], r.token).Val())
	}
	require.Nil(t, reserveModelSuccessForTest(t, clients[1], user, 3, 1))
	require.Eventually(t, func() bool { return clients[0].Exists(ctx, admitted[0].keys[0]).Val() == 0 }, 3*time.Second, 50*time.Millisecond)
	require.NotNil(t, reserveModelSuccessForTest(t, clients[1], user, 3, 1))
}

func modelSuccessRedis(t *testing.T) (*miniredis.Miniredis, *redis.Client) {
	t.Helper()
	s := miniredis.RunT(t)
	c := redis.NewClient(&redis.Options{Addr: s.Addr(), MaxRetries: -1})
	t.Cleanup(func() { _ = c.Close() })
	return s, c
}

func reserveModelSuccessForTest(t *testing.T, c *redis.Client, user string, limit int, duration int64) *redisModelSuccessReservation {
	t.Helper()
	r, _, err := reserveRedisModelSuccess(context.Background(), c, user, limit, duration)
	require.NoError(t, err)
	if r != nil {
		t.Cleanup(func() { r.Complete(false) })
	}
	return r
}

func TestRedisModelSuccessWindowAndRenewal(t *testing.T) {
	s, c := modelSuccessRedis(t)
	now := time.Now()
	s.SetTime(now)
	r := reserveModelSuccessForTest(t, c, "42", 1, 60)
	require.NotNil(t, r)
	for _, seconds := range []int{30, 60, 90, 120, 150} {
		s.SetTime(now.Add(time.Duration(seconds) * time.Second))
		s.FastForward(30 * time.Second)
		require.True(t, r.renewOrCancel(r.requestCtx))
		require.Nil(t, reserveModelSuccessForTest(t, c, "42", 1, 60))
	}
	r.Complete(true)
	stamp, err := c.ZScore(context.Background(), r.keys[0], r.token).Result()
	require.NoError(t, err)
	// A duplicate completion cannot release another request or shift its window.
	s.SetTime(now.Add(180 * time.Second))
	s.FastForward(30 * time.Second)
	r.Complete(true)
	stampAgain, err := c.ZScore(context.Background(), r.keys[0], r.token).Result()
	require.NoError(t, err)
	require.Equal(t, stamp, stampAgain)
	require.Nil(t, reserveModelSuccessForTest(t, c, "42", 1, 60))
	s.SetTime(now.Add(210 * time.Second))
	s.FastForward(30 * time.Second)
	require.NotNil(t, reserveModelSuccessForTest(t, c, "42", 1, 60))
}

func TestRedisModelSuccessExpiredLeaseCannotRevive(t *testing.T) {
	s, c := modelSuccessRedis(t)
	now := time.Now()
	s.SetTime(now)
	r := reserveModelSuccessForTest(t, c, "42", 1, 60)
	require.NotNil(t, r)
	// Keep the key alive to prove expiration is checked per member, not only TTL.
	require.NoError(t, c.PExpire(context.Background(), r.keys[1], time.Hour).Err())
	s.SetTime(now.Add(modelRateLimitLease))
	require.False(t, r.renewOrCancel(r.requestCtx))
	require.ErrorIs(t, r.requestCtx.Err(), context.Canceled)
	next := reserveModelSuccessForTest(t, c, "42", 1, 60)
	require.NotNil(t, next)
	r.Complete(true)
	require.EqualValues(t, 0, c.ZCard(context.Background(), r.keys[0]).Val())
	require.Nil(t, reserveModelSuccessForTest(t, c, "42", 1, 60))
	next.Complete(false)
	require.NotNil(t, reserveModelSuccessForTest(t, c, "42", 1, 60))
}

func TestRedisModelSuccessLegacyWindow(t *testing.T) {
	_, c := modelSuccessRedis(t)
	// The old writer used local wall time even though the format ends with Z.
	oldLocal := time.Local
	time.Local = time.FixedZone("UTC+8", 8*60*60)
	t.Cleanup(func() { time.Local = oldLocal })
	ctx := context.Background()
	key := "rateLimit:MRRLS:{42}"
	now := time.Now()
	recent, expired := now.Format(timeFormat), now.Add(-2*time.Minute).Format(timeFormat)
	require.NoError(t, c.RPush(ctx, key, recent, expired).Err())
	require.NoError(t, c.Expire(ctx, key, time.Minute).Err())
	require.Nil(t, reserveModelSuccessForTest(t, c, "42", 1, 60))
	r := reserveModelSuccessForTest(t, c, "42", 2, 60)
	require.NotNil(t, r)
	r.Complete(true)
	require.Nil(t, reserveModelSuccessForTest(t, c, "42", 2, 60))
	values, err := c.LRange(ctx, key, 0, -1).Result()
	require.NoError(t, err)
	require.Equal(t, []string{recent, expired}, values)
	require.NotNil(t, reserveModelSuccessForTest(t, c, "different-user", 1, 60))
}

func TestRedisModelSuccessCancellationAndErrors(t *testing.T) {
	s, c := modelSuccessRedis(t)
	ctx, cancel := context.WithCancel(context.Background())
	r, _, err := reserveRedisModelSuccess(ctx, c, "42", 1, 60)
	require.NoError(t, err)
	require.NotNil(t, r)
	cancel()
	r.Complete(true)
	require.EqualValues(t, 0, c.Exists(context.Background(), r.keys[0], r.keys[1]).Val())
	r = reserveModelSuccessForTest(t, c, "42", 1, 60)
	require.NotNil(t, r)
	s.SetError("ERR unavailable")
	// A transient failure keeps the request alive while the lease is still valid.
	require.True(t, r.renewOrCancel(r.requestCtx))
	require.NoError(t, r.requestCtx.Err())
	// Only a whole lease period without a successful renewal cancels it.
	r.lastRenewed = time.Now().Add(-modelRateLimitLease)
	require.False(t, r.renewOrCancel(r.requestCtx))
	require.ErrorIs(t, r.requestCtx.Err(), context.Canceled)
	denied, _, err := reserveRedisModelSuccess(context.Background(), c, "43", 1, 60)
	require.Error(t, err)
	require.Nil(t, denied)
	// A failed release is eventually reclaimed by the pending lease.
	r.Complete(false)
	s.SetError("")
	s.FastForward(modelRateLimitLease)
	require.NotNil(t, reserveModelSuccessForTest(t, c, "42", 1, 60))
}

func TestRedisModelSuccessKeysShareHashSlot(t *testing.T) {
	// A group with its own limit is a separate subject whose keys still share a slot.
	for _, subject := range []string{"42", modelRateLimitSubject(42, "vip", true)} {
		for _, key := range modelSuccessKeys(subject) {
			start, end := strings.IndexByte(key, '{'), strings.IndexByte(key, '}')
			require.True(t, start >= 0 && end > start+1, key)
			require.Equal(t, subject, key[start+1:end], key)
		}
	}
	require.Equal(t, "rateLimit:v3:MRRLS:{42:vip}:success", modelSuccessKeys(modelRateLimitSubject(42, "vip", true))[0])
	require.Equal(t, "42", modelRateLimitSubject(42, "vip", false))
}
