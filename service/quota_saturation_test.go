package service

import (
	"math"
	"net/http"
	"testing"

	"eflow.local/new-api/common"
	"eflow.local/new-api/dto"
	relaycommon "eflow.local/new-api/relay/common"
	"eflow.local/new-api/types"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestPreConsumeBillingRejectsSaturatedQuotaBeforeDeduction(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(nil)
	info := &relaycommon.RelayInfo{
		QuotaClamp: &common.QuotaClamp{
			Op:       "QuotaFromFloat",
			Kind:     common.QuotaClampOverflow,
			Original: 1e30,
			Clamped:  common.MaxQuota,
		},
	}

	apiErr := PreConsumeBilling(c, common.MaxQuota, info)

	require.NotNil(t, apiErr)
	require.Equal(t, types.ErrorCodeModelPriceError, apiErr.GetErrorCode())
	require.Equal(t, http.StatusBadRequest, apiErr.StatusCode)
	require.Same(t, info.QuotaClamp, apiErr.Err)
	var clamp *common.QuotaClamp
	require.ErrorAs(t, apiErr, &clamp)
	require.Same(t, info.QuotaClamp, clamp)
	require.Nil(t, info.Billing)
}

func TestPreConsumeBillingRejectsNegativeQuotaBeforeDeduction(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(nil)
	info := &relaycommon.RelayInfo{}

	apiErr := PreConsumeBilling(c, -1, info)

	require.NotNil(t, apiErr)
	require.Equal(t, types.ErrorCodeModelPriceError, apiErr.GetErrorCode())
	require.Equal(t, http.StatusBadRequest, apiErr.StatusCode)
	require.Nil(t, info.Billing)
}

func TestCalcViolationFeeQuotaSaturates(t *testing.T) {
	previousQuotaPerUnit := common.GetQuotaPerUnit()
	if err := common.SetQuotaPerUnit(500_000); err != nil {
		t.Fatalf("set quota per unit: %v", err)
	}
	t.Cleanup(func() {
		if err := common.SetQuotaPerUnit(previousQuotaPerUnit); err != nil {
			t.Fatalf("restore quota per unit: %v", err)
		}
	})

	require.Equal(t, common.MaxQuota, calcViolationFeeQuota(1e20, 1))
}

func TestCalcOpenRouterCacheCreateTokensDoesNotWrap(t *testing.T) {
	previousQuotaPerUnit := common.GetQuotaPerUnit()
	if err := common.SetQuotaPerUnit(500_000); err != nil {
		t.Fatalf("set quota per unit: %v", err)
	}
	t.Cleanup(func() {
		if err := common.SetQuotaPerUnit(previousQuotaPerUnit); err != nil {
			t.Fatalf("restore quota per unit: %v", err)
		}
	})

	got := CalcOpenRouterCacheCreateTokens(dto.Usage{Cost: math.Inf(1)}, types.PriceData{
		ModelRatio:         1,
		CacheCreationRatio: 2,
		CacheRatio:         1,
		CompletionRatio:    1,
	})
	require.Equal(t, -1, got)
}
