package taskplugin

import (
	"context"
	"eflow.local/new-api/setting/config"
	"eflow.local/new-api/setting/ratio_setting"
	"encoding/json"
	"errors"
	"gorm.io/gorm"
	"testing"

	"eflow.local/new-api/common"

	"eflow.local/new-api/internal/testdb"
	"eflow.local/new-api/model"
	host "eflow.local/new-api/relay/channel/task/jsplugin"
	"eflow.local/new-api/setting/billing_setting"
	"github.com/stretchr/testify/require"
)

func TestTaskPriceMigrationOfflineAndConflicts(t *testing.T) {
	m := NewManager(nil, factorySources()) // Never Start/Refresh: paused fixed versions still resolve.
	source := m.factories["happyhorse"]
	h, err := host.Compile(source)
	require.NoError(t, err)
	mapping := `{"alias":"HAPPYHORSE-1.0-T2V"}`
	channels := []model.Channel{{Id: 1, Type: model.TaskPluginChannelType("happyhorse"), Status: common.ChannelStatusManuallyDisabled, Models: "alias", ModelMapping: &mapping}}
	state := model.TaskPluginSnapshot{Production: []model.TaskPluginProductionBinding{{ChannelID: 1, Key: "happyhorse", Version: h.Identity().Version, SourceHash: h.Identity().SourceHash, Factory: true}}}
	var doc model.ModelPricingConfig
	require.NoError(t, json.Unmarshal([]byte(`{"version":2,"models":{"alias":{"unit_price":0.000003}}}`), &doc))
	report, err := m.PlanPricingMigration(context.Background(), doc, channels, state, nil, false)
	require.NoError(t, err)
	require.False(t, report.Blocked)
	require.Len(t, report.Entries, 1)
	r := report.Entries[0].Rule
	require.Equal(t, "unit_price", r.Kind)
	require.Equal(t, "happyhorse-1.0-t2v", r.UpstreamModel)
	require.Equal(t, "follows_model_price", report.Entries[0].Status)
	require.Equal(t, "original_unit_price", report.Entries[0].Reason)
	require.NotContains(t, doc.Models["alias"], "task_expressions")
	var candidate model.ModelPricingConfig
	require.NoError(t, json.Unmarshal([]byte(report.Candidate), &candidate))
	require.Equal(t, 1, candidate.TaskPriorityVersion)
	require.Equal(t, doc.Models, candidate.Models, "the model price is followed, not copied into a task rule")
	// Same public name, another bound tail: no random choice or partial candidate.
	second := channels[0]
	second.Id = 2
	other := `{"alias":"happyhorse-1.0-i2v"}`
	second.ModelMapping = &other
	b := state.Production[0]
	b.ChannelID = 2
	state.Production = append(state.Production, b)
	// Same public name, another bound tail: both follow the model price, which is
	// resolved live per channel, so nothing conflicts and nothing is stored.
	report, err = m.PlanPricingMigration(context.Background(), doc, append(channels, second), state, nil, false)
	require.NoError(t, err)
	require.False(t, report.Blocked)
	for _, e := range report.Entries {
		require.Equal(t, "follows_model_price", e.Status)
	}
	require.NoError(t, json.Unmarshal([]byte(report.Candidate), &candidate))
	require.Equal(t, doc.Models, candidate.Models)
	// A stored mapped-model rule is still one value per name, so two tails that
	// need different stored rules remain a real conflict.
	var mappedDoc model.ModelPricingConfig
	require.NoError(t, json.Unmarshal([]byte(`{"version":2,"models":{"happyhorse-1.0-t2v":{"unit_price":0.000003},"happyhorse-1.0-i2v":{"unit_price":0.000005}}}`), &mappedDoc))
	report, err = m.PlanPricingMigration(context.Background(), mappedDoc, append(channels, second), state, nil, false)
	require.NoError(t, err)
	require.True(t, report.Blocked)
	require.Empty(t, report.Candidate)
	state.Production = state.Production[:1]
	state.Production[0].SourceHash = "changed"
	report, err = m.PlanPricingMigration(context.Background(), doc, channels, state, nil, false)
	require.NoError(t, err)
	require.True(t, report.Blocked)
}

func TestTaskPriceMigrationInventoryMySQL(t *testing.T) {
	savedConfig := config.GlobalConfig.ExportAllConfigs()
	savedPrices, savedRatios := ratio_setting.ModelPrice2JSONString(), ratio_setting.ModelRatio2JSONString()
	t.Cleanup(func() {
		require.NoError(t, config.GlobalConfig.LoadFromDB(savedConfig))
		require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(savedPrices))
		require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(savedRatios))
	})
	oldDB, oldResolver, oldOptions := model.DB, model.PlanTaskPricingMigration, common.OptionMap
	t.Cleanup(func() { model.DB = oldDB; model.PlanTaskPricingMigration = oldResolver; common.OptionMap = oldOptions })
	db := testdb.OpenMySQL8(t, &model.Option{}, &model.Channel{}, &model.TaskPluginControl{}, &model.TaskPlugin{}, &model.TaskPluginProductionBinding{}, &model.TaskPluginShadowBinding{}, &model.TaskPluginSourceSnapshot{})
	model.DB = db
	common.OptionMap = map[string]string{}
	m := NewManager(nil, factorySources())
	model.PlanTaskPricingMigration = m.PlanPricingMigration
	require.NoError(t, db.Create(&model.TaskPluginControl{Id: 1, DisabledFactory: "[]"}).Error)
	h, err := host.Compile(m.factories["happyhorse"])
	require.NoError(t, err)
	mapping := `{"alias":"happyhorse-1.0-t2v"}`
	require.NoError(t, db.Create(&model.Channel{Id: 1, Type: model.TaskPluginChannelType("happyhorse"), Status: common.ChannelStatusManuallyDisabled, Models: "alias", ModelMapping: &mapping}).Error)
	require.NoError(t, db.Create(&model.TaskPluginProductionBinding{ChannelID: 1, Key: "happyhorse", Version: h.Identity().Version, SourceHash: h.Identity().SourceHash, Factory: true}).Error)
	raw := `{"version":2,"models":{"alias":{"unit_price":0.000003},"orphan":{"unit_price":0}}}`
	require.NoError(t, db.Create(&model.Option{Key: model.ModelPricingOption, Value: raw}).Error)
	ctx := context.Background()
	preview, err := model.TaskPricingMigration(ctx, "", false)
	require.NoError(t, err)
	require.False(t, preview.Blocked)
	require.Contains(t, preview.Entries, model.TaskPricingMigrationEntry{Model: "orphan", Status: "preserved_unreferenced", Reason: "price_preserved_follows_model_on_activation"})
	var count int64
	_, err = model.TaskPricingMigration(ctx, "0000000000000000000000000000000000000000000000000000000000000000", true)
	require.Error(t, err)
	// A provenance write failure rolls back the candidate too.
	const callback = "test:task-migration-backup-failure"
	require.NoError(t, db.Callback().Create().Before("gorm:create").Register(callback, func(tx *gorm.DB) {
		if option, ok := tx.Statement.Dest.(*model.Option); ok && option.Key == model.TaskPricingMigrationOption {
			tx.AddError(errors.New("injected task provenance failure"))
		}
	}))
	_, err = model.TaskPricingMigration(ctx, preview.SourceHash, true)
	require.ErrorContains(t, err, "injected task provenance failure")
	require.NoError(t, db.Callback().Create().Remove(callback))
	var unchanged model.Option
	require.NoError(t, db.Where("`key` = ?", model.ModelPricingOption).First(&unchanged).Error)
	require.Equal(t, raw, unchanged.Value)
	require.NoError(t, db.Model(&model.Option{}).Where("`key` = ?", model.TaskPricingMigrationOption).Count(&count).Error)
	require.Zero(t, count)
	// An actual group edit invalidates the previous preview.
	require.NoError(t, db.Create(&model.Option{Key: "GroupRatio", Value: `{"default":0.3}`}).Error)
	_, err = model.TaskPricingMigration(ctx, preview.SourceHash, true)
	require.ErrorContains(t, err, "stale")
	preview, err = model.TaskPricingMigration(ctx, "", false)
	require.NoError(t, err)
	applied, err := model.TaskPricingMigration(ctx, preview.SourceHash, true)
	require.NoError(t, err)
	require.False(t, applied.AlreadyApplied)
	rule := billing_setting.ResolveTaskBillingRule("happyhorse", "alias", "happyhorse-1.0-t2v")
	require.Equal(t, "legacy_original", rule.Source, "migrated tasks follow the live model price")
	require.Nil(t, rule.Rule)
	var first model.Option
	require.NoError(t, db.Where("`key` = ?", model.ModelPricingOption).First(&first).Error)
	// A normal price edit after migration must survive repeat application.
	var edited model.ModelPricingConfig
	require.NoError(t, json.Unmarshal([]byte(first.Value), &edited))
	edited.Models["alias"]["unit_price"] = json.RawMessage(`0.2`)
	afterEdit, err := json.Marshal(edited)
	require.NoError(t, err)
	require.NoError(t, db.Model(&model.Option{}).Where("`key` = ?", model.ModelPricingOption).Update("value", string(afterEdit)).Error)
	first.Value = string(afterEdit)
	again, err := model.TaskPricingMigration(ctx, preview.SourceHash, true)
	require.NoError(t, err)
	require.True(t, again.AlreadyApplied)
	var second model.Option
	require.NoError(t, db.Where("`key` = ?", model.ModelPricingOption).First(&second).Error)
	require.Equal(t, first.Value, second.Value)
	// New candidate contains no channel key (the query selects no credentials).
	require.NotContains(t, preview.Candidate, "api_key")

}

func TestTaskPriceMigrationOrphanPluginOverride(t *testing.T) {
	m := NewManager(nil, factorySources())
	var doc model.ModelPricingConfig
	require.NoError(t, json.Unmarshal([]byte(`{"version":2,"models":{"alias":{"unit_price":0.1,"task_expressions":{"sora":"v2:0"}}}}`), &doc))
	channels := []model.Channel{{Id: 1, Type: 1, Models: "alias"}}
	report, err := m.PlanPricingMigration(context.Background(), doc, channels, model.TaskPluginSnapshot{}, nil, false)
	require.NoError(t, err)
	require.True(t, report.Blocked, "a normal channel cannot cover an orphan plugin price")
	require.Empty(t, report.Candidate)
	require.Contains(t, report.Entries, model.TaskPricingMigrationEntry{Model: "alias", Plugin: "sora", Status: "blocked", Reason: "orphan_pricing_requires_disposition"})
	// Another plugin's valid binding does not cover the Sora override either.
	h, err := host.Compile(m.factories["happyhorse"])
	require.NoError(t, err)
	mapping := `{"alias":"happyhorse-1.0-t2v"}`
	channels[0].Type, channels[0].ModelMapping = model.TaskPluginChannelType("happyhorse"), &mapping
	state := model.TaskPluginSnapshot{Production: []model.TaskPluginProductionBinding{{ChannelID: 1, Key: "happyhorse", Version: h.Identity().Version, SourceHash: h.Identity().SourceHash, Factory: true}}}
	report, err = m.PlanPricingMigration(context.Background(), doc, channels, state, nil, false)
	require.NoError(t, err)
	require.True(t, report.Blocked)
	require.Empty(t, report.Candidate)
	require.Contains(t, report.Entries, model.TaskPricingMigrationEntry{Model: "alias", Plugin: "sora", Status: "blocked", Reason: "orphan_pricing_requires_disposition"})
}

func TestTaskPriceMigrationAllProvidersAndSources(t *testing.T) {
	m := NewManager(nil, factorySources())
	for key, source := range m.factories {
		t.Run(key, func(t *testing.T) {
			h, err := host.Compile(source)
			require.NoError(t, err)
			tail := h.Meta().Models[0]
			for _, record := range []string{`{"unit_price":0.000003}`, `{"unit_price":0}`, `{"unit_price":0.2,"input_ratio":4}`} {
				var fields map[string]json.RawMessage
				require.NoError(t, json.Unmarshal([]byte(record), &fields))
				doc := model.ModelPricingConfig{Version: 2, Models: map[string]map[string]json.RawMessage{"alias": fields}}
				mapping, err := json.Marshal(map[string]string{"alias": tail})
				require.NoError(t, err)
				mappingText := string(mapping)
				channels := []model.Channel{{Id: 1, Type: model.TaskPluginChannelType(key), Models: "alias", ModelMapping: &mappingText}}
				state := model.TaskPluginSnapshot{Production: []model.TaskPluginProductionBinding{{ChannelID: 1, Key: key, Version: h.Identity().Version, SourceHash: h.Identity().SourceHash, Factory: true}}}
				// Historical fixed source survives a newer bundled factory.
				original := m.factories[key]
				m.factories[key] = original + "\n// new factory build"
				report, err := m.PlanPricingMigration(context.Background(), doc, channels, state, []model.TaskPluginSourceSnapshot{{Key: key, Version: h.Identity().Version, SourceHash: h.Identity().SourceHash, Source: original}}, false)
				m.factories[key] = original
				require.NoError(t, err)
				require.False(t, report.Blocked)
				require.Equal(t, "unit_price", report.Entries[0].Rule.Kind)
				for _, group := range []float64{0, 0.3, 1, 2} {
					r := report.Entries[0].Rule
					got, err := r.BaseQuota(500000, group)
					require.NoError(t, err)
					want, err := common.QuotaFromFloatStrict(*r.UnitPrice * 500000 * group)
					require.NoError(t, err)
					require.Equal(t, want, got)
				}
			}
		})
	}
}

func TestTaskPriceMigrationFallbackPolicies(t *testing.T) {
	var doc model.ModelPricingConfig
	require.NoError(t, json.Unmarshal([]byte(`{"version":2,"models":{"tail":{"task_expressions":{"sora":"v2:0.25"}}}}`), &doc))
	rule, source, err := legacyTaskPriceRule(doc, "new-alias", "sora", "tail", false)
	require.NoError(t, err)
	require.Equal(t, "existing_plugin_mapped", source)
	require.Equal(t, "v2:0.25", rule.Expression)
	rule, source, err = legacyTaskPriceRule(doc, "new-alias", "sora", "tail", true)
	require.NoError(t, err)
	require.Equal(t, "self_use_token_ratio", source)
	require.Equal(t, 37.5, *rule.ModelRatio)
	require.Equal(t, "tokens", rule.Settlement)
}

func TestTaskPriceMigrationConvertsCopiedOverridesToFollow(t *testing.T) {
	m := NewManager(nil, factorySources())
	h, err := host.Compile(m.factories["happyhorse"])
	require.NoError(t, err)
	mapping := `{"alias":"happyhorse-1.0-t2v","tail-alias":"happyhorse-1.0-t2v"}`
	channels := []model.Channel{{Id: 1, Type: model.TaskPluginChannelType("happyhorse"), Models: "alias,tail-alias", ModelMapping: &mapping}}
	state := model.TaskPluginSnapshot{Production: []model.TaskPluginProductionBinding{{ChannelID: 1, Key: "happyhorse", Version: h.Identity().Version, SourceHash: h.Identity().SourceHash, Factory: true}}}
	var base model.ModelPricingConfig
	require.NoError(t, json.Unmarshal([]byte(`{"version":2,"models":{"alias":{"unit_price":0.000003}}}`), &base))
	copied, source, err := modelTaskPriceRule(base, "alias", "happyhorse", "happyhorse-1.0-t2v", false)
	require.NoError(t, err)
	require.Equal(t, "original_unit_price", source)
	plan := func(rule billing_setting.TaskPriceRule) (model.TaskPricingMigrationReport, model.ModelPricingConfig) {
		t.Helper()
		raw, err := json.Marshal(map[string]billing_setting.TaskPriceRule{"happyhorse": rule})
		require.NoError(t, err)
		doc := model.ModelPricingConfig{Version: 2, Models: map[string]map[string]json.RawMessage{
			"alias": {"unit_price": json.RawMessage(`0.000003`), "task_expressions": raw},
		}}
		doc.Models["tail-alias"] = map[string]json.RawMessage{"task_expressions": raw}
		report, err := m.PlanPricingMigration(context.Background(), doc, channels, state, nil, false)
		require.NoError(t, err)
		require.False(t, report.Blocked)
		var candidate model.ModelPricingConfig
		require.NoError(t, json.Unmarshal([]byte(report.Candidate), &candidate))
		return report, candidate
	}
	entry := func(report model.TaskPricingMigrationReport, name string) model.TaskPricingMigrationEntry {
		for _, e := range report.Entries {
			if e.Model == name && e.Plugin == "happyhorse" {
				return e
			}
		}
		t.Fatalf("no entry for %s", name)
		return model.TaskPricingMigrationEntry{}
	}
	// A copy identical to the model-derived rule is removed; the model price is followed.
	report, candidate := plan(copied)
	require.Equal(t, "converted_to_follow", entry(report, "alias").Status)
	require.Equal(t, "original_unit_price", entry(report, "alias").Reason)
	require.NotContains(t, candidate.Models["alias"], "task_expressions")
	require.Equal(t, json.RawMessage(`0.000003`), candidate.Models["alias"]["unit_price"])
	// Without its own price there is nothing to follow: the explicit rule is kept.
	require.Equal(t, "ready", entry(report, "tail-alias").Status)
	require.Equal(t, "existing_plugin_original", entry(report, "tail-alias").Reason)
	require.Contains(t, candidate.Models["tail-alias"], "task_expressions")
	// A deliberately different override is kept and flagged for review.
	different := copied
	price := 0.000009
	different.UnitPrice = &price
	report, candidate = plan(different)
	require.Equal(t, "override_differs_from_model", entry(report, "alias").Status)
	require.Equal(t, "existing_plugin_original", entry(report, "alias").Reason)
	var kept map[string]billing_setting.TaskPriceRule
	require.NoError(t, json.Unmarshal(candidate.Models["alias"]["task_expressions"], &kept))
	require.Equal(t, price, *kept["happyhorse"].UnitPrice)
}
