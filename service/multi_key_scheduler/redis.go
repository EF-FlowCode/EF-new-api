package multi_key_scheduler

import (
	"context"
	_ "embed"
	"fmt"
	"math/rand"
	"strconv"
	"strings"
	"time"

	"eflow.local/new-api/common"
	"eflow.local/new-api/dto"
	"github.com/go-redis/redis/v8"
)

//go:embed lua/acquire.lua
var redisAcquireScript string

//go:embed lua/peek.lua
var redisPeekScript string

//go:embed lua/release.lua
var redisReleaseScript string

//go:embed lua/cool.lua
var redisCoolScript string

type redisBackend struct {
	client     *redis.Client
	acquireSHA string
	peekSHA    string
	releaseSHA string
	coolSHA    string
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
	pSHA, err := common.RDB.ScriptLoad(ctx, redisPeekScript).Result()
	if err != nil {
		return nil, fmt.Errorf("load peek script: %w", err)
	}
	rSHA, err := common.RDB.ScriptLoad(ctx, redisReleaseScript).Result()
	if err != nil {
		return nil, fmt.Errorf("load release script: %w", err)
	}
	cSHA, err := common.RDB.ScriptLoad(ctx, redisCoolScript).Result()
	if err != nil {
		return nil, fmt.Errorf("load cool script: %w", err)
	}
	return &redisBackend{
		client:     common.RDB,
		acquireSHA: aSHA,
		peekSHA:    pSHA,
		releaseSHA: rSHA,
		coolSHA:    cSHA,
	}, nil
}

// keys 返回三元组 (rpm zset, tpm zset, ctrl hash) 的 Redis key 名。
//
// 前缀 mks: = multi-key scheduler，与 channel_limiter 的 chnlrl: 区分。
func (b *redisBackend) keys(channelID int, keyFp, model string) (rpmKey, tpmKey, ctrlKey string) {
	// model 名可能包含 ":" / "/" 等字符（如 happyhorse:t2v），统一转成 "_" 避免歧义
	safeModel := strings.NewReplacer(":", "_", "/", "_").Replace(model)
	base := fmt.Sprintf("%d:%s:%s", channelID, keyFp, safeModel)
	return "mks:rpm:" + base, "mks:tpm:" + base, "mks:ctrl:" + base
}

func uniqueToken() string {
	return strconv.FormatInt(time.Now().UnixNano(), 36) + ":" + strconv.FormatInt(rand.Int63(), 36)
}

// schedulerWriteOpTimeout 是 release / cool / set-affinity 这类「必须落地」的
// Redis 写操作的独立超时。
const schedulerWriteOpTimeout = 3 * time.Second

// detachedWriteCtx 给「必须执行」的记账/亲和写操作一个脱离请求生命周期的 context。
//
// eflow: ReportSuccess/ReportError(释放 in_flight)、MarkCool、SetAffinity 由调用方
// 传入 c.Request.Context()。客户端一旦断开(大请求挂死到 client_gone 是常态)，该 ctx
// 已 canceled，于是 redis EvalSha 直接 "context canceled" 失败 → in_flight 槽永不释放
// (泄漏)、亲和 pin 永远写不进 redis(缓存亲和形同虚设)。这两件事正是 2026-06-18 国模
// 渠道雪崩的根因。这些写操作与「请求是否还活着」无关，必须用 background ctx 保证落地。
func detachedWriteCtx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), schedulerWriteOpTimeout)
}

// isNoScript 检测 Redis 返回的是否为 NOSCRIPT 错误（脚本缓存丢失，常见于 Redis 重启）。
func isNoScript(err error) bool {
	if err == nil {
		return false
	}
	return strings.HasPrefix(err.Error(), "NOSCRIPT")
}

// evalShaOrReload 先按缓存 SHA 执行 Lua；命中 NOSCRIPT 则自动重新 SCRIPT LOAD 并重试一次。
// sha 是指向 redisBackend 上对应 SHA 字段的指针，重载成功后会原地更新，
// 后续请求直接走新 SHA，不再重复加载。
func (b *redisBackend) evalShaOrReload(ctx context.Context, sha *string, script string, keys []string, args ...any) *redis.Cmd {
	res := b.client.EvalSha(ctx, *sha, keys, args...)
	if isNoScript(res.Err()) {
		common.SysLog(fmt.Sprintf("multi_key_scheduler: NOSCRIPT detected, reloading Lua script"))
		newSHA, loadErr := b.client.ScriptLoad(ctx, script).Result()
		if loadErr != nil {
			common.SysError(fmt.Sprintf("multi_key_scheduler: script reload failed: %v", loadErr))
			return res
		}
		*sha = newSHA
		res = b.client.EvalSha(ctx, *sha, keys, args...)
	}
	return res
}

func (b *redisBackend) Acquire(ctx context.Context, channelID int, keyFp, model string, quota dto.ModelQuota) Decision {
	rpmKey, tpmKey, ctrlKey := b.keys(channelID, keyFp, model)
	res, err := b.evalShaOrReload(ctx, &b.acquireSHA, redisAcquireScript,
		[]string{rpmKey, tpmKey, ctrlKey},
		quota.RPMMax, quota.TPMMax, quota.ConcurrencyMax,
		rpmWindowSeconds, uniqueToken(), time.Now().UnixMilli(),
	).StringSlice()
	if err != nil {
		// fail-open：调度器自身故障不能拖垮网关，让上层 fallback
		common.SysError(fmt.Sprintf("multi_key_scheduler: redis acquire failed channel=%d model=%s err=%v", channelID, model, err))
		return Decision{Allowed: true}
	}
	return parseAcquireResult(res)
}

func (b *redisBackend) Peek(ctx context.Context, channelID int, keyFp, model string, quota dto.ModelQuota) Decision {
	rpmKey, tpmKey, ctrlKey := b.keys(channelID, keyFp, model)
	res, err := b.evalShaOrReload(ctx, &b.peekSHA, redisPeekScript,
		[]string{rpmKey, tpmKey, ctrlKey},
		quota.RPMMax, quota.TPMMax, quota.ConcurrencyMax,
		rpmWindowSeconds, time.Now().UnixMilli(),
	).StringSlice()
	if err != nil {
		// Peek 失败时让候选过评分（fail-open）
		return Decision{Allowed: true}
	}
	return parsePeekResult(res)
}

func (b *redisBackend) ReportSuccess(ctx context.Context, channelID int, keyFp, model string, tokens int) {
	ctx, cancel := detachedWriteCtx() // 必须落地：不能因客户端断开而漏释放 in_flight
	defer cancel()
	_, tpmKey, ctrlKey := b.keys(channelID, keyFp, model)
	if _, err := b.evalShaOrReload(ctx, &b.releaseSHA, redisReleaseScript,
		[]string{tpmKey, ctrlKey},
		tokens, time.Now().UnixMilli(), tpmWindowSeconds, uniqueToken(),
	).Result(); err != nil {
		common.SysError(fmt.Sprintf("multi_key_scheduler: redis release(success) failed channel=%d model=%s err=%v", channelID, model, err))
	}
}

func (b *redisBackend) ReportError(ctx context.Context, channelID int, keyFp, model string) {
	ctx, cancel := detachedWriteCtx() // 必须落地：不能因客户端断开而漏释放 in_flight
	defer cancel()
	_, tpmKey, ctrlKey := b.keys(channelID, keyFp, model)
	if _, err := b.evalShaOrReload(ctx, &b.releaseSHA, redisReleaseScript,
		[]string{tpmKey, ctrlKey},
		0, time.Now().UnixMilli(), tpmWindowSeconds, uniqueToken(),
	).Result(); err != nil {
		common.SysError(fmt.Sprintf("multi_key_scheduler: redis release(error) failed channel=%d model=%s err=%v", channelID, model, err))
	}
}

func (b *redisBackend) MarkCool(ctx context.Context, channelID int, keyFp, model string, seconds int, message string) {
	if seconds <= 0 {
		return
	}
	ctx, cancel := detachedWriteCtx() // 必须落地：cool 状态不能因客户端断开而写不进
	defer cancel()
	_, _, ctrlKey := b.keys(channelID, keyFp, model)
	now := time.Now().UnixMilli()
	if len(message) > 256 {
		message = message[:256]
	}
	if _, err := b.evalShaOrReload(ctx, &b.coolSHA, redisCoolScript,
		[]string{ctrlKey},
		now+int64(seconds)*1000, message, now,
	).Result(); err != nil {
		common.SysError(fmt.Sprintf("multi_key_scheduler: redis mark_cool failed channel=%d model=%s err=%v", channelID, model, err))
	}
}

// affinityRedisKey: mks:aff:<channelID>:<safeModel>:<affinityValue> -> keyFp（带 TTL）。
func (b *redisBackend) affinityRedisKey(channelID int, model, affinityValue string) string {
	safeModel := strings.NewReplacer(":", "_", "/", "_").Replace(model)
	return fmt.Sprintf("mks:aff:%d:%s:%s", channelID, safeModel, affinityValue)
}

func (b *redisBackend) GetAffinity(ctx context.Context, channelID int, model, affinityValue string) (string, bool) {
	if affinityValue == "" {
		return "", false
	}
	fp, err := b.client.Get(ctx, b.affinityRedisKey(channelID, model, affinityValue)).Result()
	if err != nil || fp == "" {
		return "", false // redis.Nil（无 pin）或读失败都按"无亲和"处理，fail-open
	}
	return fp, true
}

func (b *redisBackend) SetAffinity(ctx context.Context, channelID int, model, affinityValue, keyFp string, ttlSeconds int) {
	if affinityValue == "" || keyFp == "" || ttlSeconds <= 0 {
		return
	}
	ctx, cancel := detachedWriteCtx() // 必须落地：亲和 pin 写不进 redis 就没有缓存亲和
	defer cancel()
	if err := b.client.Set(ctx, b.affinityRedisKey(channelID, model, affinityValue), keyFp, time.Duration(ttlSeconds)*time.Second).Err(); err != nil {
		common.SysError(fmt.Sprintf("multi_key_scheduler: redis set affinity failed channel=%d model=%s err=%v", channelID, model, err))
	}
}

func (b *redisBackend) Snapshot(ctx context.Context, channelID int) []StateEntry {
	pattern := fmt.Sprintf("mks:ctrl:%d:*", channelID)
	var (
		cursor uint64
		out    []StateEntry
	)
	for {
		keys, next, err := b.client.Scan(ctx, cursor, pattern, 200).Result()
		if err != nil {
			common.SysError(fmt.Sprintf("multi_key_scheduler: snapshot scan failed channel=%d err=%v", channelID, err))
			return out
		}
		for _, k := range keys {
			entry, ok := b.snapshotOne(ctx, channelID, k)
			if ok {
				out = append(out, entry)
			}
		}
		if next == 0 {
			break
		}
		cursor = next
	}
	return out
}

func (b *redisBackend) snapshotOne(ctx context.Context, channelID int, ctrlKey string) (StateEntry, bool) {
	// ctrlKey 形如 "mks:ctrl:<channelID>:<keyFp>:<model>"
	prefix := fmt.Sprintf("mks:ctrl:%d:", channelID)
	suffix := strings.TrimPrefix(ctrlKey, prefix)
	if suffix == ctrlKey {
		return StateEntry{}, false
	}
	sep := strings.IndexByte(suffix, ':')
	if sep <= 0 {
		return StateEntry{}, false
	}
	keyFp, model := suffix[:sep], suffix[sep+1:]

	rpmKey := "mks:rpm:" + suffix
	tpmKey := "mks:tpm:" + suffix
	now := time.Now().UnixMilli()
	cutoff := now - rpmWindowSeconds*1000

	pipe := b.client.Pipeline()
	hgetAll := pipe.HGetAll(ctx, ctrlKey)
	pipe.ZRemRangeByScore(ctx, rpmKey, "0", strconv.FormatInt(cutoff, 10))
	rpmCard := pipe.ZCard(ctx, rpmKey)
	pipe.ZRemRangeByScore(ctx, tpmKey, "0", strconv.FormatInt(cutoff, 10))
	tpmRange := pipe.ZRange(ctx, tpmKey, 0, -1)
	if _, err := pipe.Exec(ctx); err != nil && err != redis.Nil {
		return StateEntry{}, false
	}

	ctrl := hgetAll.Val()
	tpmSum := 0
	for _, m := range tpmRange.Val() {
		i := strings.IndexByte(m, ':')
		if i <= 0 {
			continue
		}
		if n, err := strconv.Atoi(m[:i]); err == nil {
			tpmSum += n
		}
	}

	entry := StateEntry{
		KeyFp:       keyFp,
		Model:       model,
		RPMCount:    int(rpmCard.Val()),
		TPMCount:    tpmSum,
		InFlight:    atoiOr(ctrl["in_flight"], 0),
		CoolUntilMs: atoi64Or(ctrl["cool_until"], 0),
		Last429AtMs: atoi64Or(ctrl["last_429_at"], 0),
		Last429Msg:  ctrl["last_429_msg"],
	}
	return entry, true
}

// parseAcquireResult / parsePeekResult 把 Lua 返回的字符串数组解析回 Decision。
//
// acquire 返回 6 元组：[allowed, reason|rpm+1, rpm|tpm, tpm|inFlight+1, inFlight|coolUntil, coolUntil|"0"]
// peek    返回 5 元组：[allowed, reason|rpm,    rpm|tpm,  tpm|inFlight,    inFlight|coolUntil]
//
// 容错：长度不足时 allowed=false + reason="all_unavailable"，避免 Lua 异常吃死请求。
func parseAcquireResult(res []string) Decision {
	if len(res) < 1 {
		return Decision{Allowed: false, Reason: ReasonAllUnavailable}
	}
	if res[0] == "1" {
		// allowed; res = ["1", rpmCount+1, tpmCount, inFlight+1, "0", "0"]
		return Decision{
			Allowed:  true,
			RPMCount: atoiAt(res, 1),
			TPMCount: atoiAt(res, 2),
			InFlight: atoiAt(res, 3),
		}
	}
	reason := ""
	if len(res) >= 2 {
		reason = res[1]
	}
	return Decision{
		Allowed:  false,
		Reason:   reason,
		RPMCount: atoiAt(res, 2),
		TPMCount: atoiAt(res, 3),
		InFlight: atoiAt(res, 4),
		Cooling:  reason == ReasonCooling,
	}
}

func parsePeekResult(res []string) Decision {
	if len(res) < 1 {
		return Decision{Allowed: true}
	}
	if res[0] == "1" {
		return Decision{
			Allowed:  true,
			RPMCount: atoiAt(res, 1),
			TPMCount: atoiAt(res, 2),
			InFlight: atoiAt(res, 3),
		}
	}
	reason := ""
	if len(res) >= 2 {
		reason = res[1]
	}
	return Decision{
		Allowed:  false,
		Reason:   reason,
		RPMCount: atoiAt(res, 2),
		TPMCount: atoiAt(res, 3),
		InFlight: atoiAt(res, 4),
		Cooling:  reason == ReasonCooling,
	}
}

func atoiAt(arr []string, i int) int {
	if i >= len(arr) {
		return 0
	}
	return atoiOr(arr[i], 0)
}

func atoiOr(s string, def int) int {
	if s == "" {
		return def
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return def
	}
	return n
}

func atoi64Or(s string, def int64) int64 {
	if s == "" {
		return def
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return def
	}
	return n
}
