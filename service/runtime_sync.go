package service

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"sync"
	"time"

	"eflow.local/new-api/common"
	"eflow.local/new-api/setting/operation_setting"
	"github.com/go-redis/redis/v8"
)

// 多实例（蓝绿 A/B + worker）下，风控/审核规则与手动解封只在处理请求的实例生效。
// 变更方递增 Redis 修订号，各实例定时比对并重载本地快照；轮询而非发布订阅，
// 丢失通知或实例晚启动都能在下一轮追上。未配置 Redis 时保持单实例语义。
const (
	runtimeSyncRiskRulesKey       = "runtime_sync:risk_rules"
	runtimeSyncModerationRulesKey = "runtime_sync:moderation_rules"
	runtimeSyncRiskBlocksKey      = "runtime_sync:risk_blocks"

	runtimeSyncInterval = 5 * time.Second
	runtimeSyncTimeout  = 2 * time.Second
)

type runtimeSyncTarget struct {
	key    string
	reload func() error
}

var (
	runtimeSyncOnce     sync.Once
	runtimeSyncRevision = map[string]string{}
	runtimeSyncTargets  = []runtimeSyncTarget{
		{key: runtimeSyncRiskRulesKey, reload: ReloadRiskRules},
		{key: runtimeSyncModerationRulesKey, reload: ReloadModerationRules},
		{key: runtimeSyncRiskBlocksKey, reload: func() error { globalRiskCenter.clearGateCache(); return nil }},
	}
)

// publishRuntimeRevision 通知其他实例重载；失败只记日志，本实例已完成本地重载。
func publishRuntimeRevision(key string) {
	if !common.RedisEnabled || common.RDB == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), runtimeSyncTimeout)
	defer cancel()
	if err := common.RDB.Incr(ctx, key).Err(); err != nil {
		common.SysError("runtime sync publish " + key + " failed: " + err.Error())
	}
}

// StartRuntimeSync 在各中心启动之后调用：先记录当前修订号作为基线，再定时比对。
func StartRuntimeSync() {
	runtimeSyncOnce.Do(func() {
		syncRuntimeState(true)
		go func() {
			ticker := time.NewTicker(runtimeSyncInterval)
			defer ticker.Stop()
			for range ticker.C {
				syncRuntimeState(false)
			}
		}()
	})
}

func syncRuntimeState(baseline bool) {
	// 审核配置随 Option 周期同步到各实例，但密钥轮换与超时只在本实例保存时重建；
	// 与已应用的配置比对即可追上，无需 Redis。
	globalModerationCenter.reconcileConfig()
	if !common.RedisEnabled || common.RDB == nil {
		return
	}
	for _, target := range runtimeSyncTargets {
		revision, err := readRuntimeRevision(target.key)
		if err != nil {
			common.SysError("runtime sync read " + target.key + " failed: " + err.Error())
			continue
		}
		if previous, seen := runtimeSyncRevision[target.key]; seen && previous == revision {
			continue
		}
		if !baseline {
			if err := target.reload(); err != nil {
				// 保留旧修订号，下一轮重试。
				common.SysError("runtime sync reload " + target.key + " failed: " + err.Error())
				continue
			}
		}
		runtimeSyncRevision[target.key] = revision
	}
}

func readRuntimeRevision(key string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), runtimeSyncTimeout)
	defer cancel()
	revision, err := common.RDB.Get(ctx, key).Result()
	if errors.Is(err, redis.Nil) {
		return "0", nil
	}
	return revision, err
}

// moderationConfigFingerprint 覆盖 applyConfig 实际使用的字段。
func moderationConfigFingerprint(cfg *operation_setting.ModerationSetting) string {
	if cfg == nil {
		return ""
	}
	return strings.Join(cfg.APIKeys, "\n") + "|" + strconv.Itoa(cfg.HTTPTimeoutMS)
}
