package controller

import (
	"strconv"
	"testing"

	"eflow.local/new-api/internal/testconfig"
	"eflow.local/new-api/setting"
	"eflow.local/new-api/setting/operation_setting"
	"github.com/stretchr/testify/require"
)

func confirmPaymentComplianceForTest(t *testing.T) {
	t.Helper()
	testconfig.Set(t, "payment_setting", map[string]string{
		"compliance_confirmed":     "true",
		"compliance_terms_version": operation_setting.CurrentComplianceTermsVersion,
	})
}

func TestStripeWebhookEnabledRequiresTopUpAndWebhookConfig(t *testing.T) {
	confirmPaymentComplianceForTest(t)
	original := setting.GetStripeConfig()
	t.Cleanup(func() {
		require.NoError(t, setting.UpdateStripeConfig(map[string]string{
			"StripeApiSecret":             original.APISecret,
			"StripeWebhookSecret":         original.WebhookSecret,
			"StripePriceId":               original.PriceID,
			"StripeUnitPrice":             formatFloat(original.UnitPrice),
			"StripeMinTopUp":              formatInt(original.MinTopUp),
			"StripePromotionCodesEnabled": formatBool(original.PromotionCodesEnabled),
		}))
	})

	require.NoError(t, setting.UpdateStripeConfig(map[string]string{
		"StripeWebhookSecret": "",
		"StripeApiSecret":     "sk_test_123",
		"StripePriceId":       "price_123",
	}))
	require.False(t, isStripeWebhookEnabled())

	require.NoError(t, setting.UpdateStripeConfig(map[string]string{"StripeWebhookSecret": "whsec_test"}))
	require.True(t, isStripeWebhookEnabled())

	require.NoError(t, setting.UpdateStripeConfig(map[string]string{"StripePriceId": ""}))
	require.False(t, isStripeWebhookEnabled())
}

func TestCreemWebhookEnabledRequiresTopUpAndWebhookConfig(t *testing.T) {
	confirmPaymentComplianceForTest(t)
	original := setting.GetCreemConfig()
	t.Cleanup(func() {
		require.NoError(t, setting.UpdateCreemConfig(map[string]string{
			"CreemApiKey":        original.APIKey,
			"CreemProducts":      original.ProductsJSON,
			"CreemTestMode":      formatBool(original.TestMode),
			"CreemWebhookSecret": original.WebhookSecret,
		}))
	})

	require.NoError(t, setting.UpdateCreemConfig(map[string]string{
		"CreemWebhookSecret": "",
		"CreemApiKey":        "creem_api_key",
		"CreemProducts":      `[{"productId":"prod_123"}]`,
	}))
	require.False(t, isCreemWebhookEnabled())

	require.NoError(t, setting.UpdateCreemConfig(map[string]string{"CreemWebhookSecret": "creem_secret"}))
	require.True(t, isCreemWebhookEnabled())

	require.NoError(t, setting.UpdateCreemConfig(map[string]string{"CreemProducts": "[]"}))
	require.False(t, isCreemWebhookEnabled())
}

func TestWaffoWebhookEnabledRequiresTopUpAndWebhookConfig(t *testing.T) {
	confirmPaymentComplianceForTest(t)
	original := setting.GetWaffoConfig()
	t.Cleanup(func() {
		require.NoError(t, setting.UpdateWaffoConfig(waffoOptionValues(t, original)))
	})

	require.NoError(t, setting.UpdateWaffoConfig(map[string]string{
		"WaffoEnabled":    "true",
		"WaffoSandbox":    "false",
		"WaffoApiKey":     "",
		"WaffoPrivateKey": "private",
		"WaffoPublicCert": "public",
	}))
	require.False(t, isWaffoWebhookEnabled())

	require.NoError(t, setting.UpdateWaffoConfig(map[string]string{"WaffoApiKey": "api"}))
	require.True(t, isWaffoWebhookEnabled())

	require.NoError(t, setting.UpdateWaffoConfig(map[string]string{"WaffoEnabled": "false"}))
	require.False(t, isWaffoWebhookEnabled())

	require.NoError(t, setting.UpdateWaffoConfig(map[string]string{
		"WaffoEnabled":           "true",
		"WaffoSandbox":           "true",
		"WaffoSandboxApiKey":     "",
		"WaffoSandboxPrivateKey": "sandbox_private",
		"WaffoSandboxPublicCert": "sandbox_public",
	}))
	require.False(t, isWaffoWebhookEnabled())

	require.NoError(t, setting.UpdateWaffoConfig(map[string]string{"WaffoSandboxApiKey": "sandbox_api"}))
	require.True(t, isWaffoWebhookEnabled())
}

func TestWaffoPancakeWebhookEnabledRequiresTopUpAndWebhookConfig(t *testing.T) {
	confirmPaymentComplianceForTest(t)
	originalConfig := setting.GetWaffoPancakeConfig()
	t.Cleanup(func() {
		require.NoError(t, setting.UpdateWaffoPancakeConfig(map[string]string{
			"WaffoPancakeMerchantID": originalConfig.MerchantID,
			"WaffoPancakePrivateKey": originalConfig.PrivateKey,
			"WaffoPancakeProductID":  originalConfig.ProductID,
		}))
	})

	// Presence of all three credentials enables the gateway. Webhook public
	// keys are bundled in the SDK and there is no separate Enabled toggle —
	// clear any of the three fields to disable.
	require.NoError(t, setting.UpdateWaffoPancakeConfig(map[string]string{
		"WaffoPancakeMerchantID": "",
		"WaffoPancakePrivateKey": "private",
		"WaffoPancakeProductID":  "product",
	}))
	require.False(t, isWaffoPancakeWebhookEnabled())

	require.NoError(t, setting.UpdateWaffoPancakeConfig(map[string]string{"WaffoPancakeMerchantID": "merchant"}))
	require.True(t, isWaffoPancakeWebhookEnabled())

	require.NoError(t, setting.UpdateWaffoPancakeConfig(map[string]string{"WaffoPancakeProductID": ""}))
	require.False(t, isWaffoPancakeWebhookEnabled())

	require.NoError(t, setting.UpdateWaffoPancakeConfig(map[string]string{
		"WaffoPancakeProductID":  "product",
		"WaffoPancakePrivateKey": "",
	}))
	require.False(t, isWaffoPancakeWebhookEnabled())
}

func TestEpayWebhookEnabledRequiresTopUpAndWebhookConfig(t *testing.T) {
	confirmPaymentComplianceForTest(t)
	original := operation_setting.GetEpayConfig()
	t.Cleanup(func() {
		require.NoError(t, operation_setting.UpdateEpayConfig(epayOptionValues(t, original)))
	})

	require.NoError(t, operation_setting.UpdateEpayConfig(map[string]string{
		"PayAddress": "https://pay.example.com",
		"EpayId":     "epay_id",
		"EpayKey":    "",
		"PayMethods": `[{"type":"alipay"}]`,
	}))
	require.False(t, isEpayWebhookEnabled())

	require.NoError(t, operation_setting.UpdateEpayConfig(map[string]string{"EpayKey": "epay_key"}))
	require.True(t, isEpayWebhookEnabled())

	require.NoError(t, operation_setting.UpdateEpayConfig(map[string]string{"PayMethods": "[]"}))
	require.False(t, isEpayWebhookEnabled())
}

func formatFloat(value float64) string {
	return strconv.FormatFloat(value, 'f', -1, 64)
}

func formatInt(value int) string {
	return strconv.Itoa(value)
}

func formatBool(value bool) string {
	return strconv.FormatBool(value)
}

func waffoOptionValues(t *testing.T, config setting.WaffoConfig) map[string]string {
	t.Helper()
	return map[string]string{
		"WaffoEnabled":           formatBool(config.Enabled),
		"WaffoApiKey":            config.APIKey,
		"WaffoPrivateKey":        config.PrivateKey,
		"WaffoPublicCert":        config.PublicCert,
		"WaffoSandboxPublicCert": config.SandboxPublicCert,
		"WaffoSandboxApiKey":     config.SandboxAPIKey,
		"WaffoSandboxPrivateKey": config.SandboxPrivateKey,
		"WaffoSandbox":           formatBool(config.Sandbox),
		"WaffoMerchantId":        config.MerchantID,
		"WaffoNotifyUrl":         config.NotifyURL,
		"WaffoReturnUrl":         config.ReturnURL,
		"WaffoCurrency":          config.Currency,
		"WaffoUnitPrice":         formatFloat(config.UnitPrice),
		"WaffoMinTopUp":          formatInt(config.MinTopUp),
		"WaffoPayMethods":        testconfig.JSON(t, config.PayMethods),
	}
}

func epayOptionValues(t *testing.T, config operation_setting.EpayConfig) map[string]string {
	t.Helper()
	return map[string]string{
		"PayAddress":            config.PayAddress,
		"CustomCallbackAddress": config.CustomCallbackAddress,
		"EpayId":                config.EpayID,
		"EpayKey":               config.EpayKey,
		"Price":                 formatFloat(config.Price),
		"MinTopUp":              formatInt(config.MinTopUp),
		"USDExchangeRate":       formatFloat(config.USDExchangeRate),
		"PayMethods":            testconfig.JSON(t, config.PayMethods),
	}
}
