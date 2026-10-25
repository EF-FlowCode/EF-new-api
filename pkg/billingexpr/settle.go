package billingexpr

import (
	"fmt"
	"math"

	"eflow.local/new-api/common"
)

// quotaConversion converts raw expression output to quota based on the
// expression version. This is the central dispatch point for future versions
// that may use a different conversion formula.
func quotaConversion(exprOutput float64, snap *BillingSnapshot) float64 {
	return ExpressionQuota(exprOutput, snap.ExprString, snap.QuotaPerUnit)
}

// ComputeTieredQuota runs the Expr from a frozen BillingSnapshot against
// actual token counts and returns the settlement result.
func ComputeTieredQuota(snap *BillingSnapshot, params TokenParams) (TieredResult, error) {
	return ComputeTieredQuotaWithRequest(snap, params, RequestInput{})
}

func ComputeTieredQuotaWithRequest(snap *BillingSnapshot, params TokenParams, request RequestInput) (TieredResult, error) {
	if snap == nil {
		return TieredResult{}, fmt.Errorf("billing snapshot is nil")
	}
	cost, trace, err := RunExprByHashWithRequest(snap.ExprString, snap.ExprHash, params, request)
	if err != nil {
		return TieredResult{}, err
	}

	quotaBeforeGroup := quotaConversion(cost, snap)
	quotaAfterGroup := quotaBeforeGroup * snap.GroupRatio
	if math.IsNaN(quotaBeforeGroup) || math.IsInf(quotaBeforeGroup, 0) || quotaBeforeGroup < 0 ||
		math.IsNaN(quotaAfterGroup) || math.IsInf(quotaAfterGroup, 0) || quotaAfterGroup < 0 {
		return TieredResult{}, fmt.Errorf("tiered quota is invalid or out of range: before=%v after=%v", quotaBeforeGroup, quotaAfterGroup)
	}
	afterGroup, clamp := common.QuotaRoundChecked(quotaAfterGroup)
	crossed := trace.MatchedTier != snap.EstimatedTier

	return TieredResult{
		ActualQuotaBeforeGroup: quotaBeforeGroup,
		ActualQuotaAfterGroup:  afterGroup,
		MatchedTier:            trace.MatchedTier,
		CrossedTier:            crossed,
		Clamp:                  clamp,
	}, nil
}
