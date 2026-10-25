package model

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"eflow.local/new-api/common"
	"eflow.local/new-api/internal/testdb"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupPaymentGuardTestDB(t *testing.T) {
	t.Helper()
	db := testdb.OpenMySQL8(t, &User{}, &TopUp{}, &Log{})
	oldDB, oldLogDB := DB, LOG_DB
	oldRedisEnabled := common.RedisEnabled
	DB, LOG_DB = db, db
	common.RedisEnabled = false
	t.Cleanup(func() {
		DB, LOG_DB = oldDB, oldLogDB
		common.RedisEnabled = oldRedisEnabled
	})
}

func insertUserForPaymentGuardTest(t *testing.T, id int, quota int) {
	t.Helper()
	user := &User{
		Id:       id,
		Username: fmt.Sprintf("payment_guard_user_%d", id),
		AffCode:  fmt.Sprintf("guard-%d", id),
		Status:   common.UserStatusEnabled,
		Quota:    quota,
	}
	require.NoError(t, DB.Create(user).Error)
}

func insertTopUpForPaymentGuardTest(t *testing.T, tradeNo string, userID int, paymentProvider string) {
	t.Helper()
	topUp := &TopUp{
		UserId:          userID,
		Amount:          2,
		Money:           9.99,
		TradeNo:         tradeNo,
		PaymentMethod:   paymentProvider,
		PaymentProvider: paymentProvider,
		Status:          common.TopUpStatusPending,
		CreateTime:      time.Now().Unix(),
	}
	require.NoError(t, topUp.Insert())
}

func getTopUpStatusForPaymentGuardTest(t *testing.T, tradeNo string) string {
	t.Helper()
	topUp := GetTopUpByTradeNo(tradeNo)
	require.NotNil(t, topUp)
	return topUp.Status
}

func getUserQuotaForPaymentGuardTest(t *testing.T, userID int) int {
	t.Helper()
	user, err := GetUserById(userID, false)
	require.NoError(t, err)
	return user.Quota
}

func countTopUpLogsForPaymentGuardTest(t *testing.T, userID int) int64 {
	t.Helper()
	var count int64
	require.NoError(t, LOG_DB.Model(&Log{}).
		Where("user_id = ? AND type = ?", userID, LogTypeTopup).
		Count(&count).Error)
	return count
}

func TestManualCompleteTopUp_CreditsProviderAmountExactlyOnce(t *testing.T) {
	setupPaymentGuardTestDB(t)

	previousQuotaPerUnit := common.GetQuotaPerUnit()
	if err := common.SetQuotaPerUnit(10); err != nil {
		t.Fatalf("set quota per unit: %v", err)
	}
	t.Cleanup(func() {
		if err := common.SetQuotaPerUnit(previousQuotaPerUnit); err != nil {
			t.Fatalf("restore quota per unit: %v", err)
		}
	})

	testCases := []struct {
		name            string
		paymentProvider string
		wantQuota       int
	}{
		{name: "creem stored quota", paymentProvider: PaymentProviderCreem, wantQuota: 102},
		{name: "stripe amount", paymentProvider: PaymentProviderStripe, wantQuota: 120},
		{name: "epay amount", paymentProvider: PaymentProviderEpay, wantQuota: 120},
		{name: "waffo amount", paymentProvider: PaymentProviderWaffo, wantQuota: 120},
		{name: "waffo pancake amount", paymentProvider: PaymentProviderWaffoPancake, wantQuota: 120},
	}

	for i, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			userID := 101 + i
			insertUserForPaymentGuardTest(t, userID, 100)
			tradeNo := "manual-complete-" + tc.paymentProvider
			insertTopUpForPaymentGuardTest(t, tradeNo, userID, tc.paymentProvider)

			require.NoError(t, ManualCompleteTopUp(tradeNo, "127.0.0.1"))
			require.NoError(t, ManualCompleteTopUp(tradeNo, "127.0.0.1"))

			assert.Equal(t, tc.wantQuota, getUserQuotaForPaymentGuardTest(t, userID))
			assert.Equal(t, common.TopUpStatusSuccess, getTopUpStatusForPaymentGuardTest(t, tradeNo))
		})
	}
}

func TestManualCompleteTopUp_ConcurrentCallsCreditExactlyOnce(t *testing.T) {
	setupPaymentGuardTestDB(t)

	const userID = 171
	insertUserForPaymentGuardTest(t, userID, 100)
	insertTopUpForPaymentGuardTest(t, "manual-complete-concurrent", userID, PaymentProviderCreem)

	const workers = 8
	start := make(chan struct{})
	errors := make(chan error, workers)
	var wait sync.WaitGroup
	for range workers {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			errors <- ManualCompleteTopUp("manual-complete-concurrent", "127.0.0.1")
		}()
	}
	close(start)
	wait.Wait()
	close(errors)
	for err := range errors {
		require.NoError(t, err)
	}

	assert.Equal(t, 102, getUserQuotaForPaymentGuardTest(t, userID))
	assert.Equal(t, common.TopUpStatusSuccess, getTopUpStatusForPaymentGuardTest(t, "manual-complete-concurrent"))
}

func TestManualCompleteTopUp_RollsBackOrderWhenCreditFails(t *testing.T) {
	setupPaymentGuardTestDB(t)

	const missingUserID = 172
	insertTopUpForPaymentGuardTest(t, "manual-complete-missing-user", missingUserID, PaymentProviderCreem)

	require.Error(t, ManualCompleteTopUp("manual-complete-missing-user", "127.0.0.1"))
	assert.Equal(t, common.TopUpStatusPending, getTopUpStatusForPaymentGuardTest(t, "manual-complete-missing-user"))
}

func TestProviderRechargeCallbacksCreditAndLogExactlyOnce(t *testing.T) {
	setupPaymentGuardTestDB(t)

	previousQuotaPerUnit := common.GetQuotaPerUnit()
	require.NoError(t, common.SetQuotaPerUnit(10))
	t.Cleanup(func() {
		require.NoError(t, common.SetQuotaPerUnit(previousQuotaPerUnit))
	})

	tests := []struct {
		name       string
		provider   string
		userID     int
		wantQuota  int
		rechargeFn func(string) (bool, error)
	}{
		{
			name:      "stripe",
			provider:  PaymentProviderStripe,
			userID:    181,
			wantQuota: 120,
			rechargeFn: func(tradeNo string) (bool, error) {
				return Recharge(tradeNo, "cus_test", "127.0.0.1")
			},
		},
		{
			name:      "creem",
			provider:  PaymentProviderCreem,
			userID:    182,
			wantQuota: 102,
			rechargeFn: func(tradeNo string) (bool, error) {
				return RechargeCreem(tradeNo, "user@example.com", "Test User", "127.0.0.1")
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			tradeNo := "provider-idempotent-" + test.provider
			insertUserForPaymentGuardTest(t, test.userID, 100)
			insertTopUpForPaymentGuardTest(t, tradeNo, test.userID, test.provider)

			alreadyDone, err := test.rechargeFn(tradeNo)
			require.NoError(t, err)
			assert.False(t, alreadyDone)
			alreadyDone, err = test.rechargeFn(tradeNo)
			require.NoError(t, err)
			assert.True(t, alreadyDone)

			assert.Equal(t, test.wantQuota, getUserQuotaForPaymentGuardTest(t, test.userID))
			assert.Equal(t, common.TopUpStatusSuccess, getTopUpStatusForPaymentGuardTest(t, tradeNo))
			assert.Equal(t, int64(1), countTopUpLogsForPaymentGuardTest(t, test.userID))
		})
	}
}

func TestProviderRechargeCallbacksRollBackWhenCreditFails(t *testing.T) {
	setupPaymentGuardTestDB(t)

	tests := []struct {
		name       string
		provider   string
		tradeNo    string
		rechargeFn func(string) (bool, error)
	}{
		{
			name:     "stripe",
			provider: PaymentProviderStripe,
			tradeNo:  "stripe-missing-user",
			rechargeFn: func(tradeNo string) (bool, error) {
				return Recharge(tradeNo, "cus_test", "127.0.0.1")
			},
		},
		{
			name:     "creem",
			provider: PaymentProviderCreem,
			tradeNo:  "creem-missing-user",
			rechargeFn: func(tradeNo string) (bool, error) {
				return RechargeCreem(tradeNo, "", "", "127.0.0.1")
			},
		},
	}

	for i, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			insertTopUpForPaymentGuardTest(t, test.tradeNo, 190+i, test.provider)

			_, err := test.rechargeFn(test.tradeNo)
			require.Error(t, err)
			assert.Equal(t, common.TopUpStatusPending, getTopUpStatusForPaymentGuardTest(t, test.tradeNo))
		})
	}
}

func TestRechargeWaffoPancake_RejectsMismatchedPaymentMethod(t *testing.T) {
	setupPaymentGuardTestDB(t)

	insertUserForPaymentGuardTest(t, 101, 0)
	insertTopUpForPaymentGuardTest(t, "waffo-pancake-guard", 101, PaymentProviderStripe)

	err := RechargeWaffoPancake("waffo-pancake-guard")
	require.Error(t, err)

	topUp := GetTopUpByTradeNo("waffo-pancake-guard")
	require.NotNil(t, topUp)
	assert.Equal(t, common.TopUpStatusPending, topUp.Status)
	assert.Equal(t, 0, getUserQuotaForPaymentGuardTest(t, 101))
}

func TestUpdatePendingTopUpStatus_RejectsMismatchedPaymentProvider(t *testing.T) {
	setupPaymentGuardTestDB(t)

	testCases := []struct {
		name                    string
		tradeNo                 string
		storedPaymentProvider   string
		expectedPaymentProvider string
		targetStatus            string
	}{
		{
			name:                    "stripe expire",
			tradeNo:                 "stripe-expire-guard",
			storedPaymentProvider:   PaymentProviderCreem,
			expectedPaymentProvider: PaymentProviderStripe,
			targetStatus:            common.TopUpStatusExpired,
		},
		{
			name:                    "waffo failed",
			tradeNo:                 "waffo-failed-guard",
			storedPaymentProvider:   PaymentProviderStripe,
			expectedPaymentProvider: PaymentProviderWaffo,
			targetStatus:            common.TopUpStatusFailed,
		},
	}

	for i, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			userID := 150 + i
			insertUserForPaymentGuardTest(t, userID, 0)
			insertTopUpForPaymentGuardTest(t, tc.tradeNo, userID, tc.storedPaymentProvider)

			err := UpdatePendingTopUpStatus(tc.tradeNo, tc.expectedPaymentProvider, tc.targetStatus)
			require.ErrorIs(t, err, ErrPaymentMethodMismatch)
			assert.Equal(t, common.TopUpStatusPending, getTopUpStatusForPaymentGuardTest(t, tc.tradeNo))
		})
	}
}

func TestRechargeEpayRejectsPaidAmountMismatch(t *testing.T) {
	setupPaymentGuardTestDB(t)
	previousQuotaPerUnit := common.GetQuotaPerUnit()
	require.NoError(t, common.SetQuotaPerUnit(10))
	t.Cleanup(func() { require.NoError(t, common.SetQuotaPerUnit(previousQuotaPerUnit)) })

	insertUserForPaymentGuardTest(t, 191, 100)
	insertTopUpForPaymentGuardTest(t, "epay-amount-check", 191, PaymentProviderEpay)

	_, err := RechargeEpay("epay-amount-check", PaymentProviderEpay, "0.01", "127.0.0.1")
	require.ErrorIs(t, err, ErrTopUpAmountMismatch)
	assert.Equal(t, 100, getUserQuotaForPaymentGuardTest(t, 191))
	assert.Equal(t, common.TopUpStatusPending, getTopUpStatusForPaymentGuardTest(t, "epay-amount-check"))

	alreadyDone, err := RechargeEpay("epay-amount-check", PaymentProviderEpay, "9.99", "127.0.0.1")
	require.NoError(t, err)
	assert.False(t, alreadyDone)
	assert.Equal(t, 120, getUserQuotaForPaymentGuardTest(t, 191))
}
