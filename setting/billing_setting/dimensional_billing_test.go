package billing_setting

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestHappyhorseDimensionalPricing(t *testing.T) {
	models := []string{
		"happyhorse-1.0-t2v",
		"happyhorse-1.0-i2v",
		"happyhorse-1.0-r2v",
		"happyhorse-1.0-video-edit",
	}

	for _, modelName := range models {
		t.Run(modelName, func(t *testing.T) {
			pricing, ok := GetDimensionalPricing(modelName)
			require.True(t, ok)
			require.Equal(t, BillingModeDimensional, pricing.Mode)
			require.Equal(t, "second", pricing.Unit)
			require.Equal(t, "resolution", pricing.Dimension)
			require.Equal(t, float64(5), pricing.SampleQuantity)
			require.Equal(t, []DimensionalPricingTier{
				{Key: "720P", Label: "720P", Ratio: 1},
				{Key: "1080P", Label: "1080P", Ratio: 1.778},
			}, pricing.Tiers)

			ratio, ok := GetDimensionalTierRatio(modelName, "1080P")
			require.True(t, ok)
			require.InDelta(t, 1.778, ratio, 1e-9)
		})
	}

	_, ok := GetDimensionalPricing("unknown-model")
	require.False(t, ok)
}
