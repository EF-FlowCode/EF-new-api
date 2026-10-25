package billingexpr_test

import (
	"testing"

	"eflow.local/new-api/pkg/billingexpr"
	"github.com/stretchr/testify/require"
)

func TestParseFlatTokenBillingExprAcceptsStrictSingleTier(t *testing.T) {
	tests := []struct {
		name       string
		expression string
		label      string
	}{
		{name: "base label", expression: `tier("base", p * 3 + c * 15 + cr * 0.3 + cc * 3.75 + cc1h * 6)`, label: "base"},
		{name: "default label", expression: `tier("default", c * 0 + p * 0)`, label: "default"},
		{name: "version prefix", expression: `v1:tier("legacy", p * 1e-3 + c * 2.5)`, label: "legacy"},
		{name: "multimodal", expression: `tier("media", p * 0 + c * 2 + img * 0 + img_o * 4 + ai * 5 + ao * 6)`, label: "media"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parsed, ok := billingexpr.ParseFlatTokenBillingExpr(tt.expression)
			require.True(t, ok)
			require.Equal(t, tt.label, parsed.Label)
			require.Contains(t, parsed.Coefficients, "p")
			require.Contains(t, parsed.Coefficients, "c")
		})
	}
}

func TestParseFlatTokenBillingExprRejectsDynamicOrAmbiguousShapes(t *testing.T) {
	tests := map[string]string{
		"time multiplier":   `(tier("base", p * 4.5 + c * 13.5)) * (hour("Asia/Shanghai") >= 8 && hour("Asia/Shanghai") < 22 ? 2 : 1)`,
		"conditional tiers": `p < 1000 ? tier("small", p * 1 + c * 2) : tier("large", p * 2 + c * 4)`,
		"multiple tiers":    `tier("first", p * 1 + c * 2) + tier("second", p * 3 + c * 4)`,
		"request probe":     `tier("base", p * 1 + c * (param("fast") == true ? 4 : 2))`,
		"missing input":     `tier("base", c * 2)`,
		"missing output":    `tier("base", p * 1)`,
		"duplicate input":   `tier("base", p * 1 + c * 2 + p * 3)`,
		"reversed multiply": `tier("base", 1 * p + c * 2)`,
		"negative price":    `tier("base", p * -1 + c * 2)`,
		"extra arithmetic":  `tier("base", p * 1 + c * 2) / 1000000`,
	}
	for name, expression := range tests {
		t.Run(name, func(t *testing.T) {
			_, ok := billingexpr.ParseFlatTokenBillingExpr(expression)
			require.False(t, ok)
		})
	}
}
