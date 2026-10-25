package model

import (
	"strconv"
	"sync"
	"testing"

	"eflow.local/new-api/common"
	"eflow.local/new-api/setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRedemptionUpdateNeverRewritesUsedCode(t *testing.T) {
	useModelIntegrationTestDB(t, &User{}, &Redemption{}, &Log{})
	user := User{Username: "redeem-used-edit", Password: "password", Status: common.UserStatusEnabled}
	require.NoError(t, DB.Create(&user).Error)
	code := Redemption{Name: "used-edit", Key: "10000000000000000000000000000002", Status: common.RedemptionCodeStatusEnabled, Quota: 500, CreatedTime: common.GetTimestamp()}
	require.NoError(t, DB.Create(&code).Error)

	stale := code
	_, err := Redeem(code.Key, user.Id)
	require.NoError(t, err)

	// An edit prepared before the redemption must not re-enable the code.
	stale.Status = common.RedemptionCodeStatusEnabled
	require.ErrorIs(t, stale.Update(), ErrRedemptionAlreadyUsed)
	var stored Redemption
	require.NoError(t, DB.First(&stored, code.Id).Error)
	assert.Equal(t, common.RedemptionCodeStatusUsed, stored.Status)

	// An unchanged edit of an unused code is not an error.
	other := Redemption{Name: "unused-edit", Key: "10000000000000000000000000000003", Status: common.RedemptionCodeStatusEnabled, Quota: 500, CreatedTime: common.GetTimestamp()}
	require.NoError(t, DB.Create(&other).Error)
	require.NoError(t, other.Update())
}

func TestRedeemRollsBackWhenRedeemerIsMissing(t *testing.T) {
	useModelIntegrationTestDB(t, &User{}, &Redemption{}, &Log{})
	code := Redemption{Name: "missing-user", Key: "10000000000000000000000000000004", Status: common.RedemptionCodeStatusEnabled, Quota: 500, CreatedTime: common.GetTimestamp()}
	require.NoError(t, DB.Create(&code).Error)

	_, err := Redeem(code.Key, 987654)
	require.Error(t, err)
	var stored Redemption
	require.NoError(t, DB.First(&stored, code.Id).Error)
	assert.Equal(t, common.RedemptionCodeStatusEnabled, stored.Status, "the code stays usable")
}

func TestGenerateUserInvitationCodeConcurrentRequestsRespectQuota(t *testing.T) {
	useModelIntegrationTestDB(t, &User{}, &InvitationCode{}, &TopUp{})
	previousAuth := common.GetAuthSettings()
	previousPolicy := setting.GetInvitationCodePolicyJSON()
	t.Cleanup(func() {
		require.NoError(t, common.UpdateAuthSettings(map[string]string{"InvitationCodeUserGenerateEnabled": strconv.FormatBool(previousAuth.InvitationCodeUserGenerateEnabled)}))
		require.NoError(t, setting.UpdateInvitationCodePolicy(previousPolicy))
	})
	require.NoError(t, common.UpdateAuthSettings(map[string]string{"InvitationCodeUserGenerateEnabled": "true"}))
	require.NoError(t, setting.UpdateInvitationCodePolicy(`{"min_account_age_days":0,"default_generate_quota":1,"monthly_generate_quota":0,"default_code_max_uses":1,"default_code_valid_days":30,"group_quotas":{},"role_quotas":{}}`))

	user := User{Username: "invite-race", Password: "password", Status: common.UserStatusEnabled, Role: common.RoleCommonUser, Group: "default", AffCode: "invite-race"}
	require.NoError(t, DB.Create(&user).Error)

	var wg sync.WaitGroup
	for range 5 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = GenerateUserInvitationCode(&user, nil)
		}()
	}
	wg.Wait()

	var created int64
	require.NoError(t, DB.Model(&InvitationCode{}).Where("created_by = ?", user.Id).Count(&created).Error)
	assert.EqualValues(t, 1, created, "concurrent requests must not exceed the lifetime quota")
}
