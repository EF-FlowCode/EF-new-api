package middleware

import (
	"context"
	"fmt"
	"sync"
	"time"

	"eflow.local/new-api/common"
	"github.com/go-redis/redis/v8"
)

const (
	modelRateLimitLease         = 120 * time.Second
	modelRateLimitRenewInterval = 30 * time.Second
	modelRateLimitRedisTimeout  = 2 * time.Second
)

// All new timestamps use Redis time, so hosts with different clocks share a window.
const modelRateLimitRedisClock = `
local clock = redis.call('TIME')
local now = tonumber(clock[1]) * 1000 + math.floor(tonumber(clock[2]) / 1000)
`

var reserveModelSuccessScript = redis.NewScript(modelRateLimitRedisClock + `
local window = tonumber(ARGV[1])
local limit = tonumber(ARGV[2])
local lease = tonumber(ARGV[3])
local token = ARGV[4]
redis.call('ZREMRANGEBYSCORE', KEYS[1], '-inf', now - window)
redis.call('ZREMRANGEBYSCORE', KEYS[2], '-inf', now)
if redis.call('ZSCORE', KEYS[2], token) then return 1 end
local count = redis.call('ZCARD', KEYS[1]) + redis.call('ZCARD', KEYS[2])
-- Preserve the old version's unexpired successes without changing its List.
-- Old timestamps were formatted in the application's local timezone, despite Z.
for _, stamp in ipairs(redis.call('LRANGE', KEYS[3], 0, -1)) do
  if stamp > ARGV[5] then count = count + 1 end
end
if count >= limit then return 0 end
redis.call('ZADD', KEYS[2], now + lease, token)
redis.call('PEXPIRE', KEYS[2], lease)
return 1
`)

var renewModelSuccessScript = redis.NewScript(modelRateLimitRedisClock + `
local expires = redis.call('ZSCORE', KEYS[1], ARGV[1])
if not expires or tonumber(expires) <= now then return 0 end
redis.call('ZADD', KEYS[1], 'XX', now + tonumber(ARGV[2]), ARGV[1])
redis.call('PEXPIRE', KEYS[1], ARGV[2])
return 1
`)

var completeModelSuccessScript = redis.NewScript(modelRateLimitRedisClock + `
local token = ARGV[1]
local expires = redis.call('ZSCORE', KEYS[2], token)
redis.call('ZREM', KEYS[2], token)
if ARGV[2] ~= '1' then return 1 end
-- A retried completion must neither duplicate a success nor refresh its time.
if redis.call('ZSCORE', KEYS[1], token) then return 1 end
if not expires or tonumber(expires) <= now then return 0 end
local window = tonumber(ARGV[3])
redis.call('ZREMRANGEBYSCORE', KEYS[1], '-inf', now - window)
redis.call('ZADD', KEYS[1], 'NX', now, token)
redis.call('PEXPIRE', KEYS[1], window)
return 1
`)

type redisModelSuccessReservation struct {
	rdb        *redis.Client
	keys       []string
	token      string
	window     time.Duration
	requestCtx context.Context
	cancel     context.CancelFunc
	stop       chan struct{}
	done       chan struct{}
	once       sync.Once
	// lastRenewed is the local send time of the last call that (re)set the
	// lease. After reservation only the heartbeat goroutine touches it.
	lastRenewed time.Time
}

// modelSuccessKeys share one {subject} hash tag so the scripts touching them
// stay in a single Redis Cluster slot (otherwise CROSSSLOT fails closed).
// subject is the user ID, or "userID:group" for a group with its own limit;
// the legacy List of a group subject never existed, so it reads as empty.
func modelSuccessKeys(subject string) []string {
	return []string{
		fmt.Sprintf("rateLimit:v3:MRRLS:{%s}:success", subject),
		fmt.Sprintf("rateLimit:v3:MRRLS:{%s}:pending", subject),
		fmt.Sprintf("rateLimit:MRRLS:{%s}", subject),
	}
}

// nil, nil means the allowance is exhausted. Errors fail closed.
func reserveRedisModelSuccess(ctx context.Context, rdb *redis.Client, subject string, limit int, duration int64) (*redisModelSuccessReservation, context.Context, error) {
	keys := modelSuccessKeys(subject)
	window := rateLimitExpiration(duration)
	token := common.GetUUID()
	reservedAt := time.Now()
	checkCtx, cancelCheck := context.WithTimeout(ctx, modelRateLimitRedisTimeout)
	allowed, err := reserveModelSuccessScript.Run(checkCtx, rdb, keys, window.Milliseconds(), limit,
		modelRateLimitLease.Milliseconds(), token, time.Now().Add(-window).Format(timeFormat)).Bool()
	cancelCheck()
	if err != nil || !allowed {
		return nil, ctx, err
	}
	requestCtx, cancel := context.WithCancel(ctx)
	r := &redisModelSuccessReservation{rdb: rdb, keys: keys, token: token, window: window,
		requestCtx: requestCtx, cancel: cancel, stop: make(chan struct{}), done: make(chan struct{}),
		lastRenewed: reservedAt}
	go r.heartbeat(requestCtx)
	return r, requestCtx, nil
}

func (r *redisModelSuccessReservation) renew(ctx context.Context) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, modelRateLimitRedisTimeout)
	defer cancel()
	return renewModelSuccessScript.Run(ctx, r.rdb, []string{r.keys[1]}, r.token, modelRateLimitLease.Milliseconds()).Bool()
}

// renewOrCancel reports whether the heartbeat should keep running. A transient
// Redis error is retried on the next tick because the server-side lease is still
// valid; the request is canceled only when Redis reports the lease gone, or when
// no renewal has succeeded for a whole lease period.
func (r *redisModelSuccessReservation) renewOrCancel(ctx context.Context) bool {
	sentAt := time.Now()
	ok, err := r.renew(ctx)
	if err == nil && ok {
		r.lastRenewed = sentAt
		return true
	}
	if err == nil {
		common.SysError("model success rate-limit lease lost: lease expired in redis")
		r.cancel()
		return false
	}
	if sentAt.Sub(r.lastRenewed) >= modelRateLimitLease {
		common.SysError(fmt.Sprintf("model success rate-limit lease lost: no successful renewal within %s: %v", modelRateLimitLease, err))
		r.cancel()
		return false
	}
	common.SysError(fmt.Sprintf("model success rate-limit lease renewal failed, will retry: %v", err))
	return true
}

func (r *redisModelSuccessReservation) heartbeat(ctx context.Context) {
	defer close(r.done)
	ticker := time.NewTicker(modelRateLimitRenewInterval)
	defer ticker.Stop()
	for {
		select {
		case <-r.stop:
			return
		case <-ctx.Done():
			return
		case <-ticker.C:
			if !r.renewOrCancel(ctx) {
				return
			}
		}
	}
}

func (r *redisModelSuccessReservation) Complete(success bool) {
	if r == nil {
		return
	}
	r.once.Do(func() {
		close(r.stop)
		<-r.done
		success = success && r.requestCtx.Err() == nil
		defer r.cancel()
		// Cleanup must work even after the client canceled or the lease was lost.
		ctx, cancel := context.WithTimeout(context.Background(), modelRateLimitRedisTimeout)
		defer cancel()
		ok, err := completeModelSuccessScript.Run(ctx, r.rdb, r.keys[:2], r.token, success, r.window.Milliseconds()).Bool()
		if err != nil || !ok {
			common.SysError(fmt.Sprintf("model success rate-limit completion failed (lease will expire): %v", err))
		}
	})
}
