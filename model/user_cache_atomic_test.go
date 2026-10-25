package model

import (
	"errors"
	"testing"
	"time"

	"eflow.local/new-api/common"
	"github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestQuotaCacheDeltaFollowsDatabaseSuccess(t *testing.T) {
	useModelIntegrationTestDB(t, &User{})
	server := miniredis.RunT(t)
	oldRDB := common.RDB
	common.RDB, common.RedisEnabled = redis.NewClient(&redis.Options{Addr: server.Addr()}), true
	t.Cleanup(func() { _ = common.RDB.Close(); common.RDB = oldRDB })
	user := User{Username: "quota-cache-order", Password: "password", Quota: 100}
	require.NoError(t, DB.Create(&user).Error)
	_, err := GetUserCache(user.Id)
	require.NoError(t, err)
	forced := errors.New("forced quota update failure")
	require.NoError(t, DB.Callback().Update().Before("gorm:update").Register("test:quota_failure", func(tx *gorm.DB) { tx.AddError(forced) }))
	require.ErrorIs(t, IncreaseUserQuota(user.Id, 10, true), forced)
	require.Never(t, func() bool { cached, e := GetUserCache(user.Id); return e != nil || cached.Quota != 100 }, 100*time.Millisecond, 5*time.Millisecond)
	require.ErrorIs(t, DecreaseUserQuota(user.Id, 20, true), forced)
	require.Never(t, func() bool { cached, e := GetUserCache(user.Id); return e != nil || cached.Quota != 100 }, 100*time.Millisecond, 5*time.Millisecond)
	require.NoError(t, DB.Callback().Update().Remove("test:quota_failure"))
	require.NoError(t, IncreaseUserQuota(user.Id, 10, true))
	require.Eventually(t, func() bool { cached, e := GetUserCache(user.Id); return e == nil && cached.Quota == 110 }, time.Second, 5*time.Millisecond)
	require.NoError(t, DecreaseUserQuota(user.Id, 20, true))
	require.Eventually(t, func() bool { cached, e := GetUserCache(user.Id); return e == nil && cached.Quota == 90 }, time.Second, 5*time.Millisecond)
	require.NoError(t, DB.First(&user, user.Id).Error)
	require.Equal(t, 90, user.Quota)
}

func TestEditRefreshesCachedProfileFieldsTogether(t *testing.T) {
	truncateTables(t)
	server := miniredis.RunT(t)
	oldRedisEnabled, oldRDB := common.RedisEnabled, common.RDB
	common.RDB, common.RedisEnabled = redis.NewClient(&redis.Options{Addr: server.Addr()}), true
	t.Cleanup(func() {
		_ = common.RDB.Close()
		common.RedisEnabled, common.RDB = oldRedisEnabled, oldRDB
	})

	user := User{Username: "cache-edit", Password: "password", AffCode: "cedt01", Group: "default"}
	require.NoError(t, DB.Create(&user).Error)
	_, err := GetUserCache(user.Id)
	require.NoError(t, err)

	user.Group = "vip"
	user.AdditionalCustomerTiers = NewGroupList([]string{"svip"})
	user.DisplayName = "renamed"
	require.NoError(t, user.Edit(false))

	cached, err := GetUserCache(user.Id)
	require.NoError(t, err)
	require.Equal(t, "vip", cached.Group)
	require.Equal(t, []string{"svip"}, cached.AdditionalCustomerTiers.Values())
	require.Equal(t, common.UserStatusEnabled, cached.Status)
	require.Equal(t, "cache-edit", cached.Username)
}
