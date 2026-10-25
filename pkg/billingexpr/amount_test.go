package billingexpr

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAmountExpressionPreservesQuota(t *testing.T) {
	for _, expression := range []string{
		`tier("base", p * 2 + c * 7.5)`,
		`v1:len <= 1000 ? tier("short", p * 1.1 + c * 3) : tier("long", p * 2.2 + c * 6)`,
		`tier("video", param("seconds") * 0.01 * 1000000)`,
		`tier("rounding", ceil(p / 1000) * 2500)`,
		`tier("cache", p * 2 + c * 5 + cr * 0.1 + cc * 3 + cc1h * 4 + img * 6 + img_o * 7 + ai * 8 + ao * 9)`,
		`tier("free", 0) // preserve this comment`,
	} {
		t.Run(expression, func(t *testing.T) {
			converted, err := AmountExpression(expression)
			require.NoError(t, err)
			again, err := AmountExpression(converted)
			require.NoError(t, err)
			require.Equal(t, converted, again)
			for _, tokens := range []float64{0, 1, 999, 1000, 1001, 999999, 1000000} {
				params := TokenParams{P: tokens, C: tokens / 3, Len: tokens, CR: 15, CC: 20, CC1h: 25, Img: 30, ImgO: 35, AI: 40, AO: 45}
				request := RequestInput{Body: []byte(`{"seconds":5}`)}
				oldCost, oldTrace, err := RunExprWithRequest(expression, params, request)
				require.NoError(t, err)
				amount, trace, err := RunExprWithRequest(converted, params, request)
				require.NoError(t, err)
				require.Equal(t, oldCost/1_000_000, amount)
				require.Equal(t, oldTrace.MatchedTier, trace.MatchedTier)
				for _, group := range []float64{0, 0.05, 1, 1.8, 10} {
					snapshot := &BillingSnapshot{ExprString: converted, ExprHash: ExprHashString(converted), ExprVersion: 2, GroupRatio: group, QuotaPerUnit: 500000}
					result, err := ComputeTieredQuotaWithRequest(snapshot, params, request)
					require.NoError(t, err)
					require.Equal(t, QuotaRound(oldCost/1_000_000*500000*group), result.ActualQuotaAfterGroup)
				}
			}
		})
	}
}

func TestAmountExpressionExplicitCurrency(t *testing.T) {
	for expression, expected := range map[string]float64{
		`v2:tier("tokens", p * 2 / 1000000)`:        0.002,
		`v2:tier("video", param("seconds") * 0.01)`: 0.05,
	} {
		amount, _, err := RunExprWithRequest(expression, TokenParams{P: 1000}, RequestInput{Body: []byte(`{"seconds":5}`)})
		require.NoError(t, err)
		require.Equal(t, expected, amount)
		require.Equal(t, expected*500000, ExpressionQuota(amount, expression, 500000))
	}
	for _, expression := range []string{`v3:p`, `v2:(`, `v2:1/0`} {
		_, _, err := RunExpr(expression, TokenParams{})
		require.Error(t, err)
	}
	legacy := `tier("base", p * 2.5 + c * 15 + cr * 0)`
	converted, err := AmountExpression(legacy)
	require.NoError(t, err)
	before, ok := ParseFlatTokenBillingExpr(legacy)
	require.True(t, ok)
	after, ok := ParseFlatTokenBillingExpr(converted)
	require.True(t, ok)
	require.Equal(t, before, after)
}
