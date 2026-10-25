package model

import (
	"testing"

	"eflow.local/new-api/common"
	"github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEnforcementBanInvalidatesAuthCaches(t *testing.T) {
	truncateTables(t)

	server := miniredis.RunT(t)
	oldRedisEnabled, oldRDB := common.RedisEnabled, common.RDB
	common.RDB = redis.NewClient(&redis.Options{Addr: server.Addr()})
	common.RedisEnabled = true
	t.Cleanup(func() {
		_ = common.RDB.Close()
		common.RedisEnabled, common.RDB = oldRedisEnabled, oldRDB
	})

	user := User{Username: "enforce-ban", Password: "password", AffCode: "idt6", Status: common.UserStatusEnabled}
	require.NoError(t, DB.Create(&user).Error)
	require.NoError(t, DB.Create(&Token{UserId: user.Id, Key: "enforce-ban-token"}).Error)

	cached, err := GetUserCache(user.Id)
	require.NoError(t, err)
	require.Equal(t, common.UserStatusEnabled, cached.Status)
	require.True(t, server.Exists(getUserCacheKey(user.Id)))

	require.NoError(t, MarkEnforcementAutoBanned(user.Id, common.GetTimestamp()))
	assert.False(t, server.Exists(getUserCacheKey(user.Id)), "ban must drop the cached enabled status")

	cached, err = GetUserCache(user.Id)
	require.NoError(t, err)
	assert.Equal(t, common.UserStatusDisabled, cached.Status)
}
