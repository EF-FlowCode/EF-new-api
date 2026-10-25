package taskplugin

import (
	"encoding/json"
	"testing"

	"eflow.local/new-api/common"
	"eflow.local/new-api/internal/testdb"
	"eflow.local/new-api/model"
	host "eflow.local/new-api/relay/channel/task/jsplugin"
	"github.com/stretchr/testify/require"
)

func TestTaskPricingUnboundInventoryPreservesPrices(t *testing.T) {
	m := NewManager(nil, factorySources())
	var document model.ModelPricingConfig
	require.NoError(t, json.Unmarshal([]byte(`{"version":2,"models":{"chat":{"input_ratio":0},"dormant":{"unit_price":0.123},"unknown-video":{"unit_price":0.456}}}`), &document))
	channels := []model.Channel{{Id: 1, Type: 1, Models: "chat,unknown-video"}}
	report, err := m.PlanPricingMigration(t.Context(), document, channels, model.TaskPluginSnapshot{}, nil, false)
	require.NoError(t, err)
	require.False(t, report.Blocked)
	var after model.ModelPricingConfig
	require.NoError(t, json.Unmarshal([]byte(report.Candidate), &after))
	require.Equal(t, document.Models, after.Models)
	require.Equal(t, 1, after.TaskPriorityVersion)
	for _, e := range report.Entries {
		require.Contains(t, []string{"preserved_unbound", "preserved_unreferenced"}, e.Status)
	}
	channels[0].Models = "alias"
	mapping := `{"alias":"SORA-2"}`
	channels[0].ModelMapping = &mapping
	report, err = m.PlanPricingMigration(t.Context(), document, channels, model.TaskPluginSnapshot{}, nil, false)
	require.NoError(t, err)
	require.True(t, report.Blocked)
	require.Contains(t, report.Entries, model.TaskPricingMigrationEntry{Model: "alias", ChannelID: 1, Status: "blocked", Reason: "declared_task_without_fixed_binding"})
}

func TestTaskPricingConfigurationGuardsMySQL(t *testing.T) {
	oldDB, oldValidator := model.DB, model.ValidateTaskPricingTargets
	t.Cleanup(func() { model.DB, model.ValidateTaskPricingTargets = oldDB, oldValidator })
	m := NewManager(nil, factorySources())
	model.ValidateTaskPricingTargets = m.ValidatePricingTargets
	db := testdb.OpenMySQL8(t, &model.Option{}, &model.Channel{}, &model.TaskPluginControl{}, &model.TaskPlugin{}, &model.TaskPluginProductionBinding{}, &model.TaskPluginShadowBinding{}, &model.TaskPluginSourceSnapshot{})
	model.DB = db
	require.NoError(t, db.Create(&model.TaskPluginControl{Id: 1, Enabled: true, FactoryEnabled: true, DisabledFactory: "[]"}).Error)
	mapping := `{"alias":"happyhorse-1.0-t2v"}`
	channel := model.Channel{Id: 1, Type: model.TaskPluginChannelType("happyhorse"), Models: "alias,chat", ModelMapping: &mapping, Status: common.ChannelStatusManuallyDisabled}
	require.NoError(t, db.Create(&channel).Error)
	h, err := host.Compile(m.factories["happyhorse"])
	require.NoError(t, err)
	b := model.TaskPluginProductionBinding{ChannelID: 1, Key: "happyhorse", Version: h.Identity().Version, SourceHash: h.Identity().SourceHash, Factory: true, Enabled: true}
	require.NoError(t, db.Create(&b).Error)
	// Without its own price or an explicit mapped plugin rule, the alias cannot be enabled.
	raw := `{"version":2,"task_priority_version":1,"models":{"other":{"unit_price":0.1}}}`
	require.NoError(t, db.Create(&model.Option{Key: model.ModelPricingOption, Value: raw}).Error)
	err = db.Model(&model.Channel{}).Where("id = ?", 1).Update("status", common.ChannelStatusEnabled).Error
	require.ErrorContains(t, err, "explicit reviewed rule")
	var saved model.Channel
	require.NoError(t, db.First(&saved, 1).Error)
	require.Equal(t, common.ChannelStatusManuallyDisabled, saved.Status)
	// The alias's own price is followed live; no plugin-specific copy is needed.
	raw = `{"version":2,"task_priority_version":1,"models":{"alias":{"unit_price":0.1}}}`
	require.NoError(t, db.Model(&model.Option{}).Where("`key` = ?", model.ModelPricingOption).Update("value", raw).Error)
	require.NoError(t, db.Model(&model.Channel{}).Where("id = ?", 1).Update("status", common.ChannelStatusEnabled).Error)
	require.NoError(t, db.Model(&model.Channel{}).Where("id = ?", 1).Update("status", common.ChannelStatusManuallyDisabled).Error)
	raw = `{"version":2,"task_priority_version":1,"models":{"alias":{"unit_price":0.1,"task_expressions":{"happyhorse":{"version":1,"kind":"unit_price","unit_price":0.1,"upstream_model":"happyhorse-1.0-t2v","dimensions":"provider_v1","settlement":"submit"}}}}}`
	require.NoError(t, db.Model(&model.Option{}).Where("`key` = ?", model.ModelPricingOption).Update("value", raw).Error)
	require.NoError(t, db.Model(&model.Channel{}).Where("id = ?", 1).Update("status", common.ChannelStatusEnabled).Error)
	changed := `{"alias":"happyhorse-1.0-i2v"}`
	err = db.Model(&model.Channel{}).Where("id = ?", 1).Update("model_mapping", changed).Error
	require.ErrorContains(t, err, "not happyhorse-1.0-i2v")
	require.NoError(t, db.First(&saved, 1).Error)
	require.Equal(t, mapping, *saved.ModelMapping)
	// Struct updates and batch/tag updates use the same transactional guard.
	saved.ModelMapping = &changed
	require.Error(t, db.Model(&saved).Updates(&saved).Error)
	state := model.TaskPluginSnapshot{Enabled: true, FactoryEnabled: true, Production: []model.TaskPluginProductionBinding{b}}
	infos, err := m.OfflineTaskPricingUsage(t.Context(), []model.Channel{channel}, state, nil)
	require.NoError(t, err)
	require.Equal(t, "ready", infos["alias"].Plugins["happyhorse"].Status)
	require.NotContains(t, infos, "chat")
	require.NoError(t, db.Model(&model.Channel{}).Where("id = ?", 1).Update("status", common.ChannelStatusManuallyDisabled).Error)
	editor, err := model.GetTaskPricingEditorModels(t.Context())
	require.NoError(t, err)
	require.Len(t, editor, 1)
	require.Equal(t, "alias", editor[0].ModelName)
	require.Equal(t, "ready", editor[0].Usage.Plugins["happyhorse"].Status)
	require.Equal(t, "unit_price", editor[0].Pricing["happyhorse"][0].Rule.Kind)
}

func TestTaskPricingChannelGuardOnlyChecksWrittenChannelMySQL(t *testing.T) {
	oldDB, oldValidator := model.DB, model.ValidateTaskPricingTargets
	t.Cleanup(func() { model.DB, model.ValidateTaskPricingTargets = oldDB, oldValidator })
	m := NewManager(nil, factorySources())
	model.ValidateTaskPricingTargets = m.ValidatePricingTargets
	db := testdb.OpenMySQL8(t, &model.Option{}, &model.Channel{}, &model.TaskPluginControl{}, &model.TaskPlugin{}, &model.TaskPluginProductionBinding{}, &model.TaskPluginShadowBinding{}, &model.TaskPluginSourceSnapshot{})
	model.DB = db
	require.NoError(t, db.Create(&model.TaskPluginControl{Id: 1, Enabled: true, FactoryEnabled: true, DisabledFactory: "[]"}).Error)
	mapping := `{"alias":"happyhorse-1.0-t2v"}`
	bound := model.Channel{Id: 1, Type: model.TaskPluginChannelType("happyhorse"), Models: "alias", ModelMapping: &mapping, Status: common.ChannelStatusEnabled}
	text := model.Channel{Id: 2, Type: 1, Models: "chat", Status: common.ChannelStatusEnabled}
	require.NoError(t, db.Create(&bound).Error)
	require.NoError(t, db.Create(&text).Error)
	h, err := host.Compile(m.factories["happyhorse"])
	require.NoError(t, err)
	require.NoError(t, db.Create(&model.TaskPluginProductionBinding{ChannelID: 1, Key: "happyhorse", Version: h.Identity().Version, SourceHash: h.Identity().SourceHash, Factory: true, Enabled: true}).Error)
	// plugin_mapped on the tail is an explicit contract for an alias without its own price.
	mapped := `{"version":2,"task_priority_version":1,"models":{"happyhorse-1.0-t2v":{"task_expressions":{"happyhorse":{"version":1,"kind":"unit_price","unit_price":0.1,"upstream_model":"happyhorse-1.0-t2v","dimensions":"provider_v1","settlement":"submit"}}}}}`
	require.NoError(t, db.Create(&model.Option{Key: model.ModelPricingOption, Value: mapped}).Error)
	require.NoError(t, db.Model(&model.Channel{Id: 1}).Updates(map[string]any{"status": common.ChannelStatusEnabled}).Error)

	// Break the bound channel's contract behind its back (options-table write, no channel hook).
	broken := `{"version":2,"task_priority_version":1,"models":{"other":{"unit_price":0.1}}}`
	// Self-use mode gives the alias its own (default ratio) price, which is followed like any model price.
	var document model.ModelPricingConfig
	require.NoError(t, json.Unmarshal([]byte(broken), &document))
	var binding model.TaskPluginProductionBinding
	require.NoError(t, db.First(&binding, "channel_id = ?", 1).Error)
	state := model.TaskPluginSnapshot{Enabled: true, FactoryEnabled: true, Production: []model.TaskPluginProductionBinding{binding}}
	require.ErrorContains(t, m.ValidatePricingTargets(t.Context(), document, []model.Channel{bound}, state, nil, false), "explicit reviewed rule")
	require.NoError(t, m.ValidatePricingTargets(t.Context(), document, []model.Channel{bound}, state, nil, true))
	require.NoError(t, db.Model(&model.Option{}).Where("`key` = ?", model.ModelPricingOption).Update("value", broken).Error)
	// Unrelated channel writes and operational writes on the broken one still succeed.
	require.NoError(t, db.Model(&model.Channel{Id: 2}).Updates(map[string]any{"status": common.ChannelStatusAutoDisabled, "other_info": "{}"}).Error)
	require.NoError(t, db.Model(&model.Channel{Id: 2}).Updates(map[string]any{"status": common.ChannelStatusEnabled}).Error)
	require.NoError(t, db.Model(&text).Update("models", "chat,chat-2").Error)
	require.NoError(t, db.Model(&bound).Select("response_time", "test_time").Updates(model.Channel{ResponseTime: 10, TestTime: 1}).Error)
	require.NoError(t, db.Model(&bound).Select("balance_updated_time", "balance").Updates(model.Channel{Balance: 1, BalanceUpdatedTime: 1}).Error)
	require.NoError(t, db.Model(&model.Channel{}).Where("id = ?", 1).Update("used_quota", 5).Error)
	require.NoError(t, db.Model(&model.Channel{Id: 1}).Updates(map[string]any{"status": common.ChannelStatusAutoDisabled}).Error)
	// Re-enabling or retargeting the broken channel itself is still refused.
	require.ErrorContains(t, db.Model(&model.Channel{Id: 1}).Updates(map[string]any{"status": common.ChannelStatusEnabled}).Error, "explicit reviewed rule")
	bound.Status = common.ChannelStatusEnabled
	require.Error(t, db.Save(&bound).Error)
	var saved model.Channel
	require.NoError(t, db.First(&saved, 1).Error)
	require.Equal(t, common.ChannelStatusAutoDisabled, saved.Status)
}
