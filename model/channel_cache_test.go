package model

import (
	"testing"

	"eflow.local/new-api/common"
	"eflow.local/new-api/constant"
	"eflow.local/new-api/internal/testdb"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func useChannelCacheTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	oldDB := DB
	oldMemoryCacheEnabled := common.MemoryCacheEnabled
	channelSyncLock.Lock()
	oldGroup2model2channels := group2model2channels
	oldChannelsIDM := channelsIDM
	channelSyncLock.Unlock()

	db := testdb.OpenMySQL8(t, &Channel{}, &Ability{})

	DB = db
	common.MemoryCacheEnabled = true
	t.Cleanup(func() {
		DB = oldDB
		common.MemoryCacheEnabled = oldMemoryCacheEnabled
		channelSyncLock.Lock()
		group2model2channels = oldGroup2model2channels
		channelsIDM = oldChannelsIDM
		channelSyncLock.Unlock()
	})
	return db
}

func seedChannelCacheTestSnapshot(t *testing.T, db *gorm.DB) *Channel {
	t.Helper()

	priority := int64(10)
	weight := uint(100)
	channel := &Channel{
		Type:     constant.ChannelTypeCustom,
		Key:      "last-good-key",
		Status:   common.ChannelStatusEnabled,
		Name:     "last-good-channel",
		Models:   "last-good-model",
		Group:    "last-good-group",
		Priority: &priority,
		Weight:   &weight,
	}
	require.NoError(t, db.Create(channel).Error)
	require.NoError(t, db.Create(&Ability{
		Group:     channel.Group,
		Model:     channel.Models,
		ChannelId: channel.Id,
		Enabled:   true,
		Priority:  &priority,
		Weight:    weight,
	}).Error)

	InitChannelCache()
	assertChannelCacheTestSnapshot(t, channel.Id)
	return channel
}

func assertChannelCacheTestSnapshot(t *testing.T, channelID int) {
	t.Helper()

	assertSelectedChannel(t, "last-good-group", "last-good-model", channelID)
}

func assertSelectedChannel(t *testing.T, group string, model string, channelID int) {
	t.Helper()

	selected, err := GetRandomSatisfiedChannel(group, model, 0)
	require.NoError(t, err)
	require.NotNil(t, selected)
	require.Equal(t, channelID, selected.Id)
}

func TestInitChannelCacheKeepsLastGoodSnapshotWhenChannelQueryFails(t *testing.T) {
	db := useChannelCacheTestDB(t)
	lastGood := seedChannelCacheTestSnapshot(t, db)

	brokenDB := testdb.OpenMySQL8(t, &Ability{})
	DB = brokenDB

	InitChannelCache()
	assertChannelCacheTestSnapshot(t, lastGood.Id)
}

func TestInitChannelCacheKeepsLastGoodSnapshotWhenAbilityQueryFails(t *testing.T) {
	db := useChannelCacheTestDB(t)
	lastGood := seedChannelCacheTestSnapshot(t, db)
	require.NoError(t, db.Migrator().DropTable(&Ability{}))

	require.NotPanics(t, InitChannelCache)
	assertChannelCacheTestSnapshot(t, lastGood.Id)
}

func TestInitChannelCachePublishesNewSnapshotAfterDatabaseRecovers(t *testing.T) {
	db := useChannelCacheTestDB(t)
	lastGood := seedChannelCacheTestSnapshot(t, db)
	require.NoError(t, db.Migrator().DropTable(&Ability{}))

	InitChannelCache()
	assertChannelCacheTestSnapshot(t, lastGood.Id)

	require.NoError(t, db.AutoMigrate(&Ability{}))
	require.NoError(t, db.Delete(lastGood).Error)
	priority := int64(20)
	weight := uint(100)
	recovered := &Channel{
		Type:     constant.ChannelTypeCustom,
		Key:      "recovered-key",
		Status:   common.ChannelStatusEnabled,
		Name:     "recovered-channel",
		Models:   "recovered-model",
		Group:    "recovered-group",
		Priority: &priority,
		Weight:   &weight,
	}
	require.NoError(t, db.Create(recovered).Error)
	require.NoError(t, db.Create(&Ability{
		Group:     recovered.Group,
		Model:     recovered.Models,
		ChannelId: recovered.Id,
		Enabled:   true,
		Priority:  &priority,
		Weight:    weight,
	}).Error)

	InitChannelCache()
	selected, err := GetRandomSatisfiedChannel("last-good-group", "last-good-model", 0)
	require.NoError(t, err)
	require.Nil(t, selected)
	assertSelectedChannel(t, recovered.Group, recovered.Models, recovered.Id)
}

func TestInitChannelCacheToleratesChannelGroupWithoutAbilities(t *testing.T) {
	db := useChannelCacheTestDB(t)
	lastGood := seedChannelCacheTestSnapshot(t, db)

	// A channel moved to a brand-new group whose abilities are not yet
	// rebuilt: previously this wrote into a nil inner map and panicked.
	priority := int64(10)
	weight := uint(100)
	moved := &Channel{
		Type:     constant.ChannelTypeCustom,
		Key:      "moved-key",
		Status:   common.ChannelStatusEnabled,
		Name:     "moved-channel",
		Models:   "moved-model",
		Group:    "group-without-abilities",
		Priority: &priority,
		Weight:   &weight,
	}
	require.NoError(t, db.Create(moved).Error)

	require.NotPanics(t, InitChannelCache)
	assertChannelCacheTestSnapshot(t, lastGood.Id)
	assertSelectedChannel(t, moved.Group, moved.Models, moved.Id)
}

func TestCacheSelectEnabledChannelsProjectsEnabledOnly(t *testing.T) {
	oldMemoryCacheEnabled := common.MemoryCacheEnabled
	channelSyncLock.Lock()
	oldChannelsIDM := channelsIDM
	channelSyncLock.Unlock()
	t.Cleanup(func() {
		common.MemoryCacheEnabled = oldMemoryCacheEnabled
		channelSyncLock.Lock()
		channelsIDM = oldChannelsIDM
		channelSyncLock.Unlock()
	})
	keepAll := func(*Channel) bool { return true }

	common.MemoryCacheEnabled = false
	_, ok := CacheSelectEnabledChannels(keepAll)
	require.False(t, ok, "database fallback when the memory cache is off")
	common.MemoryCacheEnabled = true
	channelSyncLock.Lock()
	channelsIDM = nil
	channelSyncLock.Unlock()
	_, ok = CacheSelectEnabledChannels(keepAll)
	require.False(t, ok, "database fallback before the cache loads")

	mapping := `{"a":"b"}`
	channelSyncLock.Lock()
	channelsIDM = map[int]*Channel{
		3: {Id: 3, Type: constant.ChannelTypeOpenAI, Status: common.ChannelStatusEnabled, Models: "gpt-5.5", Key: "secret"},
		1: {Id: 1, Type: constant.ChannelTypeAli, Status: common.ChannelStatusEnabled, Models: "a", ModelMapping: &mapping},
		2: {Id: 2, Type: constant.ChannelTypeOpenAI, Status: common.ChannelStatusManuallyDisabled, Models: "gpt-5.5"},
		4: {Id: 4, Type: constant.ChannelTypeOpenAI, Status: common.ChannelStatusEnabled, Models: "other"},
	}
	channelSyncLock.Unlock()
	channels, ok := CacheSelectEnabledChannels(func(ch *Channel) bool { return ch.Models != "other" })
	require.True(t, ok)
	require.Len(t, channels, 2)
	require.Equal(t, []int{1, 3}, []int{channels[0].Id, channels[1].Id})
	require.Equal(t, &mapping, channels[0].ModelMapping)
	require.Empty(t, channels[1].Key, "only routing columns are copied")
}
