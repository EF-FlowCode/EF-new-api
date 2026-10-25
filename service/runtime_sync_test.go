package service

import (
	"errors"
	"testing"

	"eflow.local/new-api/common"
	"eflow.local/new-api/internal/testconfig"
	"eflow.local/new-api/setting/operation_setting"
	"github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/require"
)

func useRuntimeSyncRedis(t *testing.T) {
	t.Helper()
	server := miniredis.RunT(t)
	previousRDB, previousEnabled := common.RDB, common.RedisEnabled
	previousTargets, previousRevision := runtimeSyncTargets, runtimeSyncRevision
	common.RDB, common.RedisEnabled = redis.NewClient(&redis.Options{Addr: server.Addr()}), true
	runtimeSyncRevision = map[string]string{}
	t.Cleanup(func() {
		_ = common.RDB.Close()
		common.RDB, common.RedisEnabled = previousRDB, previousEnabled
		runtimeSyncTargets, runtimeSyncRevision = previousTargets, previousRevision
	})
}

func TestRuntimeSyncReloadsOnlyAfterRevisionChanges(t *testing.T) {
	useRuntimeSyncRedis(t)
	calls, fail := 0, false
	runtimeSyncTargets = []runtimeSyncTarget{{key: "runtime_sync:test", reload: func() error {
		calls++
		if fail {
			return errors.New("database unavailable")
		}
		return nil
	}}}

	syncRuntimeState(true)
	syncRuntimeState(false)
	require.Zero(t, calls, "baseline and unchanged revisions must not reload")

	publishRuntimeRevision("runtime_sync:test")
	fail = true
	syncRuntimeState(false)
	require.Equal(t, 1, calls)
	// A failed reload keeps the old revision so the next tick retries.
	fail = false
	syncRuntimeState(false)
	require.Equal(t, 2, calls)
	syncRuntimeState(false)
	require.Equal(t, 2, calls)
}

func TestRuntimeSyncUnblockClearsOtherInstanceGateCache(t *testing.T) {
	useRuntimeSyncRedis(t)
	syncRuntimeState(true)

	key := riskMemoryKey(RiskSubjectTypeUser, 7, "vip")
	globalRiskCenter.cache.Store(key, &localRiskGateCacheEntry{ExpiresAt: 1 << 40})
	t.Cleanup(func() { globalRiskCenter.cache.Delete(key) })

	// Another instance handled the unblock and only published the revision.
	publishRuntimeRevision(runtimeSyncRiskBlocksKey)
	syncRuntimeState(false)
	_, cached := globalRiskCenter.cache.Load(key)
	require.False(t, cached)
}

func TestModerationReconcileAppliesKeysSavedByAnotherInstance(t *testing.T) {
	center := &moderationCenter{keyRing: NewModerationKeyRing(nil)}
	center.started.Store(true)
	center.applyConfig(operation_setting.GetModerationSetting())
	require.Zero(t, center.keyRing.Size())

	// Option sync delivers keys saved elsewhere without calling ReloadModerationConfig.
	testconfig.Set(t, "moderation", map[string]string{"api_keys": `["sk-one","sk-two"]`})
	center.reconcileConfig()
	require.Equal(t, 2, center.keyRing.Size())
}
