package service

import (
	"eflow.local/new-api/dto"
	"eflow.local/new-api/pkg/billingexpr"
	"eflow.local/new-api/pkg/knowledge"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestKnowledgeSplitAndUnsplitExpressionCost(t *testing.T) {
	input, image := 2.0, 4.0
	expression := (knowledge.Prices{Input: &input, Image: &image}).Expression()
	for _, tc := range []struct {
		visual   int
		expected float64
	}{{600, 0.0032}, {0, 0.002}} {
		u := &dto.Usage{PromptTokens: 1000, CompletionTokens: 9, TotalTokens: 1009}
		u.PromptTokensDetails.ImageTokens = tc.visual
		params := BuildTieredTokenParams(u, false, billingexpr.UsedVars(expression))
		cost, _, err := billingexpr.RunExpr(expression, params)
		require.NoError(t, err)
		require.Equal(t, tc.expected, cost)
		result, err := billingexpr.ComputeTieredQuota(&billingexpr.BillingSnapshot{
			ExprString: expression, ExprHash: billingexpr.ExprHashString(expression), QuotaPerUnit: 500000, GroupRatio: 0.5,
		}, params)
		require.NoError(t, err)
		require.Equal(t, int(tc.expected*250000), result.ActualQuotaAfterGroup)
	}
}
