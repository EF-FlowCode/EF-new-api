package model

import (
	"testing"

	"eflow.local/new-api/common"
)

func TestUpdateOptionRejectsInvalidPricingJSONBeforePersistence(t *testing.T) {
	db := useModelIntegrationTestDB(t, &Option{})
	if err := UpdateOption("ModelRatio", `{invalid`); err == nil {
		t.Fatal("expected invalid pricing JSON to be rejected")
	}
	var count int64
	if err := db.Model(&Option{}).Where(&Option{Key: "ModelRatio"}).Count(&count).Error; err != nil {
		t.Fatalf("count options: %v", err)
	}
	if count != 0 {
		t.Fatalf("invalid option was persisted: count=%d", count)
	}
}

func TestUpdateOptionRejectsInvalidQuotaPerUnitWithoutPublishing(t *testing.T) {
	db := useModelIntegrationTestDB(t, &Option{})
	previous := common.GetQuotaPerUnit()
	t.Cleanup(func() {
		if err := common.SetQuotaPerUnit(previous); err != nil {
			t.Fatalf("restore quota per unit: %v", err)
		}
	})

	const baseline = 654_321.25
	if err := common.SetQuotaPerUnit(baseline); err != nil {
		t.Fatalf("set baseline: %v", err)
	}

	for _, value := range []string{"not-a-number", "NaN", "+Inf", "-Inf", "0", "-1"} {
		t.Run(value, func(t *testing.T) {
			if err := UpdateOption("QuotaPerUnit", value); err == nil {
				t.Fatalf("UpdateOption(QuotaPerUnit, %q) unexpectedly succeeded", value)
			}
			if got := common.GetQuotaPerUnit(); got != baseline {
				t.Fatalf("invalid value %q changed runtime quota per unit to %v", value, got)
			}
			var count int64
			if err := db.Model(&Option{}).Where("`key` = ?", "QuotaPerUnit").Count(&count).Error; err != nil {
				t.Fatalf("count quota option: %v", err)
			}
			if count != 0 {
				t.Fatalf("invalid quota option %q was persisted", value)
			}
		})
	}
}
