package controller

import (
	"strconv"
	"testing"

	"eflow.local/new-api/common"
	"eflow.local/new-api/internal/testconfig"
	"eflow.local/new-api/model"
	"eflow.local/new-api/setting"
	"eflow.local/new-api/setting/operation_setting"
	"github.com/stretchr/testify/require"
)

func TestFormatWaffoPancakeAmount_UsesDisplayPriceString(t *testing.T) {
	testCases := []struct {
		name     string
		amount   float64
		expected string
	}{
		{name: "whole amount", amount: 29, expected: "29.00"},
		{name: "decimal amount", amount: 29.9, expected: "29.90"},
		{name: "round half up to cents", amount: 29.999, expected: "30.00"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.expected, formatWaffoPancakeAmount(tc.amount))
		})
	}
}

func TestGetWaffoPancakePayMoney(t *testing.T) {
	originalConfig := setting.GetWaffoPancakeConfig()
	originalTopupGroupRatio := common.TopupGroupRatio2JSONString()
	quotaPerUnit := common.GetQuotaPerUnit()

	t.Cleanup(func() {
		require.NoError(t, setting.UpdateWaffoPancakeConfig(map[string]string{
			"WaffoPancakeUnitPrice": strconv.FormatFloat(originalConfig.UnitPrice, 'f', -1, 64),
		}))
		require.NoError(t, common.UpdateTopupGroupRatioByJSONString(originalTopupGroupRatio))
	})

	require.NoError(t, setting.UpdateWaffoPancakeConfig(map[string]string{
		"WaffoPancakeUnitPrice": "2.5",
	}))
	testconfig.Set(t, "payment_setting", map[string]string{
		"amount_discount": testconfig.JSON(t, map[int]float64{
			10:                    0.8,
			int(quotaPerUnit * 3): 0.5,
			20:                    0,
		}),
	})
	testconfig.Set(t, "general_setting", map[string]string{
		"quota_display_type": operation_setting.QuotaDisplayTypeUSD,
	})
	require.NoError(t, common.UpdateTopupGroupRatioByJSONString(`{"default":1,"vip":1.2}`))

	testCases := []struct {
		name             string
		amount           int64
		group            string
		quotaDisplayType string
		expected         float64
	}{
		{
			name:             "currency display applies unit price group ratio and discount",
			amount:           10,
			group:            "vip",
			quotaDisplayType: operation_setting.QuotaDisplayTypeUSD,
			expected:         24,
		},
		{
			name:             "tokens display converts quota to display units before pricing",
			amount:           int64(quotaPerUnit * 3),
			group:            "vip",
			quotaDisplayType: operation_setting.QuotaDisplayTypeTokens,
			expected:         4.5,
		},
		{
			name:             "non-positive discount falls back to no discount",
			amount:           20,
			group:            "default",
			quotaDisplayType: operation_setting.QuotaDisplayTypeUSD,
			expected:         50,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			testconfig.Update(t, "general_setting", map[string]string{
				"quota_display_type": tc.quotaDisplayType,
			})
			generalConfig := operation_setting.GetGeneralSetting()
			paymentConfig := operation_setting.GetPaymentSetting()
			actual, err := getWaffoPancakePayMoney(
				tc.amount,
				tc.group,
				2.5,
				generalConfig.QuotaDisplayType,
				paymentConfig.AmountDiscount,
				quotaPerUnit,
			)
			require.NoError(t, err)
			require.InDelta(t, tc.expected, actual, 0.000001)
		})
	}
}

func TestWaffoPancakePaymentArtifactsUseCapturedVersions(t *testing.T) {
	originalConfig := setting.GetWaffoPancakeConfig()
	t.Cleanup(func() {
		require.NoError(t, setting.UpdateWaffoPancakeConfig(waffoPancakeConfigValues(originalConfig)))
	})

	testconfig.Set(t, "general_setting", map[string]string{
		"quota_display_type": operation_setting.QuotaDisplayTypeTokens,
	})
	testconfig.Set(t, "payment_setting", map[string]string{
		"amount_discount":          testconfig.JSON(t, map[int]float64{300: 0.5}),
		"compliance_confirmed":     "true",
		"compliance_terms_version": operation_setting.CurrentComplianceTermsVersion,
	})
	require.NoError(t, setting.UpdateWaffoPancakeConfig(map[string]string{
		"WaffoPancakeMerchantID": "merchant-a",
		"WaffoPancakePrivateKey": "private-a",
		"WaffoPancakeReturnURL":  "https://a.example.com/return",
		"WaffoPancakeUnitPrice":  "2.5",
		"WaffoPancakeMinTopUp":   "2",
		"WaffoPancakeStoreID":    "store-a",
		"WaffoPancakeProductID":  "product-a",
	}))

	runtimeCaptured := setting.GetWaffoPancakeConfig()
	generalCaptured := operation_setting.GetGeneralSetting()
	paymentCaptured := operation_setting.GetPaymentSetting()
	quotaPerUnitCaptured := 100.0

	testconfig.Update(t, "general_setting", map[string]string{
		"quota_display_type": operation_setting.QuotaDisplayTypeUSD,
	})
	testconfig.Update(t, "payment_setting", map[string]string{
		"amount_discount":      `{}`,
		"compliance_confirmed": "false",
	})
	require.NoError(t, setting.UpdateWaffoPancakeConfig(map[string]string{
		"WaffoPancakeMerchantID": "merchant-b",
		"WaffoPancakePrivateKey": "private-b",
		"WaffoPancakeUnitPrice":  "9",
		"WaffoPancakeMinTopUp":   "9",
		"WaffoPancakeProductID":  "product-b",
	}))

	minTopUp, err := getWaffoPancakeMinTopUp(
		runtimeCaptured.MinTopUp, generalCaptured.QuotaDisplayType, quotaPerUnitCaptured,
	)
	require.NoError(t, err)
	require.Equal(t, int64(200), minTopUp)

	creditedQuota, err := validateWaffoPancakeTopUpQuota(300, generalCaptured.QuotaDisplayType, quotaPerUnitCaptured)
	require.NoError(t, err)
	require.Equal(t, 300, creditedQuota)

	payMoney, err := getWaffoPancakePayMoney(
		300,
		"",
		runtimeCaptured.UnitPrice,
		generalCaptured.QuotaDisplayType,
		paymentCaptured.AmountDiscount,
		quotaPerUnitCaptured,
	)
	require.NoError(t, err)
	require.InDelta(t, 3.75, payMoney, 0.000001)

	normalizedAmount, err := normalizeWaffoPancakeTopUpAmount(
		300, generalCaptured.QuotaDisplayType, quotaPerUnitCaptured,
	)
	require.NoError(t, err)
	require.Equal(t, int64(3), normalizedAmount)

	user := &model.User{Id: 7, Email: "buyer@example.com"}
	topUp, checkoutParams := buildWaffoPancakePaymentArtifacts(
		runtimeCaptured, user, normalizedAmount, payMoney, "trade-a", 123,
	)
	require.Equal(t, int64(3), topUp.Amount)
	require.InDelta(t, 3.75, topUp.Money, 0.000001)
	require.Equal(t, "trade-a", topUp.TradeNo)
	require.Equal(t, int64(123), topUp.CreateTime)
	require.Equal(t, "product-a", checkoutParams.ProductID)
	require.Equal(t, "3.75", checkoutParams.PriceSnapshot.Amount)
	require.Equal(t, "trade-a", checkoutParams.OrderMerchantExternalID)
	require.Equal(t, runtimeCaptured, checkoutParams.RuntimeConfig)
	require.Equal(t, 45*60, *checkoutParams.ExpiresInSeconds)
	require.True(t, isWaffoPancakeEnabledWithSnapshots(runtimeCaptured, paymentCaptured))
}

func waffoPancakeConfigValues(config setting.WaffoPancakeConfig) map[string]string {
	return map[string]string{
		"WaffoPancakeMerchantID": config.MerchantID,
		"WaffoPancakePrivateKey": config.PrivateKey,
		"WaffoPancakeReturnURL":  config.ReturnURL,
		"WaffoPancakeUnitPrice":  strconv.FormatFloat(config.UnitPrice, 'f', -1, 64),
		"WaffoPancakeMinTopUp":   strconv.Itoa(config.MinTopUp),
		"WaffoPancakeStoreID":    config.StoreID,
		"WaffoPancakeProductID":  config.ProductID,
	}
}
