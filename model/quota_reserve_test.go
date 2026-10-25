package model

import (
	"sync"
	"testing"

	"eflow.local/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTryReserveQuotaWithoutRedis(t *testing.T) {
	truncateTables(t)
	oldRedisEnabled := common.RedisEnabled
	oldBatchUpdateEnabled := common.BatchUpdateEnabled
	common.RedisEnabled = false
	common.BatchUpdateEnabled = false
	t.Cleanup(func() {
		common.RedisEnabled = oldRedisEnabled
		common.BatchUpdateEnabled = oldBatchUpdateEnabled
	})

	user := User{Username: "reserve-user", Password: "password", Status: common.UserStatusEnabled, Quota: 100}
	require.NoError(t, DB.Create(&user).Error)
	reserved, err := TryReserveUserQuota(user.Id, 60)
	require.NoError(t, err)
	assert.True(t, reserved)
	reserved, err = TryReserveUserQuota(user.Id, 41)
	require.NoError(t, err)
	assert.False(t, reserved)

	token := Token{UserId: user.Id, Key: "reserve-token", Status: common.TokenStatusEnabled, RemainQuota: 80}
	require.NoError(t, DB.Create(&token).Error)
	reserved, err = TryReserveTokenQuota(token.Id, token.Key, 25, false)
	require.NoError(t, err)
	assert.True(t, reserved)
	reserved, err = TryReserveTokenQuota(token.Id, token.Key, 56, false)
	require.NoError(t, err)
	assert.False(t, reserved)

	var storedUser User
	require.NoError(t, DB.First(&storedUser, user.Id).Error)
	assert.Equal(t, 40, storedUser.Quota)
	var storedToken Token
	require.NoError(t, DB.First(&storedToken, token.Id).Error)
	assert.Equal(t, 55, storedToken.RemainQuota)
	assert.Equal(t, 25, storedToken.UsedQuota)
}

func TestConcurrentUserQuotaReservationCannotOverdraw(t *testing.T) {
	truncateTables(t)
	oldRedisEnabled := common.RedisEnabled
	oldBatchUpdateEnabled := common.BatchUpdateEnabled
	common.RedisEnabled = false
	common.BatchUpdateEnabled = false
	t.Cleanup(func() {
		common.RedisEnabled = oldRedisEnabled
		common.BatchUpdateEnabled = oldBatchUpdateEnabled
	})

	user := User{Username: "reserve-race", Password: "password", Status: common.UserStatusEnabled, Quota: 100}
	require.NoError(t, DB.Create(&user).Error)

	const workers = 4
	results := make(chan bool, workers)
	errs := make(chan error, workers)
	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			reserved, err := TryReserveUserQuota(user.Id, 60)
			results <- reserved
			errs <- err
		}()
	}
	wg.Wait()
	close(results)
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	wins := 0
	for reserved := range results {
		if reserved {
			wins++
		}
	}
	assert.Equal(t, 1, wins)

	var stored User
	require.NoError(t, DB.First(&stored, user.Id).Error)
	assert.Equal(t, 40, stored.Quota)
}

func TestConcurrentTokenQuotaReservationCannotOverdraw(t *testing.T) {
	truncateTables(t)
	oldRedisEnabled := common.RedisEnabled
	oldBatchUpdateEnabled := common.BatchUpdateEnabled
	common.RedisEnabled = false
	common.BatchUpdateEnabled = false
	t.Cleanup(func() {
		common.RedisEnabled = oldRedisEnabled
		common.BatchUpdateEnabled = oldBatchUpdateEnabled
	})

	user := User{Username: "token-reserve-owner", Password: "password", Status: common.UserStatusEnabled}
	require.NoError(t, DB.Create(&user).Error)
	token := Token{UserId: user.Id, Key: "token-reserve-race", Status: common.TokenStatusEnabled, RemainQuota: 100}
	require.NoError(t, DB.Create(&token).Error)

	const workers = 4
	results := make(chan bool, workers)
	errs := make(chan error, workers)
	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			reserved, err := TryReserveTokenQuota(token.Id, token.Key, 60, false)
			results <- reserved
			errs <- err
		}()
	}
	wg.Wait()
	close(results)
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	wins := 0
	for reserved := range results {
		if reserved {
			wins++
		}
	}
	assert.Equal(t, 1, wins)

	var stored Token
	require.NoError(t, DB.First(&stored, token.Id).Error)
	assert.Equal(t, 40, stored.RemainQuota)
	assert.Equal(t, 60, stored.UsedQuota)
}
