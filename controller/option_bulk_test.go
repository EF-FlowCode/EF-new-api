package controller

import "testing"

func TestIsBulkPricingOptionAllowsCustomerTierPayload(t *testing.T) {
	if !isBulkPricingOption("payment_setting.amount_discount") {
		t.Fatal("customer tier bulk save must allow payment_setting.amount_discount")
	}
	if isBulkPricingOption("unrelated.option") {
		t.Fatal("bulk pricing endpoint must reject unrelated options")
	}
}
