package channel_limiter

import (
	"context"
	_ "embed"
	"fmt"
	"math/rand"
	"strconv"
	"time"

	"eflow.local/new-api/common"
	"eflow.local/new-api/dto"
	"github.com/go-redis/redis/v8"
)

//go:embed lua/acquire.lua
var redisAcquireScript string

//go:embed lua/release.lua
var redisReleaseScript string

type redisBackend struct {
	client     *redis.Client
	acquireSHA string
	releaseSHA string
}

func newRedisBackend() (*redisBackend, error) {
	if common.RDB == nil {
		return nil, fmt.Errorf("redis client not initialized")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	aSHA, err := common.RDB.ScriptLoad(ctx, redisAcquireScript).Result()
	if err != nil {
		return nil, fmt.Errorf("load acquire script: %w", err)
	}
	rSHA, err := common.RDB.ScriptLoad(ctx, redisReleaseScript).Result()
	if err != nil {
		return nil, fmt.Errorf("load release script: %w", err)
	}
	return &redisBackend{
		client:     common.RDB,
		acquireSHA: aSHA,
		releaseSHA: rSHA,
	}, nil
}

func (b *redisBackend) keys(channelID int) (rpmKey, concKey string) {
	return fmt.Sprintf("chnlrl:rpm:%d", channelID), fmt.Sprintf("chnlrl:conc:%d", channelID)
}

// concurrencyTTLSeconds must outlive the longest in-flight request: a shorter
// expiry (it used to be 60s) resets the counter while long streams still hold
// their slots, so the concurrency cap stops working until they finish.
func concurrencyTTLSeconds() int {
	if common.RelayTimeout > 0 {
		return common.RelayTimeout + 60
	}
	return 3600
}

func uniqueToken() string {
	return strconv.FormatInt(time.Now().UnixNano(), 36) + ":" + strconv.FormatInt(rand.Int63(), 36)
}

func (b *redisBackend) Acquire(ctx context.Context, channelID int, cfg *dto.ChannelRateLimit) (*Token, Decision) {
	rpmKey, concKey := b.keys(channelID)
	res, err := b.client.EvalSha(ctx, b.acquireSHA,
		[]string{rpmKey, concKey},
		cfg.RPM, rpmWindowSeconds, cfg.Concurrency, uniqueToken(), concurrencyTTLSeconds(),
	).StringSlice()
	if err != nil {
		// Redis 故障时 fail-open，避免限流模块拖垮整个网关
		common.SysError(fmt.Sprintf("channel_limiter: redis acquire failed channel=%d err=%v", channelID, err))
		return nil, Decision{Allowed: true}
	}
	if len(res) > 0 && res[0] == "1" {
		hasConcurrency := cfg.Concurrency > 0
		return &Token{
			release: func() {
				if !hasConcurrency {
					return
				}
				rctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
				defer cancel()
				if _, e := b.client.EvalSha(rctx, b.releaseSHA, []string{concKey}, concurrencyTTLSeconds()).Result(); e != nil {
					common.SysError(fmt.Sprintf("channel_limiter: redis release failed channel=%d err=%v", channelID, e))
				}
			},
		}, Decision{Allowed: true}
	}
	reason := ReasonRPMExceeded
	if len(res) > 1 {
		reason = res[1]
	}
	return nil, Decision{Allowed: false, Reason: reason}
}
