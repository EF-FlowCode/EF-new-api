package model

import (
	"testing"

	"eflow.local/new-api/setting/billing_setting"
	"github.com/stretchr/testify/require"
)

func TestAttachHappyhorseDimensionalPricing(t *testing.T) {
	pricing := Pricing{ModelName: "happyhorse-1.0-t2v", ModelPrice: 0.36, QuotaType: 1}

	require.True(t, attachDimensionalPricing(&pricing, pricing.ModelName))
	require.Equal(t, billing_setting.BillingModeDimensional, pricing.BillingMode)
	require.NotNil(t, pricing.DimensionalPricing)
	require.Equal(t, "second", pricing.DimensionalPricing.Unit)
	require.Equal(t, "resolution", pricing.DimensionalPricing.Dimension)
	require.Len(t, pricing.DimensionalPricing.Tiers, 2)
}

func TestAttachDimensionalPricingIgnoresOtherModels(t *testing.T) {
	pricing := Pricing{ModelName: "gpt-test"}
	require.False(t, attachDimensionalPricing(&pricing, pricing.ModelName))
	require.Empty(t, pricing.BillingMode)
	require.Nil(t, pricing.DimensionalPricing)
}
