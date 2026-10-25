package taskplugin

import (
	"eflow.local/new-api/model"
	"eflow.local/new-api/plugins"
	"github.com/stretchr/testify/require"
	"strings"
	"testing"
)

func TestBillingUsageInfosUsesFixedBindingAndRejectsAmbiguity(t *testing.T) {
	source, err := plugins.Source("happyhorse")
	require.NoError(t, err)
	store := &memoryStore{state: model.TaskPluginSnapshot{Revision: 1, Enabled: true, FactoryEnabled: true}}
	m := NewManager(store, map[string]string{"happyhorse": source})
	require.NoError(t, m.BindProduction(t.Context(), 58, ProductionBindingRequest{Key: "happyhorse", Version: "1.0.0", Factory: true, Enabled: true}))
	raw := `{"alias":"middle","middle":"HAPPYHORSE-1.0-T2V"}`
	channel := model.Channel{Id: 58, Type: model.TaskPluginChannelType("happyhorse"), Models: "alias", ModelMapping: &raw}
	infos, err := m.BillingUsageInfos(t.Context(), []model.Channel{channel})
	require.NoError(t, err)
	require.Equal(t, "ready", infos["alias"].Status)
	require.Equal(t, "second", infos["alias"].Schema["seconds"].Unit)
	require.Equal(t, "happyhorse-1.0-t2v", infos["alias"].Sources[0].Model)
	infos, err = m.BillingUsageInfos(t.Context(), []model.Channel{channel, {Id: 59, Type: 1, Models: "alias,chat"}})
	require.NoError(t, err)
	require.Equal(t, "conflict", infos["alias"].Status)
	require.Nil(t, infos["alias"].Schema)
	require.Equal(t, "ready", infos["alias"].Plugins["happyhorse"].Status, "ordinary channels cannot invalidate a plugin-specific contract")
	require.NotContains(t, infos, "chat")
	newSource := strings.Replace(source, `version: "1.0.0"`, `version: "2.0.0"`, 1)
	newSource = strings.ReplaceAll(newSource, "happyhorse-1.0-t2v", "second-version-t2v")
	_, err = m.Upload(t.Context(), UploadRequest{Source: newSource})
	require.NoError(t, err)
	require.NoError(t, m.Activate(t.Context(), "happyhorse", "2.0.0"))
	infos, err = m.BillingUsageInfos(t.Context(), []model.Channel{channel})
	require.NoError(t, err)
	require.Equal(t, "1.0.0", infos["alias"].Sources[0].Version, "active version must not replace production binding")
	require.NoError(t, m.BindProduction(t.Context(), 60, ProductionBindingRequest{Key: "happyhorse", Version: "1.0.0", Factory: true, Enabled: true}))
	second := channel
	second.Id = 60
	infos, err = m.BillingUsageInfos(t.Context(), []model.Channel{channel, second})
	require.NoError(t, err)
	require.Equal(t, "ready", infos["alias"].Status)
	require.Len(t, infos["alias"].Sources, 1)
	store.state.Production[0].SourceHash = "rebound"
	infos, err = m.BillingUsageInfos(t.Context(), []model.Channel{channel})
	require.NoError(t, err)
	require.Equal(t, "unavailable", infos["alias"].Status)
}

func TestBillingUsageInfosUsesModelProfile(t *testing.T) {
	source, err := plugins.Source("alibaba")
	require.NoError(t, err)
	store := &memoryStore{state: model.TaskPluginSnapshot{Revision: 1, Enabled: true, FactoryEnabled: true}}
	m := NewManager(store, map[string]string{"alibaba": source})
	for _, id := range []int{17, 18} {
		require.NoError(t, m.BindProduction(t.Context(), id, ProductionBindingRequest{Key: "alibaba", Version: "1.3.0", Factory: true, Enabled: true}))
	}
	videoMap, imageMap := `{"alias":"wan3.0-video"}`, `{"alias":"wan2.6-image"}`
	video := model.Channel{Id: 17, Type: model.TaskPluginChannelType("alibaba"), Models: "alias", ModelMapping: &videoMap}
	image := model.Channel{Id: 18, Type: model.TaskPluginChannelType("alibaba"), Models: "alias", ModelMapping: &imageMap}
	infos, err := m.BillingUsageInfos(t.Context(), []model.Channel{video})
	require.NoError(t, err)
	require.Contains(t, infos["alias"].Schema, "seconds")
	require.NotContains(t, infos["alias"].Schema, "count")
	infos, err = m.BillingUsageInfos(t.Context(), []model.Channel{image})
	require.NoError(t, err)
	require.NotContains(t, infos["alias"].Schema, "seconds")
	infos, err = m.BillingUsageInfos(t.Context(), []model.Channel{video, image})
	require.NoError(t, err)
	require.Equal(t, "conflict", infos["alias"].Status)
	store.state.Enabled = false
	require.Equal(t, "conflict", infos["alias"].Plugins["alibaba"].Status)
	require.Len(t, infos["alias"].Plugins["alibaba"].Sources, 2, "conflicting tails must remain visible to save-time validation")
	infos, err = m.BillingUsageInfos(t.Context(), []model.Channel{video})
	require.NoError(t, err)
	require.Equal(t, "unavailable", infos["alias"].Status)
}

func TestBillingUsageInfosIsolatesPluginProfiles(t *testing.T) {
	factories := map[string]string{}
	for _, key := range []string{"alibaba", "happyhorse"} {
		source, err := plugins.Source(key)
		require.NoError(t, err)
		factories[key] = source
	}
	store := &memoryStore{state: model.TaskPluginSnapshot{Revision: 1, Enabled: true, FactoryEnabled: true}}
	m := NewManager(store, factories)
	require.NoError(t, m.BindProduction(t.Context(), 17, ProductionBindingRequest{Key: "alibaba", Version: "1.3.0", Factory: true, Enabled: true}))
	require.NoError(t, m.BindProduction(t.Context(), 58, ProductionBindingRequest{Key: "happyhorse", Version: "1.0.0", Factory: true, Enabled: true}))
	aliMap, horseMap := `{"alias":"wan3.0-video"}`, `{"alias":"happyhorse-1.0-t2v"}`
	channels := []model.Channel{{Id: 17, Type: 17, Models: "alias", ModelMapping: &aliMap}, {Id: 58, Type: model.TaskPluginChannelType("happyhorse"), Models: "alias", ModelMapping: &horseMap}}
	infos, err := m.BillingUsageInfos(t.Context(), channels)
	require.NoError(t, err)
	require.Equal(t, "conflict", infos["alias"].Status)
	for _, key := range []string{"alibaba", "happyhorse"} {
		require.Equal(t, "ready", infos["alias"].Plugins[key].Status)
	}
	require.NoError(t, m.BindProduction(t.Context(), 18, ProductionBindingRequest{Key: "alibaba", Version: "1.3.0", Factory: true, Enabled: true}))
	textMap := `{"chat":"ordinary-text-model"}`
	ordinary := model.Channel{Id: 18, Type: 17, Models: "chat", ModelMapping: &textMap}
	textInfos, err := m.BillingUsageInfos(t.Context(), append(channels, ordinary))
	require.NoError(t, err)
	require.Empty(t, textInfos["chat"].Plugins, "ordinary text on a bound mixed channel must not block task pricing")
	brokenMap := `{"alias":"loop","loop":"alias"}`
	channels = append(channels, model.Channel{Id: 18, Type: 17, Models: "alias", ModelMapping: &brokenMap})
	infos, err = m.BillingUsageInfos(t.Context(), channels)
	require.NoError(t, err)
	require.True(t, infos["alias"].Plugins["alibaba"].UnresolvedTargets)
	require.Len(t, infos["alias"].Plugins["alibaba"].Sources, 1)
	require.Equal(t, "ready", infos["alias"].Plugins["happyhorse"].Status)
}

func TestUsageModelMappingMatchesTaskRules(t *testing.T) {
	for raw, expected := range map[string]string{`{"alias":"alias"}`: "alias", `{"alias":"b","b":"b"}`: "b", `{"alias":"b","b":"alias"}`: "", `{"alias":""}`: "alias", `invalid`: ""} {
		got, ok := mappedUsageModel("alias", &raw)
		require.Equal(t, expected != "", ok, raw)
		require.Equal(t, expected, got, raw)
	}
}
