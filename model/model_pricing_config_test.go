package model

import (
	"encoding/json"
	"errors"
	"maps"
	"os"
	"testing"

	"eflow.local/new-api/common"
	"eflow.local/new-api/internal/testdb"
	"eflow.local/new-api/pkg/billingexpr"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func pricingMigrationFixture(t *testing.T) map[string]string {
	t.Helper()
	if path := os.Getenv("PRICING_MIGRATION_SNAPSHOT"); path != "" {
		raw, err := os.ReadFile(path)
		require.NoError(t, err)
		values := map[string]string{}
		require.NoError(t, json.Unmarshal(raw, &values))
		return values
	}
	return map[string]string{
		"ModelRatio":           `{"omni":1.1,"free":0,"seconds":100,"dynamic":37.5}`,
		"CompletionRatio":      `{"omni":6.045454545455,"auxiliary-only":1.2}`,
		"AudioRatio":           `{"omni":8.181818181818}`,
		"AudioCompletionRatio": `{"omni":4}`,
		"ModelPrice":           `{"task":0.15,"request":0}`,
		"ImageRatio":           `{"embedding":2}`,
		"BatchInputRatio":      `{"embedding":0,"batch":0.125}`,
		"CacheRatio":           `{"omni":0}`, "CreateCacheRatio": `{"omni":1.25}`,
		"billing_setting.billing_mode": `{"seconds":"audio_seconds","dynamic":"tiered_expr"}`,
		"billing_setting.billing_expr": `{"orphan":"tier(\"inactive\", p * 0 + c * 0)","dynamic":"p < 100 ? tier(\"small\", p * 1 + c * 2) : tier(\"large\", p * 2 + c * 4)"}`,
		"GroupRatio":                   `{"vip":0.05}`,
	}
}

func assertPricingEqual(t *testing.T, before, after map[string]string) {
	t.Helper()
	for key := range modelPricingFields {
		original := before[key]
		if original == "" {
			original = "{}"
		}
		if key == "billing_setting.billing_expr" {
			expressions := map[string]string{}
			require.NoError(t, json.Unmarshal([]byte(original), &expressions))
			for name, expression := range expressions {
				amount, err := billingexpr.AmountExpression(expression)
				require.NoError(t, err)
				expressions[name] = amount
			}
			encoded, err := json.Marshal(expressions)
			require.NoError(t, err)
			original = string(encoded)
		}
		require.JSONEq(t, original, after[key], "changed %s", key)
	}
}

func TestModelPricingLosslessAllModels(t *testing.T) {
	before := pricingMigrationFixture(t)
	packed, err := packModelPricing(before)
	require.NoError(t, err)
	after, err := unpackModelPricing(packed)
	require.NoError(t, err)
	assertPricingEqual(t, before, after)
	again, err := packModelPricing(after)
	require.NoError(t, err)
	require.JSONEq(t, packed, again)
	names := map[string]bool{}
	for key := range modelPricingFields {
		entries := map[string]json.RawMessage{}
		if before[key] != "" {
			require.NoError(t, json.Unmarshal([]byte(before[key]), &entries))
		}
		for name := range entries {
			names[name] = true
		}
	}
	var config ModelPricingConfig
	require.NoError(t, json.Unmarshal([]byte(packed), &config))
	require.Len(t, config.Models, len(names))
	t.Logf("verified every pricing field for %d models; prices, modes, inactive expressions and units preserved", len(names))
}

func TestModelPricingDatabaseUpgradeAndRestart(t *testing.T) {
	oldDB := DB
	oldOptions := common.OptionMap
	t.Cleanup(func() { DB = oldDB; common.OptionMap = oldOptions })
	DB = testdb.OpenMySQL8(t, &Option{})
	common.OptionMap = map[string]string{}
	before := pricingMigrationFixture(t)
	require.NoError(t, saveOptionValuesWithTx(DB, before))
	require.NoError(t, migrateModelPricingFromDatabase())
	var stored Option
	require.NoError(t, DB.Where("`key` = ?", ModelPricingOption).First(&stored).Error)
	after, err := unpackModelPricing(stored.Value)
	require.NoError(t, err)
	assertPricingEqual(t, before, after)
	for key := range modelPricingFields {
		var count int64
		require.NoError(t, DB.Model(&Option{}).Where("`key` = ?", key).Count(&count).Error)
		require.Zero(t, count, key)
	}
	var group Option
	require.NoError(t, DB.Where("`key` = ?", "GroupRatio").First(&group).Error)
	require.Equal(t, before["GroupRatio"], group.Value)
	require.NoError(t, migrateModelPricingFromDatabase())
	var restarted Option
	require.NoError(t, DB.Where("`key` = ?", ModelPricingOption).First(&restarted).Error)
	require.Equal(t, stored.Value, restarted.Value)
	// A projected API write updates only its dimension, with no old DB rows.
	update := map[string]string{"BatchInputRatio": `{"new-batch":0}`}
	consolidated, err := consolidateModelPricingUpdate(DB, update)
	require.NoError(t, err)
	projected, err := unpackModelPricing(consolidated[ModelPricingOption])
	require.NoError(t, err)
	expected := map[string]string{}
	maps.Copy(expected, before)
	expected["BatchInputRatio"] = update["BatchInputRatio"]
	assertPricingEqual(t, expected, projected)
}

func TestModelPricingRejectsMalformedDocument(t *testing.T) {
	for _, raw := range []string{`{"version":3,"models":{}}`, `{"version":1,"models":{"m":{"unexpected":1}}}`, `{"version":1,"models":{"m":{"input_ratio":-1}}}`, `{"version":1,"models":{"m":{"input_ratio":null}}}`} {
		_, err := unpackModelPricing(raw)
		require.Error(t, err)
	}
}

func TestModelPricingSaveRejectsInvalidBillingModes(t *testing.T) {
	oldDB := DB
	t.Cleanup(func() { DB = oldDB })
	DB = testdb.OpenMySQL8(t, &Option{})
	for name, raw := range map[string]string{
		"unknown mode":            `{"version":1,"models":{"m":{"mode":"tierd_expr","expression":"tier(\"base\", p * 1)"}}}`,
		"expression mode no expr": `{"version":1,"models":{"m":{"mode":"flat_price","input_ratio":1}}}`,
	} {
		_, err := consolidateModelPricingUpdate(DB, map[string]string{ModelPricingOption: raw})
		require.Error(t, err, name)
	}
	_, err := consolidateModelPricingUpdate(DB, map[string]string{ModelPricingOption: `{"version":1,"models":{"m":{"mode":"flat_price","expression":"tier(\"base\", p * 1 + c * 2)"}}}`})
	require.NoError(t, err)
}

func TestGroupGroupRatioRejectsNegative(t *testing.T) {
	require.Error(t, validateConfigOption("GroupGroupRatio", `{"vip":{"default":-0.5}}`))
	require.NoError(t, validateConfigOption("GroupGroupRatio", `{"vip":{"default":0.5}}`))
}

func TestModelPricingAmountUpgradeExistingDocument(t *testing.T) {
	oldDB := DB
	t.Cleanup(func() { DB = oldDB })
	DB = testdb.OpenMySQL8(t, &Option{})
	const original = `{"version":1,"models":{"tokens":{"mode":"tiered_expr","expression":"v1:tier(\"base\", p * 2 + c * 10)"},"inactive":{"expression":"tier(\"zero\", 0)","unit_price":0},"video":{"expression":"tier(\"video\", ceil(param(\"seconds\")) * 10000)"}}}`
	require.NoError(t, saveOptionValuesWithTx(DB, map[string]string{ModelPricingOption: original, "GroupRatio": `{"vip":0.125}`, "SecretKey": "not-pricing"}))
	require.NoError(t, migrateModelPricingFromDatabase())
	var upgraded, backup Option
	require.NoError(t, DB.Where("`key` = ?", ModelPricingOption).First(&upgraded).Error)
	require.NoError(t, DB.Where("`key` = ?", ModelPricingAmountMigrationOption).First(&backup).Error)
	var record struct {
		Version  int               `json:"version"`
		Source   string            `json:"source_sha256"`
		Target   string            `json:"target_sha256"`
		Original map[string]string `json:"original"`
	}
	require.NoError(t, json.Unmarshal([]byte(backup.Value), &record))
	require.Equal(t, 2, record.Version)
	require.Equal(t, map[string]string{ModelPricingOption: original}, record.Original)
	source, err := json.Marshal(record.Original)
	require.NoError(t, err)
	require.Equal(t, billingexpr.ExprHashString(string(source)), record.Source)
	require.Equal(t, billingexpr.ExprHashString(upgraded.Value), record.Target)
	before, err := unpackModelPricing(original)
	require.NoError(t, err)
	after, err := unpackModelPricing(upgraded.Value)
	require.NoError(t, err)
	assertPricingEqual(t, before, after)
	var rules map[string]string
	require.NoError(t, json.Unmarshal([]byte(after["billing_setting.billing_expr"]), &rules))
	for name, expected := range map[string]float64{"tokens": 0.003, "inactive": 0, "video": 0.06} {
		amount, _, err := billingexpr.RunExprWithRequest(rules[name], billingexpr.TokenParams{P: 1000, C: 100}, billingexpr.RequestInput{Body: []byte(`{"seconds":5.1}`)})
		require.NoError(t, err)
		require.Equal(t, expected, amount, name)
	}
	require.NoError(t, migrateModelPricingFromDatabase())
	var restarted, sameBackup Option
	require.NoError(t, DB.Where("`key` = ?", ModelPricingOption).First(&restarted).Error)
	require.NoError(t, DB.Where("`key` = ?", ModelPricingAmountMigrationOption).First(&sameBackup).Error)
	require.Equal(t, upgraded.Value, restarted.Value)
	require.Equal(t, backup.Value, sameBackup.Value)
	var group Option
	require.NoError(t, DB.Where("`key` = ?", "GroupRatio").First(&group).Error)
	require.Equal(t, `{"vip":0.125}`, group.Value)
}

func TestModelPricingTaskRuleFormatUpgradeAndProvenance(t *testing.T) {
	oldDB := DB
	t.Cleanup(func() { DB = oldDB })
	DB = testdb.OpenMySQL8(t, &Option{})
	const original = `{"version":2,"models":{"alias":{"unit_price":0.000003,"task_expressions":{"alibaba":"v2:0"}}}}`
	require.NoError(t, saveOptionValuesWithTx(DB, map[string]string{ModelPricingOption: original}))
	require.NoError(t, migrateModelPricingFromDatabase())
	var upgraded, backup Option
	require.NoError(t, DB.Where("`key` = ?", ModelPricingOption).First(&upgraded).Error)
	require.NoError(t, DB.Where("`key` = ?", TaskRulesFormatMigrationOption).First(&backup).Error)
	var record struct {
		Version  int    `json:"version"`
		Source   string `json:"source_hash"`
		Target   string `json:"target_hash"`
		Original string `json:"original"`
	}
	require.NoError(t, json.Unmarshal([]byte(backup.Value), &record))
	require.Equal(t, 1, record.Version)
	require.Equal(t, original, record.Original)
	require.Equal(t, billingexpr.ExprHashString(original), record.Source)
	require.Equal(t, billingexpr.ExprHashString(upgraded.Value), record.Target)
	require.JSONEq(t, `{"version":2,"models":{"alias":{"unit_price":0.000003,"task_expressions":{"alibaba":{"version":1,"kind":"amount","expression":"v2:0"}}}}}`, upgraded.Value)
	require.NoError(t, migrateModelPricingFromDatabase())
	var restarted, sameBackup Option
	require.NoError(t, DB.Where("`key` = ?", ModelPricingOption).First(&restarted).Error)
	require.NoError(t, DB.Where("`key` = ?", TaskRulesFormatMigrationOption).First(&sameBackup).Error)
	require.Equal(t, upgraded.Value, restarted.Value)
	require.Equal(t, backup.Value, sameBackup.Value)
	const unproven = `{"version":2,"task_priority_version":1,"models":{}}`
	require.NoError(t, saveOptionValuesWithTx(DB, map[string]string{ModelPricingOption: unproven}))
	require.ErrorContains(t, migrateModelPricingFromDatabase(), "provenance missing or invalid")
	var unchanged Option
	require.NoError(t, DB.Where("`key` = ?", ModelPricingOption).First(&unchanged).Error)
	require.Equal(t, unproven, unchanged.Value)
}

func TestModelPricingAmountUpgradeRejectsInvalidAtomically(t *testing.T) {
	oldDB := DB
	t.Cleanup(func() { DB = oldDB })
	DB = testdb.OpenMySQL8(t, &Option{})
	const original = `{"version":1,"models":{"valid":{"expression":"p * 2"},"inactive":{"expression":"invalid("}}}`
	require.NoError(t, saveOptionValuesWithTx(DB, map[string]string{ModelPricingOption: original}))
	require.Error(t, migrateModelPricingFromDatabase())
	var unchanged Option
	require.NoError(t, DB.Where("`key` = ?", ModelPricingOption).First(&unchanged).Error)
	require.Equal(t, original, unchanged.Value)
	var count int64
	require.NoError(t, DB.Model(&Option{}).Where("`key` = ?", ModelPricingAmountMigrationOption).Count(&count).Error)
	require.Zero(t, count)
}

func TestModelPricingAmountUpgradeRollsBackBackupOnWriteFailure(t *testing.T) {
	oldDB := DB
	t.Cleanup(func() { DB = oldDB })
	DB = testdb.OpenMySQL8(t, &Option{})
	const original = `{"version":1,"models":{"m":{"expression":"p * 2"}}}`
	require.NoError(t, saveOptionValuesWithTx(DB, map[string]string{ModelPricingOption: original}))
	const callback = "test:fail-amount-migration-write"
	require.NoError(t, DB.Callback().Update().Before("gorm:update").Register(callback, func(tx *gorm.DB) {
		if option, ok := tx.Statement.Dest.(*Option); ok && option.Key == ModelPricingOption {
			tx.AddError(errors.New("injected pricing write failure"))
		}
	}))
	t.Cleanup(func() { _ = DB.Callback().Update().Remove(callback) })
	require.ErrorContains(t, migrateModelPricingFromDatabase(), "injected pricing write failure")
	var unchanged Option
	require.NoError(t, DB.Where("`key` = ?", ModelPricingOption).First(&unchanged).Error)
	require.Equal(t, original, unchanged.Value)
	var count int64
	require.NoError(t, DB.Model(&Option{}).Where("`key` = ?", ModelPricingAmountMigrationOption).Count(&count).Error)
	require.Zero(t, count)
}

func TestModelPricingAmountReMigrationKeepsEarliestProvenance(t *testing.T) {
	oldDB, oldOptions := DB, common.OptionMap
	t.Cleanup(func() { DB = oldDB; common.OptionMap = oldOptions })
	DB = testdb.OpenMySQL8(t, &Option{})
	common.OptionMap = map[string]string{}
	const first = `{"version":1,"models":{"m":{"expression":"p * 2"}}}`
	const second = `{"version":1,"models":{"m":{"expression":"p * 3"}}}`
	require.NoError(t, saveOptionValuesWithTx(DB, map[string]string{ModelPricingOption: first}))
	require.NoError(t, migrateModelPricingFromDatabase())
	var initial Option
	require.NoError(t, DB.Where("`key` = ?", ModelPricingAmountMigrationOption).First(&initial).Error)
	// Rolled back binary writes the old format again, then the upgrade reruns.
	require.NoError(t, saveOptionValuesWithTx(DB, map[string]string{ModelPricingOption: second}))
	require.NoError(t, migrateModelPricingFromDatabase())
	var current, record Option
	require.NoError(t, DB.Where("`key` = ?", ModelPricingOption).First(&current).Error)
	require.NoError(t, DB.Where("`key` = ?", ModelPricingAmountMigrationOption).First(&record).Error)
	type provenance struct {
		Source   string            `json:"source_sha256"`
		Target   string            `json:"target_sha256"`
		Original map[string]string `json:"original"`
		Latest   *provenance       `json:"latest"`
	}
	var saved, before provenance
	require.NoError(t, json.Unmarshal([]byte(initial.Value), &before))
	require.NoError(t, json.Unmarshal([]byte(record.Value), &saved))
	require.Equal(t, map[string]string{ModelPricingOption: first}, saved.Original, "the earliest original is the rollback baseline")
	require.Equal(t, before.Source, saved.Source)
	require.Equal(t, before.Target, saved.Target)
	require.NotNil(t, saved.Latest)
	require.Equal(t, map[string]string{ModelPricingOption: second}, saved.Latest.Original)
	require.Equal(t, billingexpr.ExprHashString(current.Value), saved.Latest.Target)
	// Restart: nothing to convert, record unchanged.
	require.NoError(t, migrateModelPricingFromDatabase())
	var restarted Option
	require.NoError(t, DB.Where("`key` = ?", ModelPricingAmountMigrationOption).First(&restarted).Error)
	require.Equal(t, record.Value, restarted.Value)
	// Provenance is never writable through the options API.
	for _, key := range []string{ModelPricingAmountMigrationOption, TaskRulesFormatMigrationOption, TaskPricingMigrationOption} {
		require.ErrorContains(t, UpdateOption(key, `{}`), "read-only", key)
	}
	require.NoError(t, DB.Where("`key` = ?", ModelPricingAmountMigrationOption).First(&restarted).Error)
	require.Equal(t, record.Value, restarted.Value)
}
