package controller

import (
	"testing"

	"eflow.local/new-api/model"
)

func TestFilterPricingGroupEndpointOverridesKeepsDefaultWithoutUsableGroups(t *testing.T) {
	defaultOverride := model.DefaultPricingEndpointOverride
	got := filterPricingGroupEndpointOverrides(
		map[string]model.PricingGroupEndpointOverride{
			"default":     defaultOverride,
			"1_vip_china": {Path: "/v1/custom", Method: "POST"},
		},
		nil,
	)

	if got["default"] != defaultOverride {
		t.Fatalf("default endpoint fallback was removed: %+v", got)
	}
	if _, ok := got["1_vip_china"]; ok {
		t.Fatalf("unusable group override leaked into response: %+v", got)
	}
}
