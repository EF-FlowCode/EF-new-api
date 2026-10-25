package service

import (
	"context"
	"testing"
	"time"

	"eflow.local/new-api/common"
	"eflow.local/new-api/internal/drainpool"
	"github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/require"
)

func newModerationTestQueue(t *testing.T, size int) (*miniredis.Miniredis, *moderationRedisQueue) {
	t.Helper()
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr(), MaxRetries: -1})
	t.Cleanup(func() { _ = client.Close() })
	return server, &moderationRedisQueue{rdb: client, walKey: "test:moderation:wal", maxSize: size}
}

func TestModerationWALEnqueueRecoveryAndCompletion(t *testing.T) {
	_, queue := newModerationTestQueue(t, 8)
	baseline := drainpool.Pending()
	center := &moderationCenter{queue: make(chan *moderationEvent, 8), redisQueue: queue}
	event := &moderationEvent{Source: "relay", RequestID: "request-1", Done: make(chan *ModerationResult, 1)}
	center.enqueue(event)
	require.Equal(t, int64(1), queue.depth())
	require.Equal(t, baseline+1, drainpool.Pending())
	data, err := common.Marshal(event)
	require.NoError(t, err)
	require.NotContains(t, string(data), "Done")
	require.NotContains(t, string(data), "walPayload")
	recovered := make(chan *moderationEvent, 8)
	require.Equal(t, 1, queue.recoverIntoChannel(recovered, decodeModerationEvent))
	require.Len(t, recovered, 1, "one WAL entry must not fill the recovery queue")
	copy := <-recovered
	require.Equal(t, event.RequestID, copy.RequestID)
	require.Nil(t, copy.Done)
	require.Equal(t, int64(1), queue.depth(), "recovery must not delete unprocessed work")
	// Simulate completion after a restart; mutable event fields must not affect acknowledgment.
	copy.TextItems = []string{"changed"}
	center.completeEvent(copy)
	require.Zero(t, queue.depth())
	center.completeEvent(<-center.queue)
	require.Equal(t, baseline, drainpool.Pending())
}

func TestModerationWALBoundedRecoveryRetainsUnprocessedEntries(t *testing.T) {
	_, queue := newModerationTestQueue(t, 8)
	ctx := context.Background()
	oldest := `{ "RequestID": "oldest", "Source": "relay", "future_field": true }`
	newest := `{"RequestID":"newest","Source":"relay"}`
	require.NoError(t, queue.rdb.LPush(ctx, queue.walKey, oldest, "broken-json", newest).Err())
	out := make(chan *moderationEvent, 1)
	require.Equal(t, 1, queue.recoverIntoChannel(out, decodeModerationEvent))
	entry := <-out
	require.Equal(t, "oldest", entry.RequestID)
	require.Equal(t, int64(2), queue.depth())
	center := &moderationCenter{redisQueue: queue}
	center.completeEvent(entry)
	require.Equal(t, int64(1), queue.depth(), "acknowledgment must match original bytes including unknown fields")
	require.Equal(t, 1, queue.recoverIntoChannel(out, decodeModerationEvent))
	require.Equal(t, "newest", (<-out).RequestID)
}

func TestModerationWALOverflowAndFailureBalanceDrain(t *testing.T) {
	server, queue := newModerationTestQueue(t, 2)
	baseline := drainpool.Pending()
	center := &moderationCenter{queue: make(chan *moderationEvent, 1), redisQueue: queue}
	center.enqueue(&moderationEvent{RequestID: "old"})
	center.enqueue(&moderationEvent{RequestID: "new"})
	require.Equal(t, baseline+1, drainpool.Pending())
	require.Equal(t, int64(1), queue.depth(), "discarded in-memory entries must also leave the WAL")
	center.completeEvent(<-center.queue)
	require.Equal(t, baseline, drainpool.Pending())
	server.SetError("ERR unavailable")
	center.enqueue(&moderationEvent{RequestID: "memory-only"})
	require.Len(t, center.queue, 1)
	require.Zero(t, queue.recoverIntoChannel(make(chan *moderationEvent, 2), decodeModerationEvent))
	center.completeEvent(<-center.queue)
	require.Equal(t, baseline, drainpool.Pending())
}

func TestModerationDebugExpiryOnWebInstance(t *testing.T) {
	previous, master := globalModerationCenter, common.IsMasterNode
	globalModerationCenter, common.IsMasterNode = &moderationCenter{}, false
	t.Cleanup(func() { globalModerationCenter, common.IsMasterNode = previous, master })
	now := time.Now().Unix()
	center := globalModerationCenter
	center.debugStore.Store("expired", &moderationDebugEntry{Result: &ModerationResult{}, ExpiresAt: now})
	center.debugStore.Store("live", &moderationDebugEntry{Result: &ModerationResult{RequestID: "live"}, ExpiresAt: now + 60})
	_, ok := GetModerationDebugResult("expired")
	require.False(t, ok)
	_, ok = center.debugStore.Load("expired")
	require.False(t, ok)
	result, ok := GetModerationDebugResult("live")
	require.True(t, ok)
	require.Equal(t, "live", result.RequestID)
	center.sweepDebugResults(now)
	_, ok = center.debugStore.Load("live")
	require.True(t, ok)
	center.sweepDebugResults(now + 60)
	_, ok = center.debugStore.Load("live")
	require.False(t, ok)
}
