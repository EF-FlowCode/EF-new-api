package common

import (
	"container/list"
	"sync"
	"time"
)

// rateLimitRequest stores one accepted request in a key's sliding window.
type rateLimitRequest struct {
	next      *rateLimitRequest
	timestamp int64
}

// rateLimitQueue keeps accepted requests ordered from oldest to newest.
type rateLimitQueue struct {
	head   *rateLimitRequest
	tail   *rateLimitRequest
	length int
}

// append adds an accepted request without preallocating for the configured limit.
func (q *rateLimitQueue) append(timestamp int64) {
	request := &rateLimitRequest{timestamp: timestamp}
	if q.tail == nil {
		q.head = request
		q.tail = request
	} else {
		q.tail.next = request
		q.tail = request
	}
	q.length++
}

// removeExpired releases every expired request at the front of the queue.
func (q *rateLimitQueue) removeExpired(now int64, duration int64) {
	if q.head == nil || now-q.head.timestamp < duration {
		return
	}

	// Requests are time ordered, so an expired tail means the whole queue expired.
	if now-q.tail.timestamp >= duration {
		q.clear()
		return
	}

	for now-q.head.timestamp >= duration {
		expired := q.head
		q.head = expired.next
		expired.next = nil
		q.length--
	}
}

// clear releases the whole request chain when its key expires.
func (q *rateLimitQueue) clear() {
	q.head = nil
	q.tail = nil
	q.length = 0
}

// rateLimitEntry is both a rate-limit bucket and a node in the key-level LRU.
type rateLimitEntry struct {
	lastActive  time.Time
	retainUntil time.Time
	element     *list.Element
	requests    rateLimitQueue
	key         string
}

// InMemoryRateLimiter implements a sliding-window limiter with idle-key eviction.
type InMemoryRateLimiter struct {
	store              map[string]*rateLimitEntry
	lru                *list.List
	reservations       map[string]int
	mutex              sync.Mutex
	expirationDuration time.Duration
}

// Init initializes the limiter once. Repeated calls leave the first configuration unchanged.
func (l *InMemoryRateLimiter) Init(expirationDuration time.Duration) {
	l.mutex.Lock()
	defer l.mutex.Unlock()

	if l.store != nil {
		return
	}

	l.store = make(map[string]*rateLimitEntry)
	l.lru = list.New()
	l.reservations = make(map[string]int)
	l.expirationDuration = expirationDuration
	if expirationDuration > 0 {
		go l.clearExpiredItems(time.NewTicker(expirationDuration).C)
	}
}

// clearExpiredItems periodically removes expired entries from the LRU tail.
func (l *InMemoryRateLimiter) clearExpiredItems(ticks <-chan time.Time) {
	for now := range ticks {
		l.deleteExpiredEntries(now)
	}
}

// deleteExpiredEntries walks only the oldest LRU entries and stops at the first active key.
func (l *InMemoryRateLimiter) deleteExpiredEntries(now time.Time) {
	if l.expirationDuration <= 0 {
		return
	}

	l.mutex.Lock()
	defer l.mutex.Unlock()

	for oldest := l.lru.Back(); oldest != nil; {
		previous := oldest.Prev()
		entry := oldest.Value.(*rateLimitEntry)
		if now.Sub(entry.lastActive) < l.expirationDuration {
			return
		}
		if now.Before(entry.retainUntil) {
			oldest = previous
			continue
		}

		delete(l.store, entry.key)
		l.lru.Remove(oldest)
		entry.element = nil
		entry.requests.clear()
		oldest = previous
	}
}

// touchEntry requires the mutex. Reservations survive idle bucket eviction.
func (l *InMemoryRateLimiter) touchEntry(key string, now time.Time) *rateLimitEntry {
	entry := l.store[key]
	if entry == nil {
		entry = &rateLimitEntry{key: key}
		entry.element = l.lru.PushFront(entry)
		l.store[key] = entry
	}
	entry.lastActive = now
	l.lru.MoveToFront(entry.element)
	return entry
}

type RateLimitReservation struct {
	limiter  *InMemoryRateLimiter
	key      string
	duration int64
	once     sync.Once
}

// Reserve atomically counts in-flight requests against the success allowance.
// The caller skips disabled limits; nil means the allowance is exhausted.
func (l *InMemoryRateLimiter) Reserve(key string, maxRequests int, duration int64) *RateLimitReservation {
	l.mutex.Lock()
	defer l.mutex.Unlock()
	now := time.Now()
	entry := l.touchEntry(key, now)
	entry.requests.removeExpired(now.Unix(), duration)
	if maxRequests <= 0 || entry.requests.length+l.reservations[key] >= maxRequests {
		return nil
	}
	l.reservations[key]++
	return &RateLimitReservation{limiter: l, key: key, duration: duration}
}

// Complete releases once; only success is recorded, at completion time.
func (r *RateLimitReservation) Complete(success bool) {
	if r == nil {
		return
	}
	r.once.Do(func() {
		l := r.limiter
		l.mutex.Lock()
		defer l.mutex.Unlock()
		l.reservations[r.key]--
		if l.reservations[r.key] == 0 {
			delete(l.reservations, r.key)
		}
		if success {
			now := time.Now()
			entry := l.touchEntry(r.key, now)
			entry.requests.removeExpired(now.Unix(), r.duration)
			entry.requests.append(now.Unix())
			// Saturate before converting user-configured seconds to Duration.
			seconds := min(r.duration, int64((1<<63-1)/int64(time.Second)))
			until := now.Add(time.Duration(seconds) * time.Second)
			if until.After(entry.retainUntil) {
				entry.retainUntil = until
			}
		}
	})
}

// Allow reports whether key is under maxRequestNum in the window without
// recording a request. Callers record separately once the outcome is known.
func (l *InMemoryRateLimiter) Allow(key string, maxRequestNum int, duration int64) bool {
	l.mutex.Lock()
	defer l.mutex.Unlock()

	entry, ok := l.store[key]
	if !ok {
		return maxRequestNum > 0
	}
	entry.requests.removeExpired(time.Now().Unix(), duration)
	return entry.requests.length < maxRequestNum
}

// Request parameter duration's unit is seconds
func (l *InMemoryRateLimiter) Request(key string, maxRequestNum int, duration int64) bool {
	l.mutex.Lock()
	defer l.mutex.Unlock()

	now := time.Now()
	entry, ok := l.store[key]
	if !ok {
		entry = &rateLimitEntry{
			key:        key,
			lastActive: now,
		}
		entry.element = l.lru.PushFront(entry)
		l.store[key] = entry
	} else {
		entry.requests.removeExpired(now.Unix(), duration)
		entry.lastActive = now
		l.lru.MoveToFront(entry.element)
	}

	allowed := entry.requests.length < maxRequestNum
	if allowed {
		entry.requests.append(now.Unix())
	}

	return allowed
}
