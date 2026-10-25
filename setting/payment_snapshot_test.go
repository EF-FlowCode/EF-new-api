package setting

import (
	"reflect"
	"strconv"
	"sync"
	"testing"

	"eflow.local/new-api/common"
	"github.com/stretchr/testify/require"
)

func TestPaymentConfigInvalidBatchKeepsPublishedSnapshot(t *testing.T) {
	t.Run("Stripe", func(t *testing.T) {
		preserveStripeConfig(t)
		require.NoError(t, UpdateStripeConfig(map[string]string{
			"StripeApiSecret": "sk_before",
			"StripeUnitPrice": "8.5",
		}))
		before := GetStripeConfig()
		invalid := map[string]string{"StripeApiSecret": "sk_after", "StripeUnitPrice": "NaN"}

		require.Error(t, ValidateStripeOptionValues(invalid))
		require.Equal(t, before, GetStripeConfig())
		require.Error(t, UpdateStripeConfig(invalid))
		require.Equal(t, before, GetStripeConfig())
	})

	t.Run("Creem", func(t *testing.T) {
		preserveCreemConfig(t)
		require.NoError(t, UpdateCreemConfig(map[string]string{
			"CreemApiKey":   "creem-before",
			"CreemProducts": `[{"productId":"product-before","price":1,"quota":10}]`,
		}))
		before := GetCreemConfig()
		invalid := map[string]string{"CreemApiKey": "creem-after", "CreemProducts": `[{}]`}

		require.Error(t, ValidateCreemOptionValues(invalid))
		require.Equal(t, before, GetCreemConfig())
		require.Error(t, UpdateCreemConfig(invalid))
		require.Equal(t, before, GetCreemConfig())
	})

	t.Run("Waffo", func(t *testing.T) {
		preserveWaffoConfig(t)
		require.NoError(t, UpdateWaffoConfig(map[string]string{
			"WaffoApiKey":     "waffo-before",
			"WaffoPayMethods": `[{"name":"Card","payMethodType":"CARD"}]`,
		}))
		before := GetWaffoConfig()
		invalid := map[string]string{"WaffoApiKey": "waffo-after", "WaffoPayMethods": `[null]`}

		require.Error(t, ValidateWaffoOptionValues(invalid))
		require.Equal(t, before, GetWaffoConfig())
		require.Error(t, UpdateWaffoConfig(invalid))
		require.Equal(t, before, GetWaffoConfig())
	})
}

func TestPaymentConfigGettersReturnDetachedSlices(t *testing.T) {
	preserveCreemConfig(t)
	preserveWaffoConfig(t)

	require.NoError(t, UpdateCreemConfig(map[string]string{
		"CreemProducts": `[{"productId":"product","name":"original","price":1,"quota":10}]`,
	}))
	creem := GetCreemConfig()
	creem.Products[0].Name = "mutated"
	require.Equal(t, "original", GetCreemConfig().Products[0].Name)

	require.NoError(t, UpdateWaffoConfig(map[string]string{
		"WaffoPayMethods": `[{"name":"original","payMethodType":"CARD"}]`,
	}))
	waffo := GetWaffoConfig()
	waffo.PayMethods[0].Name = "mutated"
	require.Equal(t, "original", GetWaffoConfig().PayMethods[0].Name)
}

func TestPaymentConfigReadersObserveCompleteVersions(t *testing.T) {
	t.Run("Stripe", func(t *testing.T) {
		preserveStripeConfig(t)
		assertCompleteVersions(t, UpdateStripeConfig, GetStripeConfig,
			map[string]string{
				"StripeApiSecret": "sk_a", "StripeWebhookSecret": "whsec_a", "StripePriceId": "price_a",
				"StripeUnitPrice": "1", "StripeMinTopUp": "1", "StripePromotionCodesEnabled": "false",
			},
			map[string]string{
				"StripeApiSecret": "sk_b", "StripeWebhookSecret": "whsec_b", "StripePriceId": "price_b",
				"StripeUnitPrice": "2", "StripeMinTopUp": "2", "StripePromotionCodesEnabled": "true",
			},
			func(a, b StripeConfig) bool { return a == b },
		)
	})

	t.Run("Creem", func(t *testing.T) {
		preserveCreemConfig(t)
		assertCompleteVersions(t, UpdateCreemConfig, GetCreemConfig,
			map[string]string{
				"CreemApiKey": "key-a", "CreemWebhookSecret": "secret-a", "CreemTestMode": "false",
				"CreemProducts": `[{"productId":"product-a","name":"A","price":1,"currency":"USD","quota":10}]`,
			},
			map[string]string{
				"CreemApiKey": "key-b", "CreemWebhookSecret": "secret-b", "CreemTestMode": "true",
				"CreemProducts": `[{"productId":"product-b","name":"B","price":2,"currency":"EUR","quota":20}]`,
			},
			func(a, b CreemConfig) bool { return reflect.DeepEqual(a, b) },
		)
	})

	t.Run("Waffo", func(t *testing.T) {
		preserveWaffoConfig(t)
		assertCompleteVersions(t, UpdateWaffoConfig, GetWaffoConfig,
			map[string]string{
				"WaffoEnabled": "true", "WaffoApiKey": "api-a", "WaffoPrivateKey": "private-a",
				"WaffoPublicCert": "public-a", "WaffoSandboxPublicCert": "sandbox-public-a",
				"WaffoSandboxApiKey": "sandbox-api-a", "WaffoSandboxPrivateKey": "sandbox-private-a",
				"WaffoSandbox": "false", "WaffoMerchantId": "merchant-a", "WaffoNotifyUrl": "notify-a",
				"WaffoReturnUrl": "return-a", "WaffoCurrency": "USD", "WaffoUnitPrice": "1",
				"WaffoMinTopUp": "1", "WaffoPayMethods": `[{"name":"A","payMethodType":"CARD-A"}]`,
			},
			map[string]string{
				"WaffoEnabled": "false", "WaffoApiKey": "api-b", "WaffoPrivateKey": "private-b",
				"WaffoPublicCert": "public-b", "WaffoSandboxPublicCert": "sandbox-public-b",
				"WaffoSandboxApiKey": "sandbox-api-b", "WaffoSandboxPrivateKey": "sandbox-private-b",
				"WaffoSandbox": "true", "WaffoMerchantId": "merchant-b", "WaffoNotifyUrl": "notify-b",
				"WaffoReturnUrl": "return-b", "WaffoCurrency": "JPY", "WaffoUnitPrice": "2",
				"WaffoMinTopUp": "2", "WaffoPayMethods": `[{"name":"B","payMethodType":"CARD-B"}]`,
			},
			func(a, b WaffoConfig) bool { return reflect.DeepEqual(a, b) },
		)
	})
}

func assertCompleteVersions[T any](
	t *testing.T,
	update func(map[string]string) error,
	get func() T,
	versionA map[string]string,
	versionB map[string]string,
	equal func(T, T) bool,
) {
	t.Helper()
	require.NoError(t, update(versionA))
	wantA := get()
	require.NoError(t, update(versionB))
	wantB := get()
	require.NoError(t, update(versionA))

	const iterations = 500
	start := make(chan struct{})
	invalid := make(chan T, 1)
	var readers sync.WaitGroup
	for range 4 {
		readers.Add(1)
		go func() {
			defer readers.Done()
			<-start
			for range iterations * 4 {
				got := get()
				if !equal(got, wantA) && !equal(got, wantB) {
					select {
					case invalid <- got:
					default:
					}
					return
				}
			}
		}()
	}
	close(start)
	for i := range iterations {
		values := versionA
		if i%2 == 0 {
			values = versionB
		}
		require.NoError(t, update(values))
	}
	readers.Wait()
	select {
	case got := <-invalid:
		t.Fatalf("reader observed mixed config: %#v", got)
	default:
	}
}

func preserveStripeConfig(t *testing.T) {
	t.Helper()
	original := GetStripeConfig()
	t.Cleanup(func() { require.NoError(t, UpdateStripeConfig(stripeOptionValues(original))) })
}

func preserveCreemConfig(t *testing.T) {
	t.Helper()
	original := GetCreemConfig()
	t.Cleanup(func() { require.NoError(t, UpdateCreemConfig(creemOptionValues(original))) })
}

func preserveWaffoConfig(t *testing.T) {
	t.Helper()
	original := GetWaffoConfig()
	t.Cleanup(func() { require.NoError(t, UpdateWaffoConfig(waffoSnapshotOptionValues(t, original))) })
}

func stripeOptionValues(config StripeConfig) map[string]string {
	return map[string]string{
		"StripeApiSecret": config.APISecret, "StripeWebhookSecret": config.WebhookSecret,
		"StripePriceId": config.PriceID, "StripeUnitPrice": strconv.FormatFloat(config.UnitPrice, 'f', -1, 64),
		"StripeMinTopUp":              strconv.Itoa(config.MinTopUp),
		"StripePromotionCodesEnabled": strconv.FormatBool(config.PromotionCodesEnabled),
	}
}

func creemOptionValues(config CreemConfig) map[string]string {
	return map[string]string{
		"CreemApiKey": config.APIKey, "CreemProducts": config.ProductsJSON,
		"CreemTestMode": strconv.FormatBool(config.TestMode), "CreemWebhookSecret": config.WebhookSecret,
	}
}

func waffoSnapshotOptionValues(t *testing.T, config WaffoConfig) map[string]string {
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
		"WaffoUnitPrice": strconv.FormatFloat(config.UnitPrice, 'f', -1, 64),
		"WaffoMinTopUp":  strconv.Itoa(config.MinTopUp), "WaffoPayMethods": string(methods),
	}
}
