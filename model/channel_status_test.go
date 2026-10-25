package model

import (
	"errors"
	"testing"

	"eflow.local/new-api/common"
	"eflow.local/new-api/constant"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupChannelStatusTest(t *testing.T) {
	t.Helper()
	truncateTables(t)
	require.NoError(t, DB.Exec("DELETE FROM abilities").Error)
	require.NoError(t, DB.Exec("DELETE FROM channels").Error)
	memoryCacheEnabled := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = false
	t.Cleanup(func() { common.MemoryCacheEnabled = memoryCacheEnabled })
}

func TestUpdateChannelStatusPersistsMultiKeyState(t *testing.T) {
	setupChannelStatusTest(t)
	channel := Channel{
		Name: "multi-key-status", Key: "key-a\nkey-b", Status: common.ChannelStatusEnabled,
		ChannelInfo: ChannelInfo{IsMultiKey: true, MultiKeySize: 2, MultiKeyMode: constant.MultiKeyModePolling, MultiKeyPollingIndex: 1},
	}
	require.NoError(t, DB.Create(&channel).Error)
	require.True(t, UpdateChannelStatus(channel.Id, "key-a", common.ChannelStatusAutoDisabled, "provider rejected key"))

	var stored Channel
	require.NoError(t, DB.First(&stored, channel.Id).Error)
	assert.Equal(t, common.ChannelStatusEnabled, stored.Status)
	assert.Equal(t, common.ChannelStatusAutoDisabled, stored.ChannelInfo.MultiKeyStatusList[0])
	assert.Equal(t, "provider rejected key", stored.ChannelInfo.MultiKeyDisabledReason[0])
	assert.Equal(t, 1, stored.ChannelInfo.MultiKeyPollingIndex)
}

func TestSaveStatusStatePreservesUnownedColumns(t *testing.T) {
	setupChannelStatusTest(t)
	channel := Channel{Name: "status", Key: "original-key", Status: common.ChannelStatusEnabled, Models: "original-model", Group: "default", UsedQuota: 100}
	require.NoError(t, DB.Create(&channel).Error)
	stale, err := GetChannelById(channel.Id, true)
	require.NoError(t, err)
	concurrentInfo := ChannelInfo{IsMultiKey: true, MultiKeySize: 2, MultiKeyMode: constant.MultiKeyModePolling, MultiKeyPollingIndex: 1}
	require.NoError(t, DB.Model(&Channel{}).Where("id = ?", channel.Id).Updates(map[string]any{
		"key": "rotated-key", "used_quota": gorm.Expr("used_quota + ?", 250), "models": "concurrent-model", "channel_info": concurrentInfo,
	}).Error)

	stale.Status = common.ChannelStatusManuallyDisabled
	stale.SetOtherInfo(map[string]any{"status_reason": "manual operation", "status_time": int64(1234)})
	require.NoError(t, stale.saveStatusState())

	var stored Channel
	require.NoError(t, DB.First(&stored, channel.Id).Error)
	assert.Equal(t, common.ChannelStatusManuallyDisabled, stored.Status)
	assert.Equal(t, "rotated-key", stored.Key)
	assert.Equal(t, int64(350), stored.UsedQuota)
	assert.Equal(t, "concurrent-model", stored.Models)
	assert.Equal(t, concurrentInfo, stored.ChannelInfo)
}

func seedMultiKeyStatusCache(t *testing.T, db *gorm.DB) *Channel {
	t.Helper()
	priority, weight := int64(10), uint(100)
	channel := &Channel{
		Type: constant.ChannelTypeCustom, Name: "multi-key-cache", Key: "key-a", Status: common.ChannelStatusEnabled,
		Models: "mk-model", Group: "mk-group", Priority: &priority, Weight: &weight,
		ChannelInfo: ChannelInfo{IsMultiKey: true, MultiKeySize: 1, MultiKeyMode: constant.MultiKeyModePolling},
	}
	require.NoError(t, db.Create(channel).Error)
	require.NoError(t, db.Create(&Ability{Group: channel.Group, Model: channel.Models, ChannelId: channel.Id, Enabled: true, Priority: &priority, Weight: weight}).Error)
	InitChannelCache()
	assertSelectedChannel(t, channel.Group, channel.Models, channel.Id)
	return channel
}

func assertMultiKeyCacheUntouched(t *testing.T, channel *Channel) {
	t.Helper()
	cached, err := CacheGetChannel(channel.Id)
	require.NoError(t, err)
	assert.Equal(t, common.ChannelStatusEnabled, cached.Status)
	assert.Empty(t, cached.ChannelInfo.MultiKeyStatusList)
	assert.Empty(t, cached.ChannelInfo.MultiKeyDisabledReason)
	assertSelectedChannel(t, channel.Group, channel.Models, channel.Id)
}

// A failed status write must leave the cache exactly as the database is: no
// key disabled only in memory and no channel dropped from routing.
func TestUpdateChannelStatusKeepsCacheWhenSaveFails(t *testing.T) {
	db := useChannelCacheTestDB(t)
	channel := seedMultiKeyStatusCache(t, db)
	const callback = "test:fail-channel-status-write"
	require.NoError(t, DB.Callback().Update().Before("gorm:update").Register(callback, func(tx *gorm.DB) {
		if _, ok := tx.Statement.Model.(*Channel); ok {
			tx.AddError(errors.New("injected channel status write failure"))
		}
	}))
	t.Cleanup(func() { _ = DB.Callback().Update().Remove(callback) })

	require.False(t, UpdateChannelStatus(channel.Id, "key-a", common.ChannelStatusAutoDisabled, "provider rejected key"))
	assertMultiKeyCacheUntouched(t, channel)

	require.NoError(t, DB.Callback().Update().Remove(callback))
	require.True(t, UpdateChannelStatus(channel.Id, "key-a", common.ChannelStatusAutoDisabled, "provider rejected key"))
	cached, err := CacheGetChannel(channel.Id)
	require.NoError(t, err)
	assert.Equal(t, common.ChannelStatusAutoDisabled, cached.Status)
	assert.Equal(t, common.ChannelStatusAutoDisabled, cached.ChannelInfo.MultiKeyStatusList[0])
	var stored Channel
	require.NoError(t, DB.First(&stored, channel.Id).Error)
	assert.Equal(t, common.ChannelStatusAutoDisabled, stored.Status)
	assert.Equal(t, common.ChannelStatusAutoDisabled, stored.ChannelInfo.MultiKeyStatusList[0])
}

func TestUpdateChannelStatusKeepsCacheWhenChannelReadFails(t *testing.T) {
	db := useChannelCacheTestDB(t)
	channel := seedMultiKeyStatusCache(t, db)
	require.NoError(t, db.Delete(&Channel{}, channel.Id).Error)

	require.False(t, UpdateChannelStatus(channel.Id, "key-a", common.ChannelStatusAutoDisabled, "provider rejected key"))
	assertMultiKeyCacheUntouched(t, channel)
}
