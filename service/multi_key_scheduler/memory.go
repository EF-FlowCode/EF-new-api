package multi_key_scheduler

import (
	"context"
	"sync"
	"time"

	"eflow.local/new-api/dto"
)

// memoryBackend 是单实例下的本地后端。多实例部署应改用 Redis backend
// （否则配额按 pod 数分裂，撞限结果不稳定）。
type memoryBackend struct {
	mu       sync.Mutex
	entries  map[string]*memoryEntry
	affinity map[string]memAffinityPin // (channel:affinityValue:model) -> pin
}

// memAffinityPin 内存版的会话级亲和 pin，带毫秒过期。
type memAffinityPin struct {
	keyFp    string
	expireMs int64
}

type memoryEntry struct {
	mu          sync.Mutex
	rpmHits     []int64 // unix-ms 升序的滑动窗口
	tpmHits     []tpmHit
	inFlight    int
	coolUntil   int64 // unix-ms；0 表示未 cool
	last429Ms   int64
	last429Msg  string
	observedMax int
}

type tpmHit struct {
	ts     int64 // unix-ms
	tokens int
}

func newMemoryBackend() *memoryBackend {
	return &memoryBackend{
		entries:  make(map[string]*memoryEntry),
		affinity: make(map[string]memAffinityPin),
	}
}

func memEntryKey(channelID int, keyFp, model string) string {
	// channel:keyFp:model 三元组唯一标识一个调度槽
	return itoa(channelID) + ":" + keyFp + ":" + model
}

func (b *memoryBackend) entry(channelID int, keyFp, model string) *memoryEntry {
	b.mu.Lock()
	defer b.mu.Unlock()
	k := memEntryKey(channelID, keyFp, model)
	e, ok := b.entries[k]
	if !ok {
		e = &memoryEntry{}
		b.entries[k] = e
	}
	return e
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

func (e *memoryEntry) trimTPMLocked(windowStart int64) {
	idx := 0
	for idx < len(e.tpmHits) && e.tpmHits[idx].ts < windowStart {
		idx++
	}
	if idx > 0 {
		e.tpmHits = e.tpmHits[idx:]
	}
}

func (e *memoryEntry) sumTPMLocked() int {
	s := 0
	for _, h := range e.tpmHits {
		s += h.tokens
	}
	return s
}

func (b *memoryBackend) Acquire(_ context.Context, channelID int, keyFp, model string, quota dto.ModelQuota) Decision {
	e := b.entry(channelID, keyFp, model)
	e.mu.Lock()
	defer e.mu.Unlock()

	now := time.Now().UnixMilli()
	if e.coolUntil > now {
		return Decision{Allowed: false, Reason: ReasonCooling, Cooling: true}
	}

	if quota.RPMMax > 0 {
		e.trimRPMLocked(now - rpmWindowSeconds*1000)
		if len(e.rpmHits) >= quota.RPMMax {
			return Decision{Allowed: false, Reason: ReasonRPMExceeded, RPMCount: len(e.rpmHits)}
		}
	}
	if quota.TPMMax > 0 {
		e.trimTPMLocked(now - tpmWindowSeconds*1000)
		if e.sumTPMLocked() >= quota.TPMMax {
			return Decision{Allowed: false, Reason: ReasonTPMExceeded, TPMCount: e.sumTPMLocked()}
		}
	}
	if quota.ConcurrencyMax > 0 && e.inFlight >= quota.ConcurrencyMax {
		return Decision{Allowed: false, Reason: ReasonConcurrencyExceeded, InFlight: e.inFlight}
	}

	if quota.RPMMax > 0 {
		e.rpmHits = append(e.rpmHits, now)
	}
	e.inFlight++
	return Decision{Allowed: true, RPMCount: len(e.rpmHits), InFlight: e.inFlight}
}

func (b *memoryBackend) Peek(_ context.Context, channelID int, keyFp, model string, quota dto.ModelQuota) Decision {
	e := b.entry(channelID, keyFp, model)
	e.mu.Lock()
	defer e.mu.Unlock()

	now := time.Now().UnixMilli()
	if e.coolUntil > now {
		return Decision{Allowed: false, Reason: ReasonCooling, Cooling: true}
	}

	rpmCount := 0
	if quota.RPMMax > 0 {
		e.trimRPMLocked(now - rpmWindowSeconds*1000)
		rpmCount = len(e.rpmHits)
	} else {
		rpmCount = len(e.rpmHits)
	}
	tpmCount := 0
	if quota.TPMMax > 0 {
		e.trimTPMLocked(now - tpmWindowSeconds*1000)
		tpmCount = e.sumTPMLocked()
	} else {
		tpmCount = e.sumTPMLocked()
	}

	dec := Decision{
		Allowed:  true,
		RPMCount: rpmCount,
		TPMCount: tpmCount,
		InFlight: e.inFlight,
	}
	if quota.RPMMax > 0 && rpmCount >= quota.RPMMax {
		dec.Allowed = false
		dec.Reason = ReasonRPMExceeded
	} else if quota.TPMMax > 0 && tpmCount >= quota.TPMMax {
		dec.Allowed = false
		dec.Reason = ReasonTPMExceeded
	} else if quota.ConcurrencyMax > 0 && e.inFlight >= quota.ConcurrencyMax {
		dec.Allowed = false
		dec.Reason = ReasonConcurrencyExceeded
	}
	return dec
}

func (b *memoryBackend) ReportSuccess(_ context.Context, channelID int, keyFp, model string, tokens int) {
	e := b.entry(channelID, keyFp, model)
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.inFlight > 0 {
		e.inFlight--
	}
	if tokens > 0 {
		e.tpmHits = append(e.tpmHits, tpmHit{ts: time.Now().UnixMilli(), tokens: tokens})
	}
}

func (b *memoryBackend) ReportError(_ context.Context, channelID int, keyFp, model string) {
	e := b.entry(channelID, keyFp, model)
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.inFlight > 0 {
		e.inFlight--
	}
}

func (b *memoryBackend) MarkCool(_ context.Context, channelID int, keyFp, model string, seconds int, message string) {
	if seconds <= 0 {
		return
	}
	e := b.entry(channelID, keyFp, model)
	e.mu.Lock()
	defer e.mu.Unlock()
	now := time.Now().UnixMilli()
	e.coolUntil = now + int64(seconds)*1000
	e.last429Ms = now
	if message != "" {
		// 截断防止极端情况下日志/snapshot 体积爆炸
		if len(message) > 256 {
			message = message[:256]
		}
		e.last429Msg = message
	}
	// observed max：本次撞限时的 RPM 计数。先清理过期条目再读数，避免
	// 窗口外的旧 hit 把 observedMax 虚高。
	e.trimRPMLocked(now - rpmWindowSeconds*1000)
	if len(e.rpmHits) > e.observedMax {
		e.observedMax = len(e.rpmHits)
	}
}

func memAffinityKey(channelID int, model, affinityValue string) string {
	return itoa(channelID) + ":" + affinityValue + ":" + model
}

func (b *memoryBackend) GetAffinity(_ context.Context, channelID int, model, affinityValue string) (string, bool) {
	if affinityValue == "" {
		return "", false
	}
	k := memAffinityKey(channelID, model, affinityValue)
	b.mu.Lock()
	defer b.mu.Unlock()
	pin, ok := b.affinity[k]
	if !ok {
		return "", false
	}
	if pin.expireMs <= time.Now().UnixMilli() {
		delete(b.affinity, k)
		return "", false
	}
	return pin.keyFp, true
}

func (b *memoryBackend) SetAffinity(_ context.Context, channelID int, model, affinityValue, keyFp string, ttlSeconds int) {
	if affinityValue == "" || keyFp == "" || ttlSeconds <= 0 {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.affinity[memAffinityKey(channelID, model, affinityValue)] = memAffinityPin{
		keyFp:    keyFp,
		expireMs: time.Now().UnixMilli() + int64(ttlSeconds)*1000,
	}
}

func (b *memoryBackend) Snapshot(_ context.Context, channelID int) []StateEntry {
	b.mu.Lock()
	keys := make([]string, 0, len(b.entries))
	for k := range b.entries {
		keys = append(keys, k)
	}
	b.mu.Unlock()

	prefix := itoa(channelID) + ":"
	out := make([]StateEntry, 0, 8)
	now := time.Now().UnixMilli()
	for _, k := range keys {
		if !startsWith(k, prefix) {
			continue
		}
		// k = "<channelID>:<keyFp>:<model>"
		fp, model, ok := splitTwo(k[len(prefix):], ':')
		if !ok {
			continue
		}
		b.mu.Lock()
		e := b.entries[k]
		b.mu.Unlock()
		if e == nil {
			continue
		}
		e.mu.Lock()
		e.trimRPMLocked(now - rpmWindowSeconds*1000)
		e.trimTPMLocked(now - tpmWindowSeconds*1000)
		entry := StateEntry{
			KeyFp:       fp,
			Model:       model,
			RPMCount:    len(e.rpmHits),
			TPMCount:    e.sumTPMLocked(),
			InFlight:    e.inFlight,
			CoolUntilMs: e.coolUntil,
			Last429AtMs: e.last429Ms,
			Last429Msg:  e.last429Msg,
			ObservedMax: e.observedMax,
		}
		e.mu.Unlock()
		out = append(out, entry)
	}
	return out
}

// itoa / startsWith / splitTwo 是给 entry-key 切分用的迷你 helper，
// 避免引入额外 import 让本文件膨胀。
func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	neg := i < 0
	if neg {
		i = -i
	}
	var b [20]byte
	pos := len(b)
	for i > 0 {
		pos--
		b[pos] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		pos--
		b[pos] = '-'
	}
	return string(b[pos:])
}

func startsWith(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}

func splitTwo(s string, sep byte) (string, string, bool) {
	for i := 0; i < len(s); i++ {
		if s[i] == sep {
			return s[:i], s[i+1:], true
		}
	}
	return "", "", false
}
