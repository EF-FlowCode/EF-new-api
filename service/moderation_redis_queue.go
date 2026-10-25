package service

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"eflow.local/new-api/common"
	"github.com/go-redis/redis/v8"
)

// moderationRedisQueue is a per-instance write-ahead log for moderation
// events. Events are LPUSHed to rc:mod:wal:{instance} alongside the
// in-memory channel, and LREMed when a worker finishes the event.
// On startup the WAL is drained back into the in-memory channel so any
// events queued or in-flight before a crash get re-processed.
//
// Why per-instance keys (not a shared queue):
//   - Workers consume from the in-memory channel, never directly from
//     Redis. A shared queue would leave entries no one reads.
//   - With per-instance keys, instance A's restart never replays
//     instance B's events (which B is still processing).
//
// All Redis ops use a short timeout; failures fall back to memory-only
// for that call. Persistence is best-effort and never blocks the relay
// path. Duplicate processing on rare LREM failure is tolerable —
// OpenAI moderation calls are idempotent.
type moderationRedisQueue struct {
	rdb     *redis.Client
	walKey  string
	maxSize int
}

const (
	moderationWALKeyPrefix = "rc:mod:wal:"
	moderationRedisOpTTL   = 200 * time.Millisecond
)

func moderationInstanceID() string {
	if id := strings.TrimSpace(common.NodeName); id != "" {
		return id
	}
	host, _ := os.Hostname()
	host = strings.TrimSpace(host)
	if host == "" {
		host = "node"
	}
	return fmt.Sprintf("%s-%d", host, os.Getpid())
}

func newModerationRedisQueue(maxSize int) *moderationRedisQueue {
	if !common.RedisEnabled || common.RDB == nil {
		return nil
	}
	if maxSize <= 0 {
		maxSize = 4096
	}
	return &moderationRedisQueue{
		rdb:     common.RDB,
		walKey:  moderationWALKeyPrefix + moderationInstanceID(),
		maxSize: maxSize,
	}
}

func (q *moderationRedisQueue) ctx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), moderationRedisOpTTL)
}

// enqueue appends a JSON-encoded event to the WAL with a bound on length.
// LTRIM drops the oldest entry when the WAL is full, mirroring the
// in-memory ring-buffer overflow behaviour.
func (q *moderationRedisQueue) enqueue(eventJSON string) error {
	if q == nil || eventJSON == "" {
		return nil
	}
	ctx, cancel := q.ctx()
	defer cancel()
	pipe := q.rdb.TxPipeline()
	pipe.LPush(ctx, q.walKey, eventJSON)
	pipe.LTrim(ctx, q.walKey, 0, int64(q.maxSize-1))
	_, err := pipe.Exec(ctx)
	return err
}

// complete removes a finished event from the WAL. LREM count=1 deletes a
// single occurrence even if the same payload happened to appear twice
// (uncommon, harmless).
func (q *moderationRedisQueue) complete(eventJSON string) error {
	if q == nil || eventJSON == "" {
		return nil
	}
	ctx, cancel := q.ctx()
	defer cancel()
	return q.rdb.LRem(ctx, q.walKey, 1, eventJSON).Err()
}

// recoverIntoChannel reads one bounded snapshot, oldest first, before workers
// start. Entries stay in the WAL until completion; a crash during recovery must
// not remove them. Entries beyond channel capacity remain for the next restart.
func (q *moderationRedisQueue) recoverIntoChannel(out chan *moderationEvent, decode func(string) *moderationEvent) int {
	if q == nil || out == nil || decode == nil {
		return 0
	}
	ctx, cancel := q.ctx()
	values, err := q.rdb.LRange(ctx, q.walKey, -int64(q.maxSize), -1).Result()
	cancel()
	if err != nil {
		common.SysError("moderation redis recover failed: " + err.Error())
		return 0
	}
	recovered := 0
	for i := len(values) - 1; i >= 0; i-- {
		val := values[i]
		event := decode(val)
		if event == nil {
			if err := q.complete(val); err != nil {
				common.SysError("moderation invalid WAL entry cleanup failed: " + err.Error())
			}
			continue
		}
		event.walPayload = val
		select {
		case out <- event:
			recovered++
		default:
			return recovered
		}
	}
	return recovered
}

// depth returns the live WAL length for the UI status card. Returns 0
// when Redis is unreachable so the card stays informative even if the
// persistence layer is down.
func (q *moderationRedisQueue) depth() int64 {
	if q == nil {
		return 0
	}
	ctx, cancel := q.ctx()
	defer cancel()
	n, err := q.rdb.LLen(ctx, q.walKey).Result()
	if err != nil {
		return 0
	}
	return n
}
