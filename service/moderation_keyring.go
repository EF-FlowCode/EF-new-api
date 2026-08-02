package service

import (
	"sync"
	"time"
)

// ModerationKeyRing rotates a list of OpenAI-compatible API keys with
// per-key cooldown timestamps. The moderation worker calls NextKey() before
// each request; on 429/5xx the worker reports MarkFailed(idx, retryAfter)
// and the next NextKey() call skips that key until the cooldown expires.
//
// The ring is intentionally simple: round-robin polling, no priority,
// no health stats beyond cooldown. The OpenAI moderation endpoint is
// effectively free, so fairness across keys is more important than picking
// the "best" one.
type ModerationKeyRing struct {
	mu       sync.Mutex
	keys     []string
	cooldown []int64
	cursor   int
	now      func() time.Time // injectable for tests
}

func NewModerationKeyRing(keys []string) *ModerationKeyRing {
	r := &ModerationKeyRing{now: time.Now}
	r.Reset(keys)
	return r
}

// Reset replaces the key list and clears all cooldowns. Called on config
// changes so admins can rotate keys without restarting the process.
func (r *ModerationKeyRing) Reset(keys []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.keys = append([]string(nil), keys...)
	r.cooldown = make([]int64, len(r.keys))
	r.cursor = 0
}

// Size returns how many keys are currently configured.
func (r *ModerationKeyRing) Size() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.keys)
}

// NextKey returns the next available key in polling order. If every key is
// currently cooling down, it returns ok=false so the caller can drop or
// requeue the event without busy-waiting.
func (r *ModerationKeyRing) NextKey() (key string, idx int, ok bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := len(r.keys)
	if n == 0 {
		return "", -1, false
	}
	now := r.now().Unix()
	for i := 0; i < n; i++ {
		i := (r.cursor + i) % n
		if r.cooldown[i] <= now {
			r.cursor = (i + 1) % n
			return r.keys[i], i, true
		}
	}
	return "", -1, false
}

// MarkFailed sets the cooldown for the given key index. retryAfter is the
// duration to wait before retrying; if zero or negative we use 1 second so
// the worker can move on without spinning. Out-of-range indexes are ignored.
func (r *ModerationKeyRing) MarkFailed(idx int, retryAfter time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if idx < 0 || idx >= len(r.cooldown) {
		return
	}
	if retryAfter < time.Second {
		retryAfter = time.Second
	}
	r.cooldown[idx] = r.now().Add(retryAfter).Unix()
}

// CooldownAt exposes the per-key cooldown unix timestamp for tests / debug.
func (r *ModerationKeyRing) CooldownAt(idx int) int64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	if idx < 0 || idx >= len(r.cooldown) {
		return 0
	}
	return r.cooldown[idx]
}
