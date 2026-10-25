package model

import (
	"testing"

	"eflow.local/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func useRedemptionSearchTestDB(t *testing.T) {
	t.Helper()
	useModelIntegrationTestDB(t, &Redemption{})
}

func TestSearchRedemptionsFiltersStatusAndKeyword(t *testing.T) {
	useRedemptionSearchTestDB(t)
	now := common.GetTimestamp()
	rows := []Redemption{
		{Name: "alpha-active", Key: "redemption-search-01", Status: common.RedemptionCodeStatusEnabled, ExpiredTime: 0},
		{Name: "alpha-future", Key: "redemption-search-02", Status: common.RedemptionCodeStatusEnabled, ExpiredTime: now + 3600},
		{Name: "alpha-expired", Key: "redemption-search-03", Status: common.RedemptionCodeStatusEnabled, ExpiredTime: now - 10},
		{Name: "beta-disabled", Key: "redemption-search-04", Status: common.RedemptionCodeStatusDisabled},
		{Name: "beta-used", Key: "redemption-search-05", Status: common.RedemptionCodeStatusUsed},
	}
	require.NoError(t, DB.Create(&rows).Error)
	sortOpts := NewRedemptionSortOptions("id", "desc")

	tests := []struct {
		name      string
		keyword   string
		status    string
		startIdx  int
		num       int
		wantTotal int64
		wantNames []string
	}{
		{name: "all", num: 10, wantTotal: 5, wantNames: []string{"beta-used", "beta-disabled", "alpha-expired", "alpha-future", "alpha-active"}},
		{name: "keyword", keyword: "alpha", num: 10, wantTotal: 3, wantNames: []string{"alpha-expired", "alpha-future", "alpha-active"}},
		{name: "enabled excludes expired", status: "1", num: 10, wantTotal: 2, wantNames: []string{"alpha-future", "alpha-active"}},
		{name: "expired", status: "expired", num: 10, wantTotal: 1, wantNames: []string{"alpha-expired"}},
		{name: "disabled", status: "2", num: 10, wantTotal: 1, wantNames: []string{"beta-disabled"}},
		{name: "used", status: "3", num: 10, wantTotal: 1, wantNames: []string{"beta-used"}},
		{name: "keyword and status pagination", keyword: "alpha", status: "1", startIdx: 1, num: 1, wantTotal: 2, wantNames: []string{"alpha-active"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, total, err := SearchRedemptions(tt.keyword, tt.status, tt.startIdx, tt.num, sortOpts)
			require.NoError(t, err)
			assert.Equal(t, tt.wantTotal, total)
			gotNames := make([]string, 0, len(got))
			for _, row := range got {
				gotNames = append(gotNames, row.Name)
			}
			assert.Equal(t, tt.wantNames, gotNames)
		})
	}
}

func TestRedeemCreditsQuotaExactlyOnce(t *testing.T) {
	useModelIntegrationTestDB(t, &User{}, &Redemption{}, &Log{})

	user := User{Username: "redeem-once", Password: "password", Status: common.UserStatusEnabled}
	require.NoError(t, DB.Create(&user).Error)
	redemption := Redemption{
		Name: "redeem-once", Key: "10000000000000000000000000000001",
		Status: common.RedemptionCodeStatusEnabled, Quota: 500, CreatedTime: common.GetTimestamp(),
	}
	require.NoError(t, DB.Create(&redemption).Error)

	quota, err := Redeem(redemption.Key, user.Id)
	require.NoError(t, err)
	assert.Equal(t, 500, quota)
	_, err = Redeem(redemption.Key, user.Id)
	require.Error(t, err)

	var stored User
	require.NoError(t, DB.First(&stored, user.Id).Error)
	assert.Equal(t, 500, stored.Quota)
}

func TestRedeemWalletLimitRollback(t *testing.T) {
	useModelIntegrationTestDB(t, &User{}, &Redemption{}, &Log{})
	user := User{Username: "redeem-limit", Password: "password", Quota: common.MaxQuota - 100}
	require.NoError(t, DB.Create(&user).Error)
	code := Redemption{Name: "limit", Key: "20000000000000000000000000000001", Status: common.RedemptionCodeStatusEnabled, Quota: 100}
	require.NoError(t, code.Insert())
	_, err := Redeem(code.Key, user.Id)
	require.Error(t, err)
	require.NoError(t, DB.First(&code, code.Id).Error)
	require.Equal(t, common.RedemptionCodeStatusEnabled, code.Status)
	require.Zero(t, code.UsedUserId)
	require.NoError(t, DB.First(&user, user.Id).Error)
	require.Equal(t, common.MaxQuota-100, user.Quota)
	// The rolled back code remains redeemable when capacity becomes available.
	require.NoError(t, DB.Model(&user).Update("quota", common.MaxQuota-101).Error)
	_, err = Redeem(code.Key, user.Id)
	require.NoError(t, err)
	require.NoError(t, DB.First(&user, user.Id).Error)
	require.Equal(t, common.MaxQuota-1, user.Quota)
}

func TestRedemptionRejectsInvalidQuota(t *testing.T) {
	useModelIntegrationTestDB(t, &User{}, &Redemption{}, &Log{})
	user := User{Username: "redeem-invalid", Password: "password", Quota: 100}
	require.NoError(t, DB.Create(&user).Error)
	for _, quota := range []int{-1, 0, common.MaxQuota} {
		code := Redemption{Name: "invalid", Key: "30000000000000000000000000000001", Status: common.RedemptionCodeStatusEnabled, Quota: quota}
		require.ErrorIs(t, code.Insert(), ErrInvalidTopUpQuota)
		require.ErrorIs(t, code.Update(), ErrInvalidTopUpQuota)
		// Legacy/externally inserted invalid values cannot debit a wallet.
		require.NoError(t, DB.Create(&code).Error)
		require.NoError(t, DB.Model(&code).Update("quota", quota).Error)
		_, err := Redeem(code.Key, user.Id)
		require.Error(t, err)
		require.NoError(t, DB.First(&code, code.Id).Error)
		require.Equal(t, common.RedemptionCodeStatusEnabled, code.Status)
		require.NoError(t, DB.Unscoped().Delete(&code).Error)
	}
	require.NoError(t, DB.First(&user, user.Id).Error)
	require.Equal(t, 100, user.Quota)
}

func TestRedemptionStatusOnlyUpdateSkipsQuotaCheck(t *testing.T) {
	useModelIntegrationTestDB(t, &User{}, &Redemption{}, &Log{})
	for key, quota := range map[string]int{"40000000000000000000000000000001": -1, "40000000000000000000000000000002": 0} {
		code := Redemption{Name: "legacy", Key: key, Status: common.RedemptionCodeStatusEnabled, Quota: 1}
		require.NoError(t, DB.Create(&code).Error)
		require.NoError(t, DB.Model(&code).Update("quota", quota).Error)
		code.Status, code.Name = common.RedemptionCodeStatusDisabled, "ignored"
		require.ErrorIs(t, code.Update(), ErrInvalidTopUpQuota)
		require.NoError(t, code.UpdateStatus(), "legacy codes must stay toggleable")
		var stored Redemption
		require.NoError(t, DB.First(&stored, code.Id).Error)
		require.Equal(t, common.RedemptionCodeStatusDisabled, stored.Status)
		require.Equal(t, "legacy", stored.Name, "status-only writes nothing else")
		require.Equal(t, quota, stored.Quota)
		code.Status = common.RedemptionCodeStatusEnabled
		require.NoError(t, code.UpdateStatus())
	}
	used := Redemption{Name: "used", Key: "40000000000000000000000000000009", Status: common.RedemptionCodeStatusUsed, Quota: 1}
	require.NoError(t, DB.Create(&used).Error)
	used.Status = common.RedemptionCodeStatusEnabled
	require.ErrorIs(t, used.UpdateStatus(), ErrRedemptionAlreadyUsed)
}
