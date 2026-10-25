package helper

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"eflow.local/new-api/common"
	"eflow.local/new-api/pkg/billingexpr"
	"eflow.local/new-api/pkg/jsplugin"
	relaycommon "eflow.local/new-api/relay/common"
	"eflow.local/new-api/setting/billing_setting"
	"eflow.local/new-api/setting/config"
	"eflow.local/new-api/setting/ratio_setting"
	"eflow.local/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestModelPriceUsageRequiresTaskFactsAndRejectsOverflow(t *testing.T) {
	saved := map[string]string{}
	require.NoError(t, config.GlobalConfig.SaveToDB(func(k, v string) error { saved[k] = v; return nil }))
	t.Cleanup(func() { require.NoError(t, config.GlobalConfig.LoadFromDB(saved)) })
	schema := map[string]jsplugin.UsageFieldSchema{"seconds": {Type: "number", Unit: "second"}}
	for _, expression := range []string{`v2:u("seconds") * 0.01`, `v2:u("seconds") * 1e30`} {
		rules, err := json.Marshal(map[string]string{"usage-test": expression})
		require.NoError(t, err)
		require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{
			"billing_setting.billing_mode": `{"usage-test":"tiered_expr"}`,
			"billing_setting.billing_expr": string(rules),
		}))
		ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
		ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", nil)
		info := &relaycommon.RelayInfo{OriginModelName: "usage-test", UsingGroup: "default"}
		_, err = ModelPriceHelper(ctx, info, 100, &types.TokenCountMeta{})
		require.Error(t, err, "ordinary token pricing cannot run usage expressions")
		_, err = ModelPriceHelperPerCall(ctx, info)
		require.Error(t, err, "request body alone cannot supply usage facts")
		price, err := ModelPriceHelperPerCallWithUsage(ctx, info, schema, map[string]any{"seconds": float64(5)})
		if expression == `v2:u("seconds") * 1e30` {
			var clamp *common.QuotaClamp
			require.ErrorAs(t, err, &clamp)
		} else {
			require.NoError(t, err)
			require.True(t, price.TaskFixedTotal)
			require.Equal(t, int(0.05*common.GetQuotaPerUnit()), price.Quota)
			require.Equal(t, expression, info.TieredBillingSnapshot.ExprString)
		}
	}
}

func TestModelPriceAmountTokenAndTask(t *testing.T) {
	oldGroups := ratio_setting.GroupRatio2JSONString()
	t.Cleanup(func() { require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(oldGroups)) })
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":0.5}`))
	saved := map[string]string{}
	require.NoError(t, config.GlobalConfig.SaveToDB(func(key, value string) error { saved[key] = value; return nil }))
	t.Cleanup(func() { require.NoError(t, config.GlobalConfig.LoadFromDB(saved)) })
	for _, legacy := range []string{`tier("base", p * 2 + c * 10)`, `tier("video", param("seconds") * 10000)`} {
		amount, err := billingexpr.AmountExpression(legacy)
		require.NoError(t, err)
		for _, expression := range []string{legacy, amount} {
			rules, err := json.Marshal(map[string]string{"amount-test": expression})
			require.NoError(t, err)
			require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{
				"billing_setting.billing_mode": `{"amount-test":"tiered_expr"}`,
				"billing_setting.billing_expr": string(rules),
			}))
			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", nil)
			ctx.Set("group", "default")
			info := &relaycommon.RelayInfo{OriginModelName: "amount-test", UserGroup: "default", UsingGroup: "default", BillingRequestInput: &billingexpr.RequestInput{Body: []byte(`{"seconds":5}`)}}
			if billingexpr.UsedVars(expression)["param"] {
				price, err := ModelPriceHelperPerCall(ctx, info)
				require.NoError(t, err)
				require.True(t, price.TaskFixedTotal)
				require.Equal(t, 12500, price.Quota)
			} else {
				price, err := ModelPriceHelper(ctx, info, 1000, &types.TokenCountMeta{})
				require.NoError(t, err)
				require.Equal(t, 500, price.QuotaToPreConsume)
				require.Equal(t, billingexpr.ExprVersion(expression), info.TieredBillingSnapshot.ExprVersion)
				settled, err := billingexpr.ComputeTieredQuota(info.TieredBillingSnapshot, billingexpr.TokenParams{P: 1000, C: 100})
				require.NoError(t, err)
				require.Equal(t, 750, settled.ActualQuotaAfterGroup)
			}
		}
	}
}

func TestModelPriceHelperTieredUsesPreloadedRequestInput(t *testing.T) {
	gin.SetMode(gin.TestMode)

	saved := map[string]string{}
	require.NoError(t, config.GlobalConfig.SaveToDB(func(key, value string) error {
		saved[key] = value
		return nil
	}))
	t.Cleanup(func() {
		require.NoError(t, config.GlobalConfig.LoadFromDB(saved))
	})

	require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{
		"billing_setting.billing_mode": `{"tiered-test-model":"tiered_expr","flat-test-model":"flat_price"}`,
		"billing_setting.billing_expr": `{"tiered-test-model":"param(\"stream\") == true ? tier(\"stream\", p * 3) : tier(\"base\", p * 2)","flat-test-model":"tier(\"default\", p * 3 + c * 15 + cr * 1 + cc * 3.75 + cc1h * 6)"}`,
	}))

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	req := httptest.NewRequest(http.MethodPost, "/api/channel/test/1", nil)
	req.Body = nil
	req.ContentLength = 0
	req.Header.Set("Content-Type", "application/json")
	ctx.Request = req
	ctx.Set("group", "default")

	info := &relaycommon.RelayInfo{
		OriginModelName: "tiered-test-model",
		UserGroup:       "default",
		UsingGroup:      "default",
		RequestHeaders:  map[string]string{"Content-Type": "application/json"},
		BillingRequestInput: &billingexpr.RequestInput{
			Headers: map[string]string{"Content-Type": "application/json"},
			Body:    []byte(`{"stream":true}`),
		},
	}

	priceData, err := ModelPriceHelper(ctx, info, 1000, &types.TokenCountMeta{
		BillingRatios: map[string]float64{"n": 3},
	})
	require.NoError(t, err)
	require.Equal(t, 1500, priceData.QuotaToPreConsume)
	require.NotNil(t, info.TieredBillingSnapshot)
	require.Equal(t, "stream", info.TieredBillingSnapshot.EstimatedTier)
	require.Equal(t, billing_setting.BillingModeTieredExpr, info.TieredBillingSnapshot.BillingMode)
	require.Equal(t, common.GetQuotaPerUnit(), info.TieredBillingSnapshot.QuotaPerUnit)

	flatInfo := &relaycommon.RelayInfo{
		OriginModelName: "flat-test-model",
		UserGroup:       "default",
		UsingGroup:      "default",
		BillingRequestInput: &billingexpr.RequestInput{
			Body: []byte(`{}`),
		},
	}
	flatPriceData, err := ModelPriceHelper(
		ctx,
		flatInfo,
		1000,
		&types.TokenCountMeta{},
	)
	require.NoError(t, err)
	require.Equal(t, 1500, flatPriceData.QuotaToPreConsume)
	require.NotNil(t, flatInfo.TieredBillingSnapshot)
	require.Equal(t, billing_setting.BillingModeFlatPrice, flatInfo.TieredBillingSnapshot.BillingMode)
}

func TestModelPriceHelperTieredRejectsPreConsumeOverflow(t *testing.T) {
	gin.SetMode(gin.TestMode)

	saved := map[string]string{}
	require.NoError(t, config.GlobalConfig.SaveToDB(func(key, value string) error {
		saved[key] = value
		return nil
	}))
	t.Cleanup(func() {
		require.NoError(t, config.GlobalConfig.LoadFromDB(saved))
	})

	require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{
		"billing_setting.billing_mode":    `{"tiered-overflow-model":"tiered_expr"}`,
		"billing_setting.billing_expr":    `{"tiered-overflow-model":"tier(\"overflow\", p * 1000000000000000)"}`,
		"group_ratio_setting.group_ratio": `{"default":1}`,
	}))

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	ctx.Set("group", "default")
	info := &relaycommon.RelayInfo{
		OriginModelName: "tiered-overflow-model",
		UserGroup:       "default",
		UsingGroup:      "default",
		BillingRequestInput: &billingexpr.RequestInput{
			Body: []byte(`{}`),
		},
	}

	_, err := ModelPriceHelper(ctx, info, 1000, &types.TokenCountMeta{})

	var clamp *common.QuotaClamp
	require.ErrorAs(t, err, &clamp)
	require.Equal(t, "QuotaRound", clamp.Op)
	require.Equal(t, common.QuotaClampOverflow, clamp.Kind)
}

func TestModelPriceHelperRequestBillingRatiosOnlyApplyToFixedPrice(t *testing.T) {
	gin.SetMode(gin.TestMode)
	savedModelPrices := ratio_setting.ModelPrice2JSONString()
	savedModelRatios := ratio_setting.ModelRatio2JSONString()
	t.Cleanup(func() {
		require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(savedModelPrices))
		require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(savedModelRatios))
	})

	modelPrices, err := common.Marshal(map[string]float64{
		"fixed-image-price":      0.04,
		"fractional-image-price": 0.0000012,
		"overflow-image-price":   float64(common.MaxQuota) / common.GetQuotaPerUnit() / 2,
	})
	require.NoError(t, err)
	require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(string(modelPrices)))
	modelRatios, err := common.Marshal(map[string]float64{"ratio-image-price": 15})
	require.NoError(t, err)
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(string(modelRatios)))

	tests := []struct {
		name           string
		model          string
		wantQuota      int
		wantUsePrice   bool
		wantImageCount bool
	}{
		{
			name:           "fixed price applies image count",
			model:          "fixed-image-price",
			wantQuota:      180000,
			wantUsePrice:   true,
			wantImageCount: true,
		},
		{
			name:         "ratio price ignores request billing ratios",
			model:        "ratio-image-price",
			wantQuota:    15000,
			wantUsePrice: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			ctx.Set("group", "default")
			info := &relaycommon.RelayInfo{
				OriginModelName: tt.model,
				UserGroup:       "default",
				UsingGroup:      "default",
			}
			meta := &types.TokenCountMeta{
				ImagePriceRatio: 3,
				BillingRatios:   map[string]float64{"n": 3},
			}

			priceData, err := ModelPriceHelper(ctx, info, 1000, meta)

			require.NoError(t, err)
			require.Equal(t, tt.wantQuota, priceData.QuotaToPreConsume)
			require.Equal(t, tt.wantUsePrice, priceData.UsePrice)
			_, hasImageCount := priceData.OtherRatios["n"]
			require.Equal(t, tt.wantImageCount, hasImageCount)
			require.Equal(t, priceData.OtherRatios, info.PriceData.OtherRatios)
		})
	}

	newInfo := func(model string) (*gin.Context, *relaycommon.RelayInfo) {
		ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
		ctx.Set("group", "default")
		return ctx, &relaycommon.RelayInfo{
			OriginModelName: model,
			UserGroup:       "default",
			UsingGroup:      "default",
		}
	}
	meta := &types.TokenCountMeta{BillingRatios: map[string]float64{"n": 3}}

	ctx, info := newInfo("fractional-image-price")
	priceData, err := ModelPriceHelper(ctx, info, 0, meta)
	require.NoError(t, err)
	// 0.0000012 * 500000 * 3 = 1.8, then truncate once to 1.
	require.Equal(t, 1, priceData.QuotaToPreConsume)

	ctx, info = newInfo("overflow-image-price")
	_, err = ModelPriceHelper(ctx, info, 0, meta)
	var clamp *common.QuotaClamp
	require.ErrorAs(t, err, &clamp)
	require.Equal(t, "QuotaFromFloat", clamp.Op)
	require.Equal(t, common.QuotaClampOverflow, clamp.Kind)
	require.Nil(t, info.Billing)
}

func TestModelPriceHelperForPluginRejectsFlatPriceModels(t *testing.T) {
	gin.SetMode(gin.TestMode)
	saved := map[string]string{}
	require.NoError(t, config.GlobalConfig.SaveToDB(func(key, value string) error { saved[key] = value; return nil }))
	t.Cleanup(func() { require.NoError(t, config.GlobalConfig.LoadFromDB(saved)) })
	require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{
		"billing_setting.task_priority_version": "0",
		"billing_setting.billing_mode":          `{"flat-plugin-model":"flat_price","flat-plugin-tail":"flat_price","expr-plugin-model":"tiered_expr"}`,
		"billing_setting.billing_expr":          `{"flat-plugin-model":"tier(\"default\", p * 3 + c * 15)","flat-plugin-tail":"tier(\"default\", p * 3 + c * 15)","expr-plugin-model":"v2:0.2"}`,
		billing_setting.TaskExpressionsOption:   `{}`,
	}))
	for _, tc := range []struct{ name, upstream, source string }{
		{"flat-plugin-model", "upstream-x", "model_original"},
		{"flat-plugin-alias", "flat-plugin-tail", "model_mapped"},
	} {
		rule := billing_setting.ResolveTaskBillingRule("sora", tc.name, tc.upstream)
		require.Equal(t, tc.source, rule.Source, tc.name)
		require.Equal(t, billing_setting.BillingModeFlatPrice, rule.BillingMode, tc.name)
		ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
		ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", nil)
		info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: tc.upstream}, OriginModelName: tc.name, UserGroup: "default", UsingGroup: "default", BillingRequestInput: &billingexpr.RequestInput{Body: []byte(`{}`)}}
		price, err := ModelPriceHelperForPlugin(ctx, info, "sora", nil, nil)
		require.Error(t, err, "%s: a flat token price must not be billed per call for free", tc.source)
		require.Zero(t, price.Quota)
		require.False(t, price.FreeModel)
	}
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", nil)
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "upstream-x"}, OriginModelName: "expr-plugin-model", UserGroup: "default", UsingGroup: "default", BillingRequestInput: &billingexpr.RequestInput{Body: []byte(`{}`)}}
	price, err := ModelPriceHelperForPlugin(ctx, info, "sora", nil, nil)
	require.NoError(t, err)
	require.Equal(t, billing_setting.BillingModeTieredExpr, info.TieredBillingSnapshot.BillingMode)
	require.Positive(t, price.Quota)
}

func TestModelPriceHelperForPluginFollowsModelPriceEditsAfterMigration(t *testing.T) {
	gin.SetMode(gin.TestMode)
	saved := map[string]string{}
	require.NoError(t, config.GlobalConfig.SaveToDB(func(key, value string) error { saved[key] = value; return nil }))
	prices, ratios := ratio_setting.ModelPrice2JSONString(), ratio_setting.ModelRatio2JSONString()
	t.Cleanup(func() {
		require.NoError(t, config.GlobalConfig.LoadFromDB(saved))
		require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(prices))
		require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(ratios))
	})
	require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{
		billing_setting.TaskPriorityOption:    "1",
		"billing_setting.billing_mode":        `{"follow-expr":"tiered_expr"}`,
		"billing_setting.billing_expr":        `{"follow-expr":"v2:0.2"}`,
		billing_setting.TaskExpressionsOption: `{}`,
	}))
	price := func(name string) types.PriceData {
		t.Helper()
		ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
		ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", nil)
		info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "upstream-x"}, OriginModelName: name, UserGroup: "default", UsingGroup: "default", BillingRequestInput: &billingexpr.RequestInput{Body: []byte(`{}`)}}
		data, err := ModelPriceHelperForPlugin(ctx, info, "sora", nil, nil)
		require.NoError(t, err, name)
		return data
	}
	require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(`{"follow-price":0.1}`))
	first := price("follow-price")
	require.True(t, first.UsePrice)
	require.Equal(t, 0.1, first.ModelPrice)
	require.Positive(t, first.Quota)
	require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(`{"follow-price":0.2}`))
	second := price("follow-price")
	require.Equal(t, 0.2, second.ModelPrice, "the next task request uses the edited model price")
	require.Equal(t, first.Quota*2, second.Quota)
	require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(`{}`))
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"follow-price":3}`))
	ratio := price("follow-price")
	require.False(t, ratio.UsePrice)
	require.Equal(t, 3.0, ratio.ModelRatio)
	expr := price("follow-expr")
	require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{"billing_setting.billing_expr": `{"follow-expr":"v2:0.4"}`}))
	require.Equal(t, expr.Quota*2, price("follow-expr").Quota, "an edited model expression applies to the next task request")
	// An explicit plugin price still overrides the followed model price.
	require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{billing_setting.TaskExpressionsOption: `{"follow-expr":{"sora":"v2:0.2"}}`}))
	require.Equal(t, expr.Quota, price("follow-expr").Quota)
}
