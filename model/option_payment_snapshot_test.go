package model

import (
	"maps"
	"reflect"
	"strconv"
	"testing"

	"eflow.local/new-api/common"
	"eflow.local/new-api/setting"
	"eflow.local/new-api/setting/operation_setting"
	"github.com/stretchr/testify/require"
)

type paymentSnapshots struct {
	stripe setting.StripeConfig
	creem  setting.CreemConfig
	waffo  setting.WaffoConfig
	epay   operation_setting.EpayConfig
}

func TestValidatePaymentOptionValuesRejectsEveryDomain(t *testing.T) {
	tests := []struct {
		name   string
		values map[string]string
	}{
		{name: "Stripe", values: map[string]string{"StripeUnitPrice": "NaN"}},
		{name: "Creem", values: map[string]string{"CreemProducts": `[{}]`}},
		{name: "Waffo", values: map[string]string{"WaffoPayMethods": `[null]`}},
		{name: "Epay", values: map[string]string{"PayMethods": `[null]`}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Error(t, validatePaymentOptionValues(tt.values))
		})
	}
}

func TestUpdateOptionMapSkipsPaymentSnapshotKeys(t *testing.T) {
	preservePaymentSnapshots(t)
	preserveOptionMap(t)
	values := paymentTestOptionValues("baseline")
	require.NoError(t, publishOptionValues(values))
	before := capturePaymentSnapshots()

	for _, key := range []string{"StripeApiSecret", "CreemApiKey", "WaffoApiKey", "PayAddress"} {
		require.NoError(t, updateOptionMap(key, "must-not-publish"))
	}

	require.True(t, equalPaymentSnapshots(before, capturePaymentSnapshots()))
	common.OptionMapRWMutex.RLock()
	defer common.OptionMapRWMutex.RUnlock()
	for _, key := range []string{"StripeApiSecret", "CreemApiKey", "WaffoApiKey", "PayAddress"} {
		require.Equal(t, values[key], common.OptionMap[key], key)
	}
}

func TestUpdateOptionsBulkPublishesPaymentSnapshotsAfterCommit(t *testing.T) {
	db := useModelIntegrationTestDB(t, &Option{})
	preservePaymentSnapshots(t)
	preserveOptionMap(t)

	values := paymentTestOptionValues("published")
	require.NoError(t, UpdateOptionsBulk(values))

	stripe := setting.GetStripeConfig()
	creem := setting.GetCreemConfig()
	waffo := setting.GetWaffoConfig()
	epay := operation_setting.GetEpayConfig()
	require.Equal(t, "stripe-published", stripe.APISecret)
	require.Equal(t, 12.5, stripe.UnitPrice)
	require.Equal(t, "creem-published", creem.APIKey)
	require.Equal(t, "creem-product-published", creem.Products[0].ProductID)
	require.Equal(t, "waffo-published", waffo.APIKey)
	require.Equal(t, "WAFFO-published", waffo.PayMethods[0].PayMethodType)
	require.Equal(t, "https://epay-published.example.com", epay.PayAddress)
	require.Equal(t, "epay-published", epay.PayMethods[0]["type"])

	for key, want := range values {
		var persisted Option
		require.NoError(t, db.Where("`key` = ?", key).First(&persisted).Error)
		require.Equal(t, want, persisted.Value, key)
		common.OptionMapRWMutex.RLock()
		published := common.OptionMap[key]
		common.OptionMapRWMutex.RUnlock()
		require.Equal(t, want, published, key)
	}
}

func TestUpdateOptionsBulkRejectsInvalidPaymentDomainBeforeCommit(t *testing.T) {
	tests := []struct {
		name    string
		invalid map[string]string
	}{
		{name: "Stripe", invalid: map[string]string{"StripeUnitPrice": "NaN"}},
		{name: "Creem", invalid: map[string]string{"CreemProducts": `[{}]`}},
		{name: "Waffo", invalid: map[string]string{"WaffoPayMethods": `[null]`}},
		{name: "Epay", invalid: map[string]string{"PayMethods": `[null]`}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := useModelIntegrationTestDB(t, &Option{})
			preservePaymentSnapshots(t)
			preserveOptionMap(t)

			baselineValues := paymentTestOptionValues("baseline")
			require.NoError(t, UpdateOptionsBulk(baselineValues))
			before := capturePaymentSnapshots()

			updates := paymentTestOptionValues("after")
			maps.Copy(updates, tt.invalid)
			require.Error(t, UpdateOptionsBulk(updates))
			require.True(t, equalPaymentSnapshots(before, capturePaymentSnapshots()))

			for key, want := range baselineValues {
				var persisted Option
				require.NoError(t, db.Where("`key` = ?", key).First(&persisted).Error)
				require.Equal(t, want, persisted.Value, key)
				common.OptionMapRWMutex.RLock()
				published := common.OptionMap[key]
				common.OptionMapRWMutex.RUnlock()
				require.Equal(t, want, published, key)
			}
		})
	}
}

func TestLoadOptionsFromDatabaseValidatesAllPaymentDomainsBeforePublish(t *testing.T) {
	db := useModelIntegrationTestDB(t, &Option{})
	preservePaymentSnapshots(t)
	preserveOptionMap(t)

	baselineValues := paymentTestOptionValues("baseline")
	require.NoError(t, publishOptionValues(baselineValues))
	before := capturePaymentSnapshots()

	require.NoError(t, db.Create([]Option{
		{Key: "StripeApiSecret", Value: "stripe-after"},
		{Key: "CreemProducts", Value: `[{}]`},
	}).Error)
	require.Error(t, loadOptionsFromDatabase())
	require.True(t, equalPaymentSnapshots(before, capturePaymentSnapshots()))

	common.OptionMapRWMutex.RLock()
	defer common.OptionMapRWMutex.RUnlock()
	require.Equal(t, baselineValues["StripeApiSecret"], common.OptionMap["StripeApiSecret"])
	require.Equal(t, baselineValues["CreemProducts"], common.OptionMap["CreemProducts"])
}

func TestInitOptionMapUsesPaymentSnapshots(t *testing.T) {
	useModelIntegrationTestDB(t, &Option{})
	preservePaymentSnapshots(t)
	preserveOptionMap(t)

	values := paymentTestOptionValues("snapshot")
	require.NoError(t, setting.UpdateStripeConfig(values))
	require.NoError(t, setting.UpdateCreemConfig(values))
	require.NoError(t, setting.UpdateWaffoConfig(values))
	require.NoError(t, operation_setting.UpdateEpayConfig(values))
	waffoMethods, err := common.Marshal(setting.GetWaffoConfig().PayMethods)
	require.NoError(t, err)

	InitOptionMap()

	common.OptionMapRWMutex.RLock()
	defer common.OptionMapRWMutex.RUnlock()
	require.Equal(t, values["StripeApiSecret"], common.OptionMap["StripeApiSecret"])
	require.Equal(t, values["CreemApiKey"], common.OptionMap["CreemApiKey"])
	require.Equal(t, values["WaffoApiKey"], common.OptionMap["WaffoApiKey"])
	require.Equal(t, values["PayAddress"], common.OptionMap["PayAddress"])
	require.JSONEq(t, string(waffoMethods), common.OptionMap["WaffoPayMethods"])
	require.JSONEq(t, values["PayMethods"], common.OptionMap["PayMethods"])
}

func preservePaymentSnapshots(t *testing.T) {
	t.Helper()
	original := capturePaymentSnapshots()
	t.Cleanup(func() {
		require.NoError(t, setting.UpdateStripeConfig(stripeSnapshotValues(original.stripe)))
		require.NoError(t, setting.UpdateCreemConfig(creemSnapshotValues(original.creem)))
		require.NoError(t, setting.UpdateWaffoConfig(waffoSnapshotValues(t, original.waffo)))
		require.NoError(t, operation_setting.UpdateEpayConfig(epaySnapshotValues(t, original.epay)))
	})
}

func preserveOptionMap(t *testing.T) {
	t.Helper()
	common.OptionMapRWMutex.Lock()
	original := common.OptionMap
	common.OptionMap = make(map[string]string)
	common.OptionMapRWMutex.Unlock()
	t.Cleanup(func() {
		common.OptionMapRWMutex.Lock()
		common.OptionMap = original
		common.OptionMapRWMutex.Unlock()
	})
}

func capturePaymentSnapshots() paymentSnapshots {
	return paymentSnapshots{
		stripe: setting.GetStripeConfig(),
		creem:  setting.GetCreemConfig(),
		waffo:  setting.GetWaffoConfig(),
		epay:   operation_setting.GetEpayConfig(),
	}
}

func equalPaymentSnapshots(left, right paymentSnapshots) bool {
	return left.stripe == right.stripe &&
		reflect.DeepEqual(left.creem, right.creem) &&
		reflect.DeepEqual(left.waffo, right.waffo) &&
		reflect.DeepEqual(left.epay, right.epay)
}

func paymentTestOptionValues(version string) map[string]string {
	return map[string]string{
		"StripeApiSecret":             "stripe-" + version,
		"StripeWebhookSecret":         "stripe-webhook-" + version,
		"StripePriceId":               "stripe-price-" + version,
		"StripeUnitPrice":             "12.5",
		"StripeMinTopUp":              "2",
		"StripePromotionCodesEnabled": "true",
		"CreemApiKey":                 "creem-" + version,
		"CreemProducts":               `[{"productId":"creem-product-` + version + `","price":2,"currency":"USD","quota":20}]`,
		"CreemTestMode":               "true",
		"CreemWebhookSecret":          "creem-webhook-" + version,
		"WaffoEnabled":                "true",
		"WaffoApiKey":                 "waffo-" + version,
		"WaffoPrivateKey":             "waffo-private-" + version,
		"WaffoPublicCert":             "waffo-cert-" + version,
		"WaffoSandboxPublicCert":      "waffo-sandbox-cert-" + version,
		"WaffoSandboxApiKey":          "waffo-sandbox-" + version,
		"WaffoSandboxPrivateKey":      "waffo-sandbox-private-" + version,
		"WaffoSandbox":                "false",
		"WaffoMerchantId":             "waffo-merchant-" + version,
		"WaffoNotifyUrl":              "https://waffo-" + version + ".example.com/notify",
		"WaffoReturnUrl":              "https://waffo-" + version + ".example.com/return",
		"WaffoCurrency":               "USD",
		"WaffoUnitPrice":              "3.5",
		"WaffoMinTopUp":               "3",
		"WaffoPayMethods":             `[{"name":"Waffo","payMethodType":"WAFFO-` + version + `"}]`,
		"PayAddress":                  "https://epay-" + version + ".example.com",
		"CustomCallbackAddress":       "https://epay-" + version + ".example.com/callback",
		"EpayId":                      "epay-id-" + version,
		"EpayKey":                     "epay-key-" + version,
		"Price":                       "7.5",
		"MinTopUp":                    "4",
		"USDExchangeRate":             "7.2",
		"PayMethods":                  `[{"name":"Epay","type":"epay-` + version + `"}]`,
	}
}

func stripeSnapshotValues(config setting.StripeConfig) map[string]string {
	return map[string]string{
		"StripeApiSecret": config.APISecret, "StripeWebhookSecret": config.WebhookSecret,
		"StripePriceId": config.PriceID, "StripeUnitPrice": strconv.FormatFloat(config.UnitPrice, 'f', -1, 64),
		"StripeMinTopUp": strconv.Itoa(config.MinTopUp), "StripePromotionCodesEnabled": strconv.FormatBool(config.PromotionCodesEnabled),
	}
}

func creemSnapshotValues(config setting.CreemConfig) map[string]string {
	return map[string]string{
		"CreemApiKey": config.APIKey, "CreemProducts": config.ProductsJSON,
		"CreemTestMode": strconv.FormatBool(config.TestMode), "CreemWebhookSecret": config.WebhookSecret,
	}
}

func waffoSnapshotValues(t *testing.T, config setting.WaffoConfig) map[string]string {
	t.Helper()
	methods, err := common.Marshal(config.PayMethods)
	require.NoError(t, err)
	return map[string]string{
		"WaffoEnabled": strconv.FormatBool(config.Enabled), "WaffoApiKey": config.APIKey,
		"WaffoPrivateKey": config.PrivateKey, "WaffoPublicCert": config.PublicCert,
		"WaffoSandboxPublicCert": config.SandboxPublicCert, "WaffoSandboxApiKey": config.SandboxAPIKey,
		"WaffoSandboxPrivateKey": config.SandboxPrivateKey, "WaffoSandbox": strconv.FormatBool(config.Sandbox),
		"WaffoMerchantId": config.MerchantID, "WaffoNotifyUrl": config.NotifyURL,
		"WaffoReturnUrl": config.ReturnURL, "WaffoCurrency": config.Currency,
		"WaffoUnitPrice": strconv.FormatFloat(config.UnitPrice, 'f', -1, 64), "WaffoMinTopUp": strconv.Itoa(config.MinTopUp),
		"WaffoPayMethods": string(methods),
	}
}

func epaySnapshotValues(t *testing.T, config operation_setting.EpayConfig) map[string]string {
	t.Helper()
	methods, err := common.Marshal(config.PayMethods)
	require.NoError(t, err)
	return map[string]string{
		"PayAddress": config.PayAddress, "CustomCallbackAddress": config.CustomCallbackAddress,
		"EpayId": config.EpayID, "EpayKey": config.EpayKey,
		"Price": strconv.FormatFloat(config.Price, 'f', -1, 64), "MinTopUp": strconv.Itoa(config.MinTopUp),
		"USDExchangeRate": strconv.FormatFloat(config.USDExchangeRate, 'f', -1, 64), "PayMethods": string(methods),
	}
}
