package service

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"time"

	"eflow.local/new-api/common"
	"eflow.local/new-api/setting/operation_setting"
)

var (
	monitoredGroupsMu    sync.RWMutex
	monitoredGroupsCache map[string]struct{}
)

func refreshMonitoredGroupsCache() {
	cfg := operation_setting.GetGroupMonitoringSetting()
	m := make(map[string]struct{}, len(cfg.MonitoringGroups))
	for _, g := range cfg.MonitoringGroups {
		m[g] = struct{}{}
	}
	monitoredGroupsMu.Lock()
	monitoredGroupsCache = m
	monitoredGroupsMu.Unlock()
}

func isGroupMonitored(group string) bool {
	monitoredGroupsMu.RLock()
	defer monitoredGroupsMu.RUnlock()
	if monitoredGroupsCache == nil {
		return false
	}
	_, ok := monitoredGroupsCache[group]
	return ok
}

func RecordMonitoringMetric(group string, channelId int, isSuccess bool, promptTokens, cacheTokens, useTimeMs, frtMs int, modelName string, statusCode int, content string) {
	if !common.RedisEnabled || group == "" || group == "auto" {
		return
	}
	if !isGroupMonitored(group) {
		return
	}

	cfg := operation_setting.GetGroupMonitoringSetting()

	// eflow: 可用率排除统一走 GroupMonitoringSetting.IsAvailabilityExcluded（成功请求 statusCode=0、content="" 时
	// 只可能命中模型排除，与原写时逻辑等价）。同一函数在聚合时(读时)对 logs 错误行重判，
	// 保证写/读两侧口径一致。注意：此处写时排除仅影响 Redis 的 e 计数（现仅供 IsOnline 用），
	// 可用率的错误数已改由聚合时从 logs 现算（见 runRedisAggregation）。
	excludeAvail := cfg.IsAvailabilityExcluded(statusCode, modelName, content)

	excludeCache := false
	if modelName != "" {
		if slices.Contains(cfg.CacheHitExcludeModels, modelName) {
			excludeCache = true
		}
	}

	// eflow: bucket 强制 60 秒，跟 cfg.AggregationIntervalMinutes（控制 ticker 间隔 + DB upsert 频率）
	// 解耦——让响应时间 / 缓存命中数据按一分钟粒度沉淀，看得到瞬时尖峰；上层聚合循环仍按
	// setting 控制，不增加 DB 写入开销。
	const bucketSec int64 = 60
	now := time.Now().Unix()
	bucket := (now / bucketSec) * bucketSec
	key := fmt.Sprintf("gm:b:%d:%s:%d", bucket, group, channelId)

	maxPeriod := max(cfg.CacheHitPeriodMinutes, cfg.AvailabilityPeriodMinutes)
	ttl := time.Duration(maxPeriod*60*2+int(bucketSec)) * time.Second

	ctx := context.Background()
	pipe := common.RDB.TxPipeline()

	if !excludeAvail {
		pipe.HIncrBy(ctx, key, "t", 1)
		if isSuccess {
			pipe.HIncrBy(ctx, key, "s", 1)
		} else {
			pipe.HIncrBy(ctx, key, "e", 1)
		}
	}

	if !excludeCache {
		pipe.HIncrBy(ctx, key, "ct", int64(cacheTokens))
		pipe.HIncrBy(ctx, key, "pt", int64(promptTokens))
	}

	// rt 对全部请求累加，n 是它的分母；t 只计入未被可用率排除的请求，不能当分母。
	pipe.HIncrBy(ctx, key, "rt", int64(useTimeMs))
	pipe.HIncrBy(ctx, key, "n", 1)
	if frtMs > 0 {
		pipe.HIncrBy(ctx, key, "fs", int64(frtMs))
		pipe.HIncrBy(ctx, key, "fc", 1)
	}
	pipe.Expire(ctx, key, ttl)
	_, _ = pipe.Exec(ctx)
}
