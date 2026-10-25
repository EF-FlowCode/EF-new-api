package billing_setting

import (
	"sync"
	"testing"

	"eflow.local/new-api/common"
	"eflow.local/new-api/setting/config"
	"eflow.local/new-api/setting/ratio_setting"
	"github.com/stretchr/testify/require"
)

func TestSmokeTestExprAllowsRequestParamPricing(t *testing.T) {
	expression := `param("metadata.resolution") == "1080p" ? tier("1080P @ $2.70/秒", param("metadata.duration") * 2.70 * 1000000) : param("metadata.resolution") == "720p" ? tier("720P @ $1.40/秒", param("metadata.duration") * 1.40 * 1000000) : tier("480P @ $0.75/秒", param("metadata.duration") * 0.75 * 1000000)`
	if err := SmokeTestExpr(expression); err != nil {
		t.Fatal(err)
	}
}

func TestSmokeTestExprStillValidatesParamExpressionSyntax(t *testing.T) {
	if err := SmokeTestExpr(`param("metadata.duration") *`); err == nil {
		t.Fatal("expected invalid expression to be rejected")
	}
}

func TestNormalizeExpressionBillingModeClassifiesLegacyFlatPrice(t *testing.T) {
	fixed := `tier("base", p * 3 + c * 15 + cr * 0.3 + cc * 3.75 + cc1h * 6)`
	dynamic := `(tier("base", p * 4.5 + c * 13.5 + cr * 0.45)) * (hour("Asia/Shanghai") >= 8 && hour("Asia/Shanghai") < 22 ? 2 : 1)`

	require.Equal(t, BillingModeFlatPrice, NormalizeExpressionBillingMode(BillingModeTieredExpr, fixed))
	require.Equal(t, BillingModeFlatPrice, NormalizeExpressionBillingMode(BillingModeFlatPrice, fixed))
	require.Equal(t, BillingModeTieredExpr, NormalizeExpressionBillingMode(BillingModeTieredExpr, dynamic))
	require.Equal(t, BillingModeTieredExpr, NormalizeExpressionBillingMode(BillingModeFlatPrice, dynamic))
	require.Equal(t, BillingModeRatio, NormalizeExpressionBillingMode(BillingModeRatio, fixed))
}

func TestGetBillingRuleObservesOnePublishedVersion(t *testing.T) {
	original := billingSettingSnapshot()
	t.Cleanup(func() {
		_, err := config.GlobalConfig.Update("billing_setting", billingSettingValues(t, original))
		require.NoError(t, err)
	})

	const model = "snapshot-model"
	first := BillingSetting{
		BillingMode: map[string]string{model: BillingModeFlatPrice},
		BillingExpr: map[string]string{model: `tier("first", p)`},
	}
	second := BillingSetting{
		BillingMode: map[string]string{model: BillingModeTieredExpr},
		BillingExpr: map[string]string{model: `tier("second", p * 2)`},
	}
	_, err := config.GlobalConfig.Update("billing_setting", billingSettingValues(t, first))
	require.NoError(t, err)

	const iterations = 1000
	start := make(chan struct{})
	torn := make(chan struct {
		mode string
		expr string
	}, 1)
	var readers sync.WaitGroup
	for range 4 {
		readers.Add(1)
		go func() {
			defer readers.Done()
			<-start
			for range iterations {
				mode, expr, ok := GetBillingRule(model)
				if !ok || (mode != first.BillingMode[model] || expr != first.BillingExpr[model]) &&
					(mode != second.BillingMode[model] || expr != second.BillingExpr[model]) {
					select {
					case torn <- struct {
						mode string
						expr string
					}{mode: mode, expr: expr}:
					default:
					}
					return
				}
			}
		}()
	}
	close(start)
	for i := range iterations {
		next := first
		if i%2 == 0 {
			next = second
		}
		_, err = config.GlobalConfig.Update("billing_setting", billingSettingValues(t, next))
		require.NoError(t, err)
	}
	readers.Wait()
	select {
	case got := <-torn:
		t.Fatalf("GetBillingRule observed a torn version: mode=%q expr=%q", got.mode, got.expr)
	default:
	}
}

func billingSettingValues(t *testing.T, setting BillingSetting) map[string]string {
	t.Helper()
	modes, err := common.Marshal(setting.BillingMode)
	require.NoError(t, err)
	expressions, err := common.Marshal(setting.BillingExpr)
	require.NoError(t, err)
	return map[string]string{
		"billing_mode": string(modes),
		"billing_expr": string(expressions),
	}
}

func TestGetBillingRuleKeepsPerCallPriceForKnowledgeModels(t *testing.T) {
	originalRatio := ratio_setting.ModelRatio2JSONString()
	originalPrice := ratio_setting.ModelPrice2JSONString()
	t.Cleanup(func() {
		require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(originalRatio))
		require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(originalPrice))
	})

	const model = "text-embedding-3-small"
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"text-embedding-3-small":0.01}`))
	require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(`{}`))
	mode, _, synthesized := GetBillingRule(model)
	require.True(t, synthesized)
	require.Equal(t, BillingModeTieredExpr, mode)

	require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(`{"text-embedding-3-small":0.01}`))
	mode, _, synthesized = GetBillingRule(model)
	require.False(t, synthesized, "a per-call price must not be shadowed by the synthesized token rule")
	require.Equal(t, BillingModeRatio, mode)
}
