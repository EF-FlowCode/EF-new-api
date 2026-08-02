// Package channel_limiter 提供渠道级 RPM / 并发 限流能力。
//
// 三种满载策略：
//   - skip   => 调用方继续 retry 同分组的其他渠道
//   - queue  => 串行排队等待，最多 QueueMaxWaitMs；队列满 (QueueDepth) 直接回退
//   - reject => 直接拒绝 (HTTP 429)
//
// 优先使用 Redis 后端（多实例精确）；无 Redis 时降级到内存后端，仅在单实例内生效，
// UI 层会向运维提示该警告。
package channel_limiter

import (
	"context"
	"fmt"
	"math/rand"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
)

// 满载策略常量
const (
	OnLimitSkip   = "skip"
	OnLimitQueue  = "queue"
	OnLimitReject = "reject"
)

// 拒绝原因常量
const (
	ReasonRPMExceeded         = "rpm_exceeded"
	ReasonConcurrencyExceeded = "concurrency_exceeded"
	ReasonQueueFull           = "queue_full"
	ReasonQueueTimeout        = "queue_timeout"
)

const (
	defaultQueueMaxWaitMs = 2000
	defaultQueueDepth     = 20
	rpmWindowSeconds      = 60
)

// Decision 表示一次限流判定结果。
type Decision struct {
	Allowed bool
	Reason  string
}

// Token 是一次成功 Acquire 的句柄。调用方完成上游请求后必须 Release 一次（多次调用幂等）。
type Token struct {
	once    sync.Once
	release func()
}

// Release 释放本次预占的并发槽位（RPM 槽位会随窗口自然过期，无需释放）。
func (t *Token) Release() {
	if t == nil {
		return
	}
	t.once.Do(func() {
		if t.release != nil {
			t.release()
		}
	})
}

// IsActive 判定该限流配置是否当前有效。
func IsActive(cfg *dto.ChannelRateLimit) bool {
	return cfg != nil && cfg.Enabled && (cfg.RPM > 0 || cfg.Concurrency > 0)
}

// resolvedConfig 应用默认值，避免 0/空字符串的不直观语义渗入后端。
func resolvedConfig(cfg *dto.ChannelRateLimit) *dto.ChannelRateLimit {
	if cfg == nil {
		return nil
	}
	out := *cfg
	switch out.OnLimit {
	case OnLimitSkip, OnLimitQueue, OnLimitReject:
	default:
		out.OnLimit = OnLimitSkip
	}
	if out.QueueDepth <= 0 {
		out.QueueDepth = defaultQueueDepth
	}
	if out.QueueMaxWaitMs <= 0 {
		out.QueueMaxWaitMs = defaultQueueMaxWaitMs
	}
	return &out
}

// CheckOnly 在不预占的情况下，快速判定渠道是否仍有容量。
// Allowed=true 不保证后续 Acquire 也成功（可能竞态），但能廉价过滤明显达限的渠道。
func CheckOnly(ctx context.Context, channelID int, cfg *dto.ChannelRateLimit) Decision {
	resolved := resolvedConfig(cfg)
	if !IsActive(resolved) {
		return Decision{Allowed: true}
	}
	return getBackend().Peek(ctx, channelID, resolved)
}

// Acquire 预占一个渠道槽位。Allowed=true 时返回的 Token 必须 Release。
// 若 OnLimit=queue，本函数会阻塞最多 QueueMaxWaitMs。
func Acquire(ctx context.Context, channelID int, cfg *dto.ChannelRateLimit) (*Token, Decision) {
	resolved := resolvedConfig(cfg)
	if !IsActive(resolved) {
		return nil, Decision{Allowed: true}
	}

	backend := getBackend()
	if resolved.OnLimit != OnLimitQueue {
		return backend.Acquire(ctx, channelID, resolved)
	}

	// queue 模式：限制等待方数量，避免内存膨胀
	if !enterQueue(channelID, resolved.QueueDepth) {
		return nil, Decision{Allowed: false, Reason: ReasonQueueFull}
	}
	defer leaveQueue(channelID)

	deadline := time.Now().Add(time.Duration(resolved.QueueMaxWaitMs) * time.Millisecond)
	backoff := 30 * time.Millisecond
	for {
		token, decision := backend.Acquire(ctx, channelID, resolved)
		if decision.Allowed {
			return token, decision
		}
		if time.Now().After(deadline) {
			return nil, Decision{Allowed: false, Reason: ReasonQueueTimeout}
		}
		jitter := time.Duration(rand.Intn(20)) * time.Millisecond
		select {
		case <-ctx.Done():
			return nil, Decision{Allowed: false, Reason: ReasonQueueTimeout}
		case <-time.After(backoff + jitter):
		}
		if backoff < 200*time.Millisecond {
			backoff *= 2
		}
	}
}

// 队列等待计数（仅本进程内有效，因为等待本就是进程局部行为）
var (
	queueWaitersMu sync.Mutex
	queueWaiters   = make(map[int]int)
)

func enterQueue(channelID, depth int) bool {
	queueWaitersMu.Lock()
	defer queueWaitersMu.Unlock()
	if queueWaiters[channelID] >= depth {
		return false
	}
	queueWaiters[channelID]++
	return true
}

func leaveQueue(channelID int) {
	queueWaitersMu.Lock()
	defer queueWaitersMu.Unlock()
	queueWaiters[channelID]--
	if queueWaiters[channelID] <= 0 {
		delete(queueWaiters, channelID)
	}
}

// backend 抽象：当前提供 Redis（首选）与内存（单实例）两种实现。
type backend interface {
	Acquire(ctx context.Context, channelID int, cfg *dto.ChannelRateLimit) (*Token, Decision)
	Peek(ctx context.Context, channelID int, cfg *dto.ChannelRateLimit) Decision
}

var (
	backendOnce sync.Once
	backendImpl backend
)

func getBackend() backend {
	backendOnce.Do(func() {
		if common.RedisEnabled {
			impl, err := newRedisBackend()
			if err != nil {
				common.SysError(fmt.Sprintf("channel_limiter: redis backend init failed, fallback to memory: %v", err))
				backendImpl = newMemoryBackend()
				return
			}
			backendImpl = impl
			common.SysLog("channel_limiter: using redis backend")
		} else {
			backendImpl = newMemoryBackend()
			common.SysLog("channel_limiter: redis disabled, using in-memory backend (single-instance only)")
		}
	})
	return backendImpl
}
