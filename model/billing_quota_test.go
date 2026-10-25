package model

import "testing"

func TestAdjustBillingQuotaAtomicRollsBackFundingWhenTokenUpdateFails(t *testing.T) {
	db := useModelIntegrationTestDB(t, &User{})
	user := User{Username: "atomic-rollback", Password: "password", Quota: 100}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}

	err := AdjustBillingQuotaAtomic(BillingQuotaAdjustment{
		UserID:  user.Id,
		TokenID: 999,
		Delta:   10,
	})
	if err == nil {
		t.Fatal("expected missing token table to fail the transaction")
	}
	var got User
	if err = db.First(&got, user.Id).Error; err != nil {
		t.Fatalf("reload user: %v", err)
	}
	if got.Quota != 100 {
		t.Fatalf("wallet quota changed despite rollback: got %d want 100", got.Quota)
	}
}

func TestAdjustBillingQuotaAtomicUpdatesWalletAndTokenTogether(t *testing.T) {
	db := useModelIntegrationTestDB(t, &User{}, &Token{})
	user := User{Username: "atomic-success", Password: "password", Quota: 100}
	token := Token{UserId: 1, Key: "atomic-token", RemainQuota: 80, UsedQuota: 20}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	token.UserId = user.Id
	if err := db.Create(&token).Error; err != nil {
		t.Fatalf("create token: %v", err)
	}

	if err := AdjustBillingQuotaAtomic(BillingQuotaAdjustment{
		UserID:   user.Id,
		TokenID:  token.Id,
		TokenKey: token.Key,
		Delta:    15,
	}); err != nil {
		t.Fatalf("adjust quota: %v", err)
	}
	if err := db.First(&user, user.Id).Error; err != nil {
		t.Fatalf("reload user: %v", err)
	}
	if err := db.First(&token, token.Id).Error; err != nil {
		t.Fatalf("reload token: %v", err)
	}
	if user.Quota != 85 || token.RemainQuota != 65 || token.UsedQuota != 35 {
		t.Fatalf("unexpected quotas: user=%d token remain=%d used=%d", user.Quota, token.RemainQuota, token.UsedQuota)
	}
}
