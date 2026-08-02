package channel_limiter

import (
	"context"
	_ "embed"
	"fmt"
	"math/rand"
	"strconv"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/go-redis/redis/v8"
)

//go:embed lua/acquire.lua
var redisAcquireScript string

//go:embed lua/release.lua
var redisReleaseScript string

//go:embed lua/peek.lua
var redisPeekScript string

type redisBackend struct {
	client     *redis.Client
	acquireSHA string
	releaseSHA string
	peekSHA    string
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
	pSHA, err := common.RDB.ScriptLoad(ctx, redisPeekScript).Result()
	if err != nil {
		return nil, fmt.Errorf("load peek script: %w", err)
	}
	return &redisBackend{
		client:     common.RDB,
		acquireSHA: aSHA,
		releaseSHA: rSHA,
		peekSHA:    pSHA,
	}, nil
}

func (b *redisBackend) keys(channelID int) (rpmKey, concKey string) {
	return fmt.Sprintf("chnlrl:rpm:%d", channelID), fmt.Sprintf("chnlrl:conc:%d", channelID)
}

func uniqueToken() string {
	return strconv.FormatInt(time.Now().UnixNano(), 36) + ":" + strconv.FormatInt(rand.Int63(), 36)
}

func (b *redisBackend) Acquire(ctx context.Context, channelID int, cfg *dto.ChannelRateLimit) (*Token, Decision) {
	rpmKey, concKey := b.keys(channelID)
	res, err := b.client.EvalSha(ctx, b.acquireSHA,
		[]string{rpmKey, concKey},
		cfg.RPM, rpmWindowSeconds, cfg.Concurrency, uniqueToken(),
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
				if _, e := b.client.EvalSha(rctx, b.releaseSHA, []string{concKey}).Result(); e != nil {
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

func (b *redisBackend) Peek(ctx context.Context, channelID int, cfg *dto.ChannelRateLimit) Decision {
	rpmKey, concKey := b.keys(channelID)
	res, err := b.client.EvalSha(ctx, b.peekSHA,
		[]string{rpmKey, concKey},
		cfg.RPM, rpmWindowSeconds, cfg.Concurrency,
	).StringSlice()
	if err != nil {
		return Decision{Allowed: true}
	}
	if len(res) > 0 && res[0] == "1" {
		return Decision{Allowed: true}
	}
	reason := ReasonRPMExceeded
	if len(res) > 1 {
		reason = res[1]
	}
	return Decision{Allowed: false, Reason: reason}
}
