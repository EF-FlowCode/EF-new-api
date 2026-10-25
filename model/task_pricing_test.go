package model

import (
	"context"
	"eflow.local/new-api/common"
	"eflow.local/new-api/internal/testdb"
	"eflow.local/new-api/pkg/jsplugin"
	"eflow.local/new-api/setting/billing_setting"
	"encoding/json"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestTaskPricingSingleStorageAndScopedValidationMySQL(t *testing.T) {
	oldDB, oldResolver, oldOptions := DB, ResolveBillingUsageBindings, common.OptionMap
	t.Cleanup(func() { DB = oldDB; ResolveBillingUsageBindings = oldResolver; common.OptionMap = oldOptions })
	DB = testdb.OpenMySQL8(t, &Option{}, &Channel{})
	common.OptionMap = map[string]string{}
	require.NoError(t, saveOptionValuesWithTx(DB, map[string]string{ModelPricingOption: `{"version":2,"models":{"alias":{"unit_price":0.1},"orphan":{"expression":"v2:0"}}}`}))
	status, calls := "ready", 0
	unresolved := false
	ResolveBillingUsageBindings = func(context.Context, []Channel) (map[string]BillingUsageInfo, error) {
		calls++
		return map[string]BillingUsageInfo{"alias": {Status: "conflict", Plugins: map[string]BillingUsageInfo{
			"alibaba": {Status: status, UnresolvedTargets: unresolved, Schema: map[string]jsplugin.UsageFieldSchema{"seconds": {Type: "number", Unit: "second"}}, Sources: []BillingUsageSource{{Plugin: "alibaba", Model: "wan3.0-video"}}},
			"sora":    {Status: "ready", Schema: map[string]jsplugin.UsageFieldSchema{"seconds": {Type: "boolean"}}, Sources: []BillingUsageSource{{Plugin: "sora", Model: "sora-2"}}},
		}}}, nil
	}
	valid := `{"alias":{"alibaba":"v2:u(\"seconds\") * 0.01"},"wan3.0-video":{"alibaba":"v2:0.02"}}`
	updates, err := consolidateModelPricingUpdate(DB, map[string]string{billing_setting.TaskExpressionsOption: valid})
	require.NoError(t, err)
	require.Len(t, updates, 1)
	require.Contains(t, updates, ModelPricingOption)
	require.NoError(t, saveOptionValuesWithTx(DB, updates))
	projected, err := unpackModelPricing(updates[ModelPricingOption])
	require.NoError(t, err)
	var normalized map[string]map[string]billing_setting.TaskPriceRule
	require.NoError(t, json.Unmarshal([]byte(valid), &normalized))
	normalizedJSON, err := json.Marshal(normalized)
	require.NoError(t, err)
	require.JSONEq(t, string(normalizedJSON), projected[billing_setting.TaskExpressionsOption])
	require.JSONEq(t, `{"alias":0.1}`, projected["ModelPrice"])
	require.JSONEq(t, `{"orphan":"v2:0"}`, projected["billing_setting.billing_expr"])
	repacked, err := packModelPricing(projected)
	require.NoError(t, err)
	require.Equal(t, updates[ModelPricingOption], repacked)
	for _, invalid := range []string{`{"alias":{"sora":"v2:u(\"seconds\") * 0.1"}}`, `{"absent":{"alibaba":"v2:0.1"}}`, `{"alias":{"alibaba":"v2:u(\"unknown\")"}}`} {
		_, err = consolidateModelPricingUpdate(DB, map[string]string{billing_setting.TaskExpressionsOption: invalid})
		require.Error(t, err)
	}
	unresolved = true
	_, err = consolidateModelPricingUpdate(DB, map[string]string{billing_setting.TaskExpressionsOption: `{"wan3.0-video":{"alibaba":"v2:0.3"}}`})
	require.Error(t, err, "unknown binding targets must not be hidden by known sources")
	unresolved = false
	require.NoError(t, migrateModelPricingFromDatabase())
	require.NoError(t, migrateModelPricingFromDatabase())
	var persisted Option
	require.NoError(t, DB.Where("`key` = ?", ModelPricingOption).First(&persisted).Error)
	require.JSONEq(t, repacked, persisted.Value)
	var count int64
	require.NoError(t, DB.Model(&Option{}).Where("`key` = ?", billing_setting.TaskExpressionsOption).Count(&count).Error)
	require.Zero(t, count, "plugin rules cannot become a second persistent option")
	status = "conflict"
	_, err = consolidateModelPricingUpdate(DB, map[string]string{billing_setting.TaskExpressionsOption: `{"wan3.0-video":{"alibaba":"v2:0.3"}}`})
	require.Error(t, err, "mapped rules must reject conflicting profiles too")
	status, calls = "unavailable", 0
	_, err = consolidateModelPricingUpdate(DB, map[string]string{billing_setting.TaskExpressionsOption: valid})
	require.NoError(t, err)
	_, err = consolidateModelPricingUpdate(DB, map[string]string{"ModelPrice": `{"other":0.3}`})
	require.NoError(t, err)
	require.Zero(t, calls)
	_, err = consolidateModelPricingUpdate(DB, map[string]string{billing_setting.TaskExpressionsOption: `{"alias":{"alibaba":"v2:0.3"}}`})
	require.Error(t, err)
	updates, err = consolidateModelPricingUpdate(DB, map[string]string{billing_setting.TaskExpressionsOption: `{}`})
	require.NoError(t, err)
	var doc ModelPricingConfig
	require.NoError(t, json.Unmarshal([]byte(updates[ModelPricingOption]), &doc))
	require.NotContains(t, doc.Models["alias"], "task_expressions")
}

func TestSelectConfiguredTaskRuleFollowsModelPrice(t *testing.T) {
	var document ModelPricingConfig
	require.NoError(t, json.Unmarshal([]byte(`{"version":2,"task_priority_version":1,"models":{
		"priced":{"unit_price":0.1},"ratio":{"input_ratio":2},"expr":{"mode":"tiered_expr","expression":"v2:0.3"},
		"flat":{"mode":"flat_price","expression":"tier(\"default\", p * 3)"},"tail":{"mode":"tiered_expr","expression":"v2:0.4"},
		"override":{"unit_price":0.1,"task_expressions":{"sora":"v2:0.5"}}}}`), &document))
	for _, tc := range []struct{ name, source, mode string }{
		{"priced", "legacy_original", ""},
		{"ratio", "legacy_original", ""},
		{"expr", "model_original", billing_setting.BillingModeTieredExpr},
		{"flat", "model_original", billing_setting.BillingModeFlatPrice},
		{"override", "plugin_original", ""},
		{"alias", "explicit_task_rule_required", ""},
	} {
		rule, err := SelectConfiguredTaskRule(document, "sora", tc.name, "tail", false)
		require.NoError(t, err)
		require.Equal(t, tc.source, rule.Source, tc.name)
		require.Equal(t, tc.mode, rule.BillingMode, tc.name)
	}
	rule, err := SelectConfiguredTaskRule(document, "sora", "flat", "tail", false)
	require.NoError(t, err)
	require.Error(t, rule.Validate(nil), "flat token pricing cannot price a task")
	rule, err = SelectConfiguredTaskRule(document, "sora", "alias", "tail", true)
	require.NoError(t, err)
	require.Equal(t, "legacy_original", rule.Source, "self-use mode prices every model")
	document.TaskPriorityVersion = 0
	rule, err = SelectConfiguredTaskRule(document, "sora", "alias", "tail", false)
	require.NoError(t, err)
	require.Equal(t, "model_mapped", rule.Source)
}
