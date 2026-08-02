package model

import "testing"

func TestUpdateOptionRejectsInvalidPricingJSONBeforePersistence(t *testing.T) {
	db := useBillingQuotaTestDB(t, &Option{})
	if err := UpdateOption("ModelRatio", `{invalid`); err == nil {
		t.Fatal("expected invalid pricing JSON to be rejected")
	}
	var count int64
	if err := db.Model(&Option{}).Where("key = ?", "ModelRatio").Count(&count).Error; err != nil {
		t.Fatalf("count options: %v", err)
	}
	if count != 0 {
		t.Fatalf("invalid option was persisted: count=%d", count)
	}
}
