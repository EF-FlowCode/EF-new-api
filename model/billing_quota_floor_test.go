package model

import "testing"

func TestAdjustBillingQuotaAtomicChargesBeyondRemainingWallet(t *testing.T) {
	db := useModelIntegrationTestDB(t, &User{}, &Token{})
	user := User{Username: "settle-overdraw", Password: "password", Quota: 100}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	token := Token{UserId: user.Id, Key: "settle-overdraw-token", RemainQuota: 50}
	if err := db.Create(&token).Error; err != nil {
		t.Fatalf("create token: %v", err)
	}

	if err := AdjustBillingQuotaAtomic(BillingQuotaAdjustment{UserID: user.Id, TokenID: token.Id, TokenKey: token.Key, Delta: 150}); err != nil {
		t.Fatalf("settlement beyond wallet must still charge: %v", err)
	}
	if err := db.First(&user, user.Id).Error; err != nil {
		t.Fatalf("reload user: %v", err)
	}
	if err := db.First(&token, token.Id).Error; err != nil {
		t.Fatalf("reload token: %v", err)
	}
	if user.Quota != -50 || token.RemainQuota != -100 || token.UsedQuota != 150 {
		t.Fatalf("unexpected quotas: user=%d token remain=%d used=%d", user.Quota, token.RemainQuota, token.UsedQuota)
	}
}

func TestReserveCachedUserQuotaKeepsDatabaseFloor(t *testing.T) {
	db := useModelIntegrationTestDB(t, &User{}, &Token{})
	user := User{Username: "reserve-floor", Password: "password", Quota: 10}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	token := Token{UserId: user.Id, Key: "reserve-floor-token", RemainQuota: 10}
	if err := db.Create(&token).Error; err != nil {
		t.Fatalf("create token: %v", err)
	}

	if reserved, err := reserveCachedUserQuota(user.Id, 20); err != nil || reserved {
		t.Fatalf("stale cache must not overdraw the wallet: reserved=%v err=%v", reserved, err)
	}
	if reserved, err := reserveCachedTokenQuota(token.Id, 20); err != nil || reserved {
		t.Fatalf("stale cache must not overdraw the token: reserved=%v err=%v", reserved, err)
	}
	if err := db.First(&user, user.Id).Error; err != nil {
		t.Fatalf("reload user: %v", err)
	}
	if err := db.First(&token, token.Id).Error; err != nil {
		t.Fatalf("reload token: %v", err)
	}
	if user.Quota != 10 || token.RemainQuota != 10 {
		t.Fatalf("balances changed: user=%d token=%d", user.Quota, token.RemainQuota)
	}
}

func TestSettlementAndRefundStillApplyToSoftDeletedUser(t *testing.T) {
	db := useModelIntegrationTestDB(t, &User{}, &Token{})
	user := User{Username: "settle-soft-deleted", Password: "password", Quota: 100}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := db.Delete(&user).Error; err != nil {
		t.Fatalf("soft delete user: %v", err)
	}

	// The deleted_at scope used to match no row, so the settlement errored and
	// rolled back and the refund was silently skipped.
	if err := AdjustBillingQuotaAtomic(BillingQuotaAdjustment{UserID: user.Id, Delta: 30}); err != nil {
		t.Fatalf("settlement for a soft-deleted user must apply: %v", err)
	}
	if err := RefundBillingQuotaAtomic(BillingQuotaRefund{UserID: user.Id, FundingAmount: 10}); err != nil {
		t.Fatalf("refund for a soft-deleted user must apply: %v", err)
	}
	var reloaded User
	if err := db.Unscoped().First(&reloaded, user.Id).Error; err != nil {
		t.Fatalf("reload user: %v", err)
	}
	if reloaded.Quota != 80 {
		t.Fatalf("quota = %d, want 80", reloaded.Quota)
	}

	if err := AdjustBillingQuotaAtomic(BillingQuotaAdjustment{UserID: user.Id + 100000, Delta: 1}); err == nil {
		t.Fatal("a user that no longer exists at all must still be reported")
	}
}
