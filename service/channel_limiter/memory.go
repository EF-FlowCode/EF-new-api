package channel_limiter

import (
	"context"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/dto"
)

type memoryEntry struct {
	mu          sync.Mutex
	rpmHits     []int64 // unix-ms 的滑窗时间戳，升序
	concurrency int
}

func (e *memoryEntry) trimRPMLocked(windowStart int64) {
	idx := 0
	for idx < len(e.rpmHits) && e.rpmHits[idx] < windowStart {
		idx++
	}
	if idx > 0 {
		e.rpmHits = e.rpmHits[idx:]
	}
}

type memoryBackend struct {
	mu      sync.Mutex
	entries map[int]*memoryEntry
}

func newMemoryBackend() *memoryBackend {
	return &memoryBackend{entries: make(map[int]*memoryEntry)}
}

func (b *memoryBackend) entry(channelID int) *memoryEntry {
	b.mu.Lock()
	defer b.mu.Unlock()
	e, ok := b.entries[channelID]
	if !ok {
		e = &memoryEntry{}
		b.entries[channelID] = e
	}
	return e
}

func (b *memoryBackend) Acquire(_ context.Context, channelID int, cfg *dto.ChannelRateLimit) (*Token, Decision) {
	e := b.entry(channelID)
	e.mu.Lock()
	defer e.mu.Unlock()

	now := time.Now().UnixMilli()
	windowStart := now - rpmWindowSeconds*1000

	if cfg.RPM > 0 {
		e.trimRPMLocked(windowStart)
		if len(e.rpmHits) >= cfg.RPM {
			return nil, Decision{Allowed: false, Reason: ReasonRPMExceeded}
		}
	}
	if cfg.Concurrency > 0 && e.concurrency >= cfg.Concurrency {
		return nil, Decision{Allowed: false, Reason: ReasonConcurrencyExceeded}
	}

	if cfg.RPM > 0 {
		e.rpmHits = append(e.rpmHits, now)
	}
	concEnabled := cfg.Concurrency > 0
	if concEnabled {
		e.concurrency++
	}
	return &Token{
		release: func() {
			if !concEnabled {
				return
			}
			e.mu.Lock()
			defer e.mu.Unlock()
			if e.concurrency > 0 {
				e.concurrency--
			}
		},
	}, Decision{Allowed: true}
}

func (b *memoryBackend) Peek(_ context.Context, channelID int, cfg *dto.ChannelRateLimit) Decision {
	e := b.entry(channelID)
	e.mu.Lock()
	defer e.mu.Unlock()

	now := time.Now().UnixMilli()
	if cfg.RPM > 0 {
		e.trimRPMLocked(now - rpmWindowSeconds*1000)
		if len(e.rpmHits) >= cfg.RPM {
			return Decision{Allowed: false, Reason: ReasonRPMExceeded}
		}
	}
	if cfg.Concurrency > 0 && e.concurrency >= cfg.Concurrency {
		return Decision{Allowed: false, Reason: ReasonConcurrencyExceeded}
	}
	return Decision{Allowed: true}
}
