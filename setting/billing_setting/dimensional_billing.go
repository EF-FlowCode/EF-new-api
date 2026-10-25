package billing_setting

import "strings"

const BillingModeDimensional = "dimensional"

type DimensionalPricingTier struct {
	Key   string  `json:"key"`
	Label string  `json:"label"`
	Ratio float64 `json:"ratio"`
}

type DimensionalPricing struct {
	Mode           string                   `json:"-"`
	Unit           string                   `json:"unit"`
	Dimension      string                   `json:"dimension"`
	SampleQuantity float64                  `json:"sample_quantity"`
	Tiers          []DimensionalPricingTier `json:"tiers"`
}

var happyhorseDimensionalPricing = DimensionalPricing{
	Mode:           BillingModeDimensional,
	Unit:           "second",
	Dimension:      "resolution",
	SampleQuantity: 5,
	Tiers: []DimensionalPricingTier{
		{Key: "720P", Label: "720P", Ratio: 1},
		{Key: "1080P", Label: "1080P", Ratio: 1.778},
	},
}

var happyhorseDimensionalModels = map[string]struct{}{
	"happyhorse-1.0-t2v":        {},
	"happyhorse-1.0-i2v":        {},
	"happyhorse-1.0-r2v":        {},
	"happyhorse-1.0-video-edit": {},
}

func GetDimensionalPricing(model string) (DimensionalPricing, bool) {
	if _, ok := happyhorseDimensionalModels[model]; !ok {
		return DimensionalPricing{}, false
	}

	pricing := happyhorseDimensionalPricing
	pricing.Tiers = append([]DimensionalPricingTier(nil), pricing.Tiers...)
	return pricing, true
}

func GetDimensionalTierRatio(model, tierKey string) (float64, bool) {
	pricing, ok := GetDimensionalPricing(model)
	if !ok {
		return 0, false
	}
	for _, tier := range pricing.Tiers {
		if strings.EqualFold(tier.Key, strings.TrimSpace(tierKey)) {
			return tier.Ratio, true
		}
	}
	return 0, false
}
