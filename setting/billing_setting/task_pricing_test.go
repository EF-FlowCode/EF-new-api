package billing_setting

import (
	"eflow.local/new-api/setting/config"
	"eflow.local/new-api/setting/ratio_setting"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestTaskPricingResolutionPreservesOriginalContract(t *testing.T) {
	saved := config.GlobalConfig.ExportAllConfigs()
	prices, ratios := ratio_setting.ModelPrice2JSONString(), ratio_setting.ModelRatio2JSONString()
	t.Cleanup(func() {
		require.NoError(t, config.GlobalConfig.LoadFromDB(saved))
		require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(prices))
		require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(ratios))
	})
	require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(`{"original-zero":0,"original-price":0.0000019}`))
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"original-ratio":2}`))
	require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{
		"billing_setting.billing_mode": `{"original-expr":"tiered_expr","tail":"tiered_expr","empty":"tiered_expr"}`,
		"billing_setting.billing_expr": `{"original-expr":"v2:0.3","tail":"v2:0.4"}`,
		TaskExpressionsOption:          `{"tail":{"alibaba":"v2:0.2"},"original-price":{"sora":"v2:0"}}`,
	}))
	for _, tc := range []struct{ plugin, name, tail, source, expression string }{
		{"alibaba", "new", "tail", "plugin_mapped", "v2:0.2"},
		{"sora", "new", "tail", "model_mapped", "v2:0.4"},
		{"alibaba", "original-zero", "tail", "legacy_original", ""},
		{"alibaba", "original-price", "tail", "legacy_original", ""},
		{"alibaba", "original-ratio", "tail", "legacy_original", ""},
		{"alibaba", "original-expr", "tail", "model_original", "v2:0.3"},
		{"sora", "original-price", "tail", "plugin_original", "v2:0"},
		{"sora", "empty", "tail", "model_original", ""},
		{"sora", "missing", "none", "missing", ""},
	} {
		r := ResolveTaskBillingRule(tc.plugin, tc.name, tc.tail)
		require.Equal(t, tc.source, r.Source, tc.name)
		require.Equal(t, tc.expression, r.Expression, tc.name)
		if tc.name == "empty" {
			require.Error(t, r.Validate(nil))
		}
	}
}

func TestTaskPricingRejectsMalformedRules(t *testing.T) {
	for _, raw := range []string{`null`, `[]`, `{"m":null}`, `{"m":{}}`, `{"m":{"bad::key":"v2:1"}}`, `{"m":{"sora":"1"}}`, `{"m":{"sora":"v2:"}}`, `{"m":{"sora":"v2:-1"}}`, `{"m":{"sora":"v2:u(param(\"key\"))"}}`} {
		require.Error(t, ValidateTaskExpressions(raw), raw)
	}
	require.NoError(t, ValidateTaskExpressions(`{"m":{"sora":"v2:0"}}`))
}

func TestMigratedTaskPricingFollowsModelPriceAndRejectsImplicitMapping(t *testing.T) {
	s := BillingSetting{TaskPriorityVersion: 1,
		BillingMode: map[string]string{"expr": BillingModeTieredExpr, "tail-expr": BillingModeTieredExpr},
		BillingExpr: map[string]string{"expr": "v2:0.3", "tail-expr": "v2:0.4"},
		TaskExpressions: map[string]map[string]TaskPriceRule{
			"tail": {"sora": {Version: 1, Kind: "amount", Expression: "v2:0.2"}},
		}}
	// Without a plugin override the model's own live price applies, exactly as
	// before migration; it outranks a mapped plugin price.
	r := SelectTaskBillingRule(s, "sora", "alias", "tail", true)
	require.Equal(t, "legacy_original", r.Source)
	require.NoError(t, r.Validate(nil))
	r = SelectTaskBillingRule(s, "sora", "expr", "tail", false)
	require.Equal(t, "model_original", r.Source)
	require.Equal(t, "v2:0.3", r.Expression)
	require.Equal(t, BillingModeTieredExpr, r.BillingMode)
	require.NoError(t, r.Validate(nil))
	r = SelectTaskBillingRule(s, "sora", "alias", "tail", false)
	require.Equal(t, "plugin_mapped", r.Source)
	require.NoError(t, r.Validate(nil))
	// A model without its own price must not silently inherit a mapped general price.
	r = SelectTaskBillingRule(s, "sora", "alias", "tail-expr", false)
	require.Equal(t, "explicit_task_rule_required", r.Source)
	require.Error(t, r.Validate(nil))
	r = SelectTaskBillingRule(s, "sora", "new", "missing", false)
	require.Equal(t, "explicit_task_rule_required", r.Source)
	require.Error(t, r.Validate(nil))
	s.TaskPriorityVersion = 0
	r = SelectTaskBillingRule(s, "sora", "alias", "tail-expr", false)
	require.Equal(t, "model_mapped", r.Source, "unmigrated sites keep the mapped fallback")
	require.Equal(t, "v2:0.4", r.Expression)
	s.TaskPriorityVersion = 1
	s.TaskExpressions["alias"] = map[string]TaskPriceRule{"sora": {Version: 1, Kind: "amount", Expression: "v2:0"}}
	r = SelectTaskBillingRule(s, "sora", "alias", "tail", true)
	require.Equal(t, "plugin_original", r.Source)
	require.Equal(t, "v2:0", r.Expression)
	require.NoError(t, r.Validate(nil))
}

func TestTaskPricingFollowsLiveModelPriceEdits(t *testing.T) {
	saved := config.GlobalConfig.ExportAllConfigs()
	prices, ratios := ratio_setting.ModelPrice2JSONString(), ratio_setting.ModelRatio2JSONString()
	t.Cleanup(func() {
		require.NoError(t, config.GlobalConfig.LoadFromDB(saved))
		require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(prices))
		require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(ratios))
	})
	require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{
		TaskPriorityOption:             "1",
		"billing_setting.billing_mode": `{}`,
		"billing_setting.billing_expr": `{}`,
		TaskExpressionsOption:          `{}`,
	}))
	require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(`{}`))
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{}`))
	require.Equal(t, "explicit_task_rule_required", ResolveTaskBillingRule("sora", "live", "tail").Source)
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"live":2}`))
	require.Equal(t, "legacy_original", ResolveTaskBillingRule("sora", "live", "tail").Source)
	require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(`{"live":0.1}`))
	require.Equal(t, "legacy_original", ResolveTaskBillingRule("sora", "live", "tail").Source)
	require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{
		"billing_setting.billing_mode": `{"live":"tiered_expr"}`,
		"billing_setting.billing_expr": `{"live":"v2:0.3"}`,
	}))
	r := ResolveTaskBillingRule("sora", "live", "tail")
	require.Equal(t, "model_original", r.Source)
	require.Equal(t, "v2:0.3", r.Expression)
	require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{"billing_setting.billing_expr": `{"live":"v2:0.4"}`}))
	require.Equal(t, "v2:0.4", ResolveTaskBillingRule("sora", "live", "tail").Expression)
}

func TestTaskPricingModelSourcesCarryFlatPriceMode(t *testing.T) {
	s := BillingSetting{
		BillingMode: map[string]string{"flat": BillingModeFlatPrice, "flat-tail": BillingModeFlatPrice, "expr": BillingModeTieredExpr},
		BillingExpr: map[string]string{"flat": `tier("default", p * 3 + c * 15)`, "flat-tail": `tier("default", p * 3 + c * 15)`, "expr": "v2:0.2"},
	}
	for _, tc := range []struct{ name, mapped, source string }{
		{"flat", "tail", "model_original"},
		{"alias", "flat-tail", "model_mapped"},
	} {
		r := SelectTaskBillingRule(s, "sora", tc.name, tc.mapped, false)
		require.Equal(t, tc.source, r.Source)
		require.Equal(t, BillingModeFlatPrice, r.ExpressionBillingMode())
		require.Error(t, r.Validate(nil), "flat token prices evaluate to zero per call")
	}
	s.TaskPriorityVersion = 1
	r := SelectTaskBillingRule(s, "sora", "flat", "tail", false)
	require.Equal(t, "model_original", r.Source, "migrated sites follow the model price too")
	require.Equal(t, BillingModeFlatPrice, r.ExpressionBillingMode())
	require.Error(t, r.Validate(nil), "flat token prices evaluate to zero per call")
	s.TaskPriorityVersion = 0
	r = SelectTaskBillingRule(s, "sora", "expr", "", false)
	require.Equal(t, BillingModeTieredExpr, r.ExpressionBillingMode())
	require.NoError(t, r.Validate(nil))
	s.TaskExpressions = map[string]map[string]TaskPriceRule{"flat-tail": {"sora": {Version: 1, Kind: "amount", Expression: "v2:0.1"}}}
	r = SelectTaskBillingRule(s, "sora", "alias", "flat-tail", false)
	require.Equal(t, "plugin_mapped", r.Source)
	require.Equal(t, BillingModeTieredExpr, r.ExpressionBillingMode(), "plugin rules are per-call expressions")
	require.NoError(t, r.Validate(nil))
}
