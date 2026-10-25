package taskplugin

import (
	"strings"
	"testing"

	"eflow.local/new-api/model"
	"eflow.local/new-api/plugins"
	host "eflow.local/new-api/relay/channel/task/jsplugin"
	"github.com/stretchr/testify/require"
)

func TestProductionPreparedRechecksSwitches(t *testing.T) {
	for _, change := range []string{"total", "factory", "provider", "binding", "unbind", "rebind", "storage"} {
		t.Run(change, func(t *testing.T) {
			source, err := plugins.Source("happyhorse")
			require.NoError(t, err)
			store := &memoryStore{state: model.TaskPluginSnapshot{Revision: 1, Enabled: true, FactoryEnabled: true}}
			m := NewManager(store, map[string]string{"happyhorse": source})
			require.NoError(t, m.BindProduction(t.Context(), 58, ProductionBindingRequest{Key: "happyhorse", Version: "1.0.0", Factory: true, Enabled: true}))
			prepared, err := m.PrepareProductionSubmit(t.Context(), 58, map[string]any{"model": "local-video", "upstreamModel": "HAPPYHORSE-1.0-T2V", "baseUrl": "https://provider.example", "requestBody": map[string]any{"prompt": "test"}})
			require.NoError(t, err)
			switch change {
			case "total":
				store.state.Enabled = false
			case "factory":
				store.state.FactoryEnabled = false
			case "provider":
				store.state.DisabledFactory = []string{"happyhorse"}
			case "binding":
				store.state.Production[0].Enabled = false
			case "unbind":
				store.state.Production = nil
			case "rebind":
				store.state.Production[0].SourceHash = "changed"
			case "storage":
				store.readError = ErrUnavailable
			}
			_, err = prepared.Execute(t.Context(), host.Executor{ValidateURL: func(string) error { t.Fatal("disabled prepared request reached HTTP executor"); return nil }}, "https://provider.example", nil)
			require.Error(t, err)
		})
	}
}

func TestProductionBoundModelNormalization(t *testing.T) {
	source, err := plugins.Source("happyhorse")
	require.NoError(t, err)
	for _, tc := range []struct {
		name, prefix, requested, canonical string
	}{
		{"exact", "happyhorse", "happyhorse-1.0-t2v", "happyhorse-1.0-t2v"},
		{"ascii_case", "happyhorse", "HAPPYHORSE-1.0-T2V", "happyhorse-1.0-t2v"},
		{"undeclared", "happyhorse", "WAN3.0-VIDEO", ""},
		{"empty", "happyhorse", "", ""},
		{"whitespace", "happyhorse", " happyhorse-1.0-t2v", ""},
		{"kelvin_not_ascii_k", "kelvin", "KELVIN-1.0-T2V", ""},
		{"non_ascii_exact", "Kelvin", "Kelvin-1.0-t2v", "Kelvin-1.0-t2v"},
		{"non_ascii_preserved", "Kelvin", "KELVIN-1.0-T2V", "Kelvin-1.0-t2v"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			localSource := strings.ReplaceAll(source, "happyhorse-1.0", tc.prefix+"-1.0")
			store := &memoryStore{state: model.TaskPluginSnapshot{Revision: 1, Enabled: true, FactoryEnabled: true}}
			m := NewManager(store, map[string]string{"happyhorse": localSource})
			require.NoError(t, m.BindProduction(t.Context(), 58, ProductionBindingRequest{Key: "happyhorse", Version: "1.0.0", Factory: true, Enabled: true}))
			body := map[string]any{"model": "local-video", "prompt": "test", "seconds": 5}
			input := map[string]any{"model": "local-video", "upstreamModel": tc.requested, "baseUrl": "https://provider.example", "requestBody": body}
			prepared, err := m.PrepareProductionSubmit(t.Context(), 58, input)
			require.Equal(t, tc.requested, input["upstreamModel"], "normalization must not mutate the caller")
			require.Equal(t, "local-video", body["model"])
			if tc.canonical == "" {
				require.ErrorContains(t, err, "model is not declared")
				require.Nil(t, prepared)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.canonical, prepared.UpstreamModel)
			require.Equal(t, tc.canonical, prepared.Input["upstreamModel"])
			require.Equal(t, "local-video", prepared.Input["model"])
			require.Equal(t, tc.canonical, prepared.Descriptor.Body.(map[string]any)["model"])
			facts, err := prepared.Usage(t.Context())
			require.NoError(t, err)
			require.Equal(t, float64(5), facts["seconds"])
			body["prompt"] = "changed after preparation"
			require.Equal(t, "test", prepared.Input["requestBody"].(map[string]any)["prompt"])
		})
	}
}

func TestProductionModelUsesBoundVersion(t *testing.T) {
	source, err := plugins.Source("happyhorse")
	require.NoError(t, err)
	store := &memoryStore{state: model.TaskPluginSnapshot{Revision: 1, Enabled: true, FactoryEnabled: true}}
	m := NewManager(store, map[string]string{"happyhorse": source})
	require.NoError(t, m.BindProduction(t.Context(), 58, ProductionBindingRequest{Key: "happyhorse", Version: "1.0.0", Factory: true, Enabled: true}))
	newSource := strings.Replace(source, `version: "1.0.0"`, `version: "2.0.0"`, 1)
	newSource = strings.ReplaceAll(newSource, "happyhorse-1.0-t2v", "second-version-t2v")
	_, err = m.Upload(t.Context(), UploadRequest{Source: newSource})
	require.NoError(t, err)
	require.NoError(t, m.Activate(t.Context(), "happyhorse", "2.0.0"))
	require.Equal(t, "2.0.0", currentVersion(t, m, "happyhorse"))
	input := map[string]any{"model": "alias", "upstreamModel": "HAPPYHORSE-1.0-T2V", "baseUrl": "https://provider.example", "requestBody": map[string]any{"prompt": "test"}}
	prepared, err := m.PrepareProductionSubmit(t.Context(), 58, input)
	require.NoError(t, err)
	require.Equal(t, "1.0.0", prepared.Identity.Version)
	require.Equal(t, store.state.Production[0].SourceHash, prepared.Identity.SourceHash)
	input["upstreamModel"] = "SECOND-VERSION-T2V"
	_, err = m.PrepareProductionSubmit(t.Context(), 58, input)
	require.ErrorContains(t, err, "model is not declared")
	require.NoError(t, m.BindProduction(t.Context(), 58, ProductionBindingRequest{Key: "happyhorse", Version: "2.0.0", Enabled: true}))
	require.Error(t, prepared.ValidateExecution(t.Context()), "rebind must revoke the old prepared request")
	newPrepared, err := m.PrepareProductionSubmit(t.Context(), 58, input)
	require.NoError(t, err)
	require.Equal(t, "second-version-t2v", newPrepared.UpstreamModel)
	require.Equal(t, "2.0.0", newPrepared.Identity.Version)
	store.state.Enabled = false
	_, err = m.PrepareProductionSubmit(t.Context(), 58, input)
	require.ErrorIs(t, err, model.ErrTaskPluginDisabled)
}

func TestProductionRejectsModelDeclarationCollision(t *testing.T) {
	source, err := plugins.Source("happyhorse")
	require.NoError(t, err)
	source = strings.Replace(source, `"happyhorse-1.0-i2v"`, `"HAPPYHORSE-1.0-T2V"`, 1)
	store := &memoryStore{state: model.TaskPluginSnapshot{Revision: 1, Enabled: true, FactoryEnabled: true}}
	m := NewManager(store, map[string]string{"happyhorse": source})
	require.Error(t, m.BindProduction(t.Context(), 58, ProductionBindingRequest{Key: "happyhorse", Version: "1.0.0", Factory: true, Enabled: true}))
	require.Empty(t, store.state.Production)
}

func TestProductionWANCompletionModels(t *testing.T) {
	source, err := plugins.Source("alibaba")
	require.NoError(t, err)
	store := &memoryStore{state: model.TaskPluginSnapshot{Revision: 1, Enabled: true, FactoryEnabled: true}}
	m := NewManager(store, map[string]string{"alibaba": source})
	require.NoError(t, m.BindProduction(t.Context(), 17, ProductionBindingRequest{Key: "alibaba", Version: "1.3.0", Factory: true, Enabled: true}))
	for _, name := range []string{"wan2.2-s2v", "wan3.0-video", "wan3.0-video-prime", "not-declared"} {
		p, err := m.PrepareProductionSubmit(t.Context(), 17, map[string]any{"upstreamModel": name, "baseUrl": "https://provider.example", "requestBody": map[string]any{"prompt": "test", "image_url": "https://input.example/image.png", "audio_url": "https://input.example/audio.mp3"}})
		if name == "not-declared" {
			require.Error(t, err)
			require.Nil(t, p)
		} else {
			require.NoError(t, err, name)
			require.NotNil(t, p)
		}
	}
}
