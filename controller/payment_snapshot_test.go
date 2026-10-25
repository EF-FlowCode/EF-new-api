package controller

import (
	"context"
	"io"
	"testing"

	"eflow.local/new-api/common"
	"eflow.local/new-api/setting"
	"eflow.local/new-api/setting/operation_setting"
	"github.com/stretchr/testify/require"
	waffoconfig "github.com/waffo-com/waffo-go/config"
)

func TestPaymentBuildersUseCapturedProviderVersions(t *testing.T) {
	originalStripe := setting.GetStripeConfig()
	originalCreem := setting.GetCreemConfig()
	originalWaffo := setting.GetWaffoConfig()
	originalEpay := operation_setting.GetEpayConfig()
	originalQuotaPerUnit := common.GetQuotaPerUnit()
	t.Cleanup(func() {
		require.NoError(t, setting.UpdateStripeConfig(map[string]string{
			"StripeApiSecret": originalStripe.APISecret, "StripeWebhookSecret": originalStripe.WebhookSecret,
			"StripePriceId": originalStripe.PriceID, "StripeUnitPrice": formatFloat(originalStripe.UnitPrice),
			"StripeMinTopUp":              formatInt(originalStripe.MinTopUp),
			"StripePromotionCodesEnabled": formatBool(originalStripe.PromotionCodesEnabled),
		}))
		require.NoError(t, setting.UpdateCreemConfig(map[string]string{
			"CreemApiKey": originalCreem.APIKey, "CreemProducts": originalCreem.ProductsJSON,
			"CreemTestMode": formatBool(originalCreem.TestMode), "CreemWebhookSecret": originalCreem.WebhookSecret,
		}))
		require.NoError(t, setting.UpdateWaffoConfig(waffoOptionValues(t, originalWaffo)))
		require.NoError(t, operation_setting.UpdateEpayConfig(epayOptionValues(t, originalEpay)))
		require.NoError(t, common.SetQuotaPerUnit(originalQuotaPerUnit))
	})

	require.NoError(t, setting.UpdateStripeConfig(map[string]string{
		"StripeApiSecret": "sk_captured", "StripePriceId": "price_captured",
		"StripePromotionCodesEnabled": "true", "StripeUnitPrice": "10", "StripeMinTopUp": "2",
	}))
	require.NoError(t, setting.UpdateCreemConfig(map[string]string{
		"CreemApiKey": "creem-captured", "CreemTestMode": "true",
		"CreemProducts": `[{"productId":"product-captured","name":"Captured","price":3,"quota":30}]`,
	}))
	require.NoError(t, setting.UpdateWaffoConfig(map[string]string{
		"WaffoSandbox": "false", "WaffoApiKey": "waffo-captured", "WaffoPrivateKey": "private-captured",
		"WaffoPublicCert": "public-captured", "WaffoMerchantId": "merchant-captured",
		"WaffoUnitPrice": "10", "WaffoMinTopUp": "2",
	}))
	require.NoError(t, operation_setting.UpdateEpayConfig(map[string]string{
		"PayAddress": "https://pay-captured.example.com", "EpayId": "epay-captured", "EpayKey": "key-captured",
		"Price": "10", "MinTopUp": "2",
	}))

	stripeCaptured := setting.GetStripeConfig()
	creemCaptured := setting.GetCreemConfig()
	waffoCaptured := setting.GetWaffoConfig()
	epayCaptured := operation_setting.GetEpayConfig()

	require.NoError(t, setting.UpdateStripeConfig(map[string]string{
		"StripeApiSecret": "sk_new", "StripePriceId": "price_new", "StripePromotionCodesEnabled": "false",
		"StripeUnitPrice": "20", "StripeMinTopUp": "3",
	}))
	require.NoError(t, setting.UpdateCreemConfig(map[string]string{
		"CreemApiKey": "creem-new", "CreemTestMode": "false",
		"CreemProducts": `[{"productId":"product-new","name":"New","price":4,"quota":40}]`,
	}))
	require.NoError(t, setting.UpdateWaffoConfig(map[string]string{
		"WaffoSandbox": "true", "WaffoSandboxApiKey": "waffo-new",
		"WaffoSandboxPrivateKey": "private-new", "WaffoSandboxPublicCert": "public-new",
		"WaffoMerchantId": "merchant-new", "WaffoUnitPrice": "20", "WaffoMinTopUp": "3",
	}))
	require.NoError(t, operation_setting.UpdateEpayConfig(map[string]string{
		"PayAddress": "https://pay-new.example.com", "EpayId": "epay-new", "EpayKey": "key-new",
		"Price": "20", "MinTopUp": "3",
	}))

	apiSecret, stripeRequest, err := buildStripeCheckoutRequest(
		stripeCaptured, "reference", "", "buyer@example.com", 2,
		"https://app.example.com/success", "https://app.example.com/cancel",
	)
	require.NoError(t, err)
	require.Equal(t, "sk_captured", apiSecret)
	require.Equal(t, "price_captured", *stripeRequest.LineItems[0].Price)
	require.True(t, *stripeRequest.AllowPromotionCodes)

	creemRequest, err := buildCreemCheckoutRequest(
		context.Background(), creemCaptured, "reference", &creemCaptured.Products[0], "buyer@example.com", "buyer",
	)
	require.NoError(t, err)
	require.Equal(t, "https://test-api.creem.io/v1/checkouts", creemRequest.URL.String())
	require.Equal(t, "creem-captured", creemRequest.Header.Get("x-api-key"))
	creemBody, err := io.ReadAll(creemRequest.Body)
	require.NoError(t, err)
	require.NoError(t, creemRequest.Body.Close())
	var creemPayload CreemCheckoutRequest
	require.NoError(t, common.Unmarshal(creemBody, &creemPayload))
	require.Equal(t, "product-captured", creemPayload.ProductId)

	waffoOptions := resolveWaffoSDKOptions(waffoCaptured)
	require.Equal(t, waffoconfig.Production, waffoOptions.Environment)
	require.Equal(t, "waffo-captured", waffoOptions.APIKey)
	require.Equal(t, "private-captured", waffoOptions.PrivateKey)
	require.Equal(t, "public-captured", waffoOptions.PublicKey)
	require.Equal(t, "merchant-captured", waffoOptions.MerchantID)

	require.NoError(t, common.SetQuotaPerUnit(100))
	generalCaptured := &operation_setting.GeneralSetting{QuotaDisplayType: operation_setting.QuotaDisplayTypeTokens}
	quotaPerUnit := common.GetQuotaPerUnit()
	paymentCaptured := &operation_setting.PaymentSetting{
		AmountDiscount: map[int]float64{int(quotaPerUnit): 0.5},
	}
	amount := int64(quotaPerUnit)
	require.NoError(t, common.SetQuotaPerUnit(250))
	require.Equal(t, 250.0, common.GetQuotaPerUnit())
	// Stripe charges its fixed PriceID; amount discounts do not apply.
	require.InDelta(t, 10, getStripePayMoney(stripeCaptured, generalCaptured, float64(amount), quotaPerUnit), 1e-9)
	require.InDelta(t, 5, getWaffoPayMoney(waffoCaptured, generalCaptured, paymentCaptured, float64(amount), "", quotaPerUnit), 1e-9)
	require.InDelta(t, 5, getPayMoney(epayCaptured, generalCaptured, paymentCaptured, amount, "", quotaPerUnit), 1e-9)
	require.Equal(t, int64(2*quotaPerUnit), getStripeMinTopup(stripeCaptured, generalCaptured.QuotaDisplayType, quotaPerUnit))
	require.Equal(t, int64(2*quotaPerUnit), getMinTopup(epayCaptured, generalCaptured.QuotaDisplayType, quotaPerUnit))

	epayClient := GetEpayClientWithConfig(epayCaptured)
	require.NotNil(t, epayClient)
	require.Equal(t, "epay-captured", epayClient.Config.PartnerID)
	require.Equal(t, "key-captured", epayClient.Config.Key)
	require.Equal(t, "https://pay-captured.example.com", epayClient.BaseUrl.String())
}
