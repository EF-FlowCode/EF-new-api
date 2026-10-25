package model

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"eflow.local/new-api/common"
	"eflow.local/new-api/internal/testdb"
	"eflow.local/new-api/pkg/jsplugin"
	"eflow.local/new-api/setting/billing_setting"
	"github.com/stretchr/testify/require"
)

func TestUsagePricingSaveValidatesChangedRulesWithoutBlockingPausedReads(t *testing.T) {
	oldDB, oldResolver, oldOptions := DB, ResolveBillingUsageBindings, common.OptionMap
	t.Cleanup(func() { DB = oldDB; ResolveBillingUsageBindings = oldResolver; common.OptionMap = oldOptions })
	DB = testdb.OpenMySQL8(t, &Option{}, &Channel{})
	common.OptionMap = map[string]string{}
	require.NoError(t, saveOptionValuesWithTx(DB, map[string]string{ModelPricingOption: `{"version":2,"models":{}}`}))
	require.NoError(t, DB.Create(&Channel{Id: 58, Type: 78, Status: common.ChannelStatusEnabled, Models: "video", Key: "never-selected"}).Error)
	status, calls := "ready", 0
	ResolveBillingUsageBindings = func(ctx context.Context, channels []Channel) (map[string]BillingUsageInfo, error) {
		calls++
		require.Len(t, channels, 1)
		require.Empty(t, channels[0].Key)
		return map[string]BillingUsageInfo{"video": {Status: status, Schema: map[string]jsplugin.UsageFieldSchema{"seconds": {Type: "number", Unit: "second"}}}}, nil
	}
	document := func(expression string) string {
		raw, err := json.Marshal(map[string]any{"version": 2, "models": map[string]any{"video": map[string]any{"mode": "tiered_expr", "expression": expression}}})
		require.NoError(t, err)
		return string(raw)
	}
	for _, invalid := range []string{`v2:u("unknown")`, `v2:false ? u("unknown") : 0`, `v2:u(param("key"))`} {
		_, err := consolidateModelPricingUpdate(DB, map[string]string{ModelPricingOption: document(invalid)})
		require.Error(t, err)
	}
	valid := document(`v2:u("seconds") * 0.01`)
	updates, err := consolidateModelPricingUpdate(DB, map[string]string{ModelPricingOption: valid})
	require.NoError(t, err)
	require.NoError(t, saveOptionValuesWithTx(DB, updates))
	require.Positive(t, calls)
	status, calls = "unavailable", 0
	_, err = unpackModelPricing(valid)
	require.NoError(t, err)
	_, err = consolidateModelPricingUpdate(DB, map[string]string{ModelPricingOption: valid})
	require.NoError(t, err)
	_, err = consolidateModelPricingUpdate(DB, map[string]string{"ModelPrice": `{"other":0.1}`})
	require.NoError(t, err)
	require.Zero(t, calls, "unrelated saves and startup must not require live bindings")
	_, err = consolidateModelPricingUpdate(DB, map[string]string{ModelPricingOption: document(`v2:u("seconds") * 0.02`)})
	require.ErrorContains(t, err, "no unambiguous")
	status = "conflict"
	_, err = consolidateModelPricingUpdate(DB, map[string]string{ModelPricingOption: document(`v2:u("seconds") * 0.02`)})
	require.Error(t, err)
	// A dormant rule may be read while paused, but enabling it requires a live schema.
	dormant := strings.Replace(valid, `"tiered_expr"`, `"ratio"`, 1)
	_, err = consolidateModelPricingUpdate(DB, map[string]string{ModelPricingOption: dormant})
	require.NoError(t, err)
	require.NoError(t, saveOptionValuesWithTx(DB, map[string]string{ModelPricingOption: dormant}))
	_, err = consolidateModelPricingUpdate(DB, map[string]string{"billing_setting.billing_mode": `{"video":"tiered_expr"}`})
	require.Error(t, err)
	status = "ready"
	_, err = consolidateModelPricingUpdate(DB, map[string]string{"billing_setting.billing_mode": `{"video":"tiered_expr"}`})
	require.NoError(t, err)
	_, err = consolidateModelPricingUpdate(DB, map[string]string{ModelPricingOption: strings.Replace(valid, `"tiered_expr"`, `"flat_price"`, 1)})
	require.Error(t, err, "fixed token mode must not accept task usage")
}

func TestPublicPricingStripsPluginInternalsWithoutMutatingCache(t *testing.T) {
	source := []BillingUsageSource{{Plugin: "sora", Version: "1", SourceHash: "hash", Model: "upstream-tail"}}
	cached := Pricing{
		ModelName:   "alias",
		TaskPricing: map[string][]billing_setting.TaskBillingRule{"sora": {{Source: "plugin_mapped", Plugin: "sora", Model: "upstream-tail", MappedModel: "upstream-tail", Expression: "v2:0.1"}}},
		BillingUsage: &BillingUsageInfo{Status: "ready", Sources: source, Plugins: map[string]BillingUsageInfo{
			"sora": {Status: "ready", Schema: map[string]jsplugin.UsageFieldSchema{"seconds": {Type: "number"}}, Sources: source},
		}},
	}
	public := PublicPricing(cached)
	require.Nil(t, public.TaskPricing)
	require.Nil(t, public.BillingUsage.Sources)
	require.Nil(t, public.BillingUsage.Plugins["sora"].Sources)
	require.Equal(t, "ready", public.BillingUsage.Plugins["sora"].Status)
	require.Contains(t, public.BillingUsage.Plugins["sora"].Schema, "seconds")
	raw, err := common.Marshal(public)
	require.NoError(t, err)
	for _, secret := range []string{"source_hash", "upstream-tail", "task_pricing", "v2:0.1"} {
		require.NotContains(t, string(raw), secret)
	}
	require.NotNil(t, cached.TaskPricing, "the shared cache entry keeps the Root view")
	require.Equal(t, source, cached.BillingUsage.Sources)
	require.Equal(t, source, cached.BillingUsage.Plugins["sora"].Sources)
}

func TestExpressionBillingModeRequiresExpressionAcrossBatches(t *testing.T) {
	oldDB, oldOptions := DB, common.OptionMap
	t.Cleanup(func() { DB = oldDB; common.OptionMap = oldOptions })
	DB = testdb.OpenMySQL8(t, &Option{}, &Channel{})
	common.OptionMap = map[string]string{}
	require.NoError(t, saveOptionValuesWithTx(DB, map[string]string{ModelPricingOption: `{"version":2,"models":{"video":{"mode":"tiered_expr","expression":"v2:0.1"}}}`}))
	for name, updates := range map[string]map[string]string{
		"same batch empty":      {"billing_setting.billing_mode": `{"m":"tiered_expr"}`, "billing_setting.billing_expr": `{"m":"  "}`},
		"same batch flat":       {"billing_setting.billing_mode": `{"m":"flat_price"}`, "billing_setting.billing_expr": `{}`},
		"expr cleared later":    {"billing_setting.billing_expr": `{"video":""}`},
		"expr removed later":    {"billing_setting.billing_expr": `{}`},
		"mode without stored":   {"billing_setting.billing_mode": `{"video":"tiered_expr","m":"tiered_expr"}`},
		"document without expr": {ModelPricingOption: `{"version":2,"models":{"m":{"mode":"tiered_expr"}}}`},
		"document blank expr":   {ModelPricingOption: `{"version":2,"models":{"m":{"mode":"flat_price","expression":" "}}}`},
	} {
		require.NoError(t, validateOptionValues(updates), name)
		_, err := consolidateModelPricingUpdate(DB, updates)
		require.ErrorContains(t, err, "has no expression", name)
	}
	_, err := consolidateModelPricingUpdate(DB, map[string]string{"billing_setting.billing_mode": `{"video":"ratio"}`, "billing_setting.billing_expr": `{"video":""}`})
	require.NoError(t, err, "a ratio model may keep an empty expression")
}
