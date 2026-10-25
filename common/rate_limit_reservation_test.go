package common

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRateLimitReservationConcurrentAdmissionAndCompletion(t *testing.T) {
	var limiter InMemoryRateLimiter
	limiter.Init(0)
	reservations := make(chan *RateLimitReservation, 100)
	var wg sync.WaitGroup
	for range 100 {
		wg.Go(func() {
			if r := limiter.Reserve("user", 3, 60); r != nil {
				reservations <- r
			}
		})
	}
	wg.Wait()
	close(reservations)
	require.Len(t, reservations, 3)
	for r := range reservations {
		// Concurrent duplicate completion must release and count exactly once.
		for range 10 {
			wg.Go(func() { r.Complete(true) })
		}
	}
	wg.Wait()
	require.Empty(t, limiter.reservations)
	require.Nil(t, limiter.Reserve("user", 3, 60))
	// Age successes; a new window can admit again.
	for n := limiter.store["user"].requests.head; n != nil; n = n.next {
		n.timestamp -= 60
	}
	r := limiter.Reserve("user", 3, 60)
	require.NotNil(t, r)
	r.Complete(false)
	require.Empty(t, limiter.reservations)
}

func TestRateLimitReservationSurvivesIdleCleanup(t *testing.T) {
	var limiter InMemoryRateLimiter
	limiter.Init(0)
	limiter.expirationDuration = time.Second
	r := limiter.Reserve("pending", 1, 60)
	require.NotNil(t, r)
	limiter.deleteExpiredEntries(time.Now().Add(2 * time.Second))
	// An idle bucket may disappear; its in-flight allowance must not.
	require.Nil(t, limiter.Reserve("pending", 1, 60))
	r.Complete(true)
	limiter.deleteExpiredEntries(time.Now().Add(2 * time.Second))
	require.Nil(t, limiter.Reserve("pending", 1, 60))
	limiter.deleteExpiredEntries(time.Now().Add(61 * time.Second))
	r = limiter.Reserve("pending", 1, 60)
	require.NotNil(t, r)
	r.Complete(false)
}

func TestRateLimitReservationFailedCompletionIsFinal(t *testing.T) {
	var limiter InMemoryRateLimiter
	limiter.Init(0)
	r := limiter.Reserve("user", 1, 60)
	r.Complete(false)
	r.Complete(true)
	require.Empty(t, limiter.reservations)
	next := limiter.Reserve("user", 1, 60)
	require.NotNil(t, next)
	next.Complete(false)
}
