package controller

import (
	"testing"

	"eflow.local/new-api/setting/billing_setting"
	"github.com/stretchr/testify/require"
)

func TestNormalizeExpressionBillingSyncData(t *testing.T) {
	data := map[string]any{
		"model_ratio": map[string]any{"ratio-model": float64(2)},
		billing_setting.BillingModeField: map[string]any{
			"legacy-fixed": "tiered_expr",
			"dynamic":      "flat_price",
		},
		billing_setting.BillingExprField: map[string]any{
			"legacy-fixed": `tier("base", p * 3 + c * 15 + cc1h * 6)`,
			"dynamic":      `tier("base", p * 3 + c * 15) * (hour("UTC") >= 8 ? 2 : 1)`,
		},
	}

	normalized := normalizeExpressionBillingSyncData(data)
	modes := valueMap(normalized[billing_setting.BillingModeField])
	require.Equal(t, billing_setting.BillingModeFlatPrice, modes["legacy-fixed"])
	require.Equal(t, billing_setting.BillingModeTieredExpr, modes["dynamic"])
	require.Equal(t, float64(2), valueMap(normalized["model_ratio"])["ratio-model"])
}
