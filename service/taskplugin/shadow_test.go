package taskplugin

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"eflow.local/new-api/constant"
	"eflow.local/new-api/model"
	"eflow.local/new-api/plugins"
	host "eflow.local/new-api/relay/channel/task/jsplugin"
	"github.com/stretchr/testify/require"
)

func shadowTestManager(t *testing.T) (*Manager, *memoryStore, string) {
	t.Helper()
	source, err := plugins.Source("alibaba")
	require.NoError(t, err)
	s := &memoryStore{state: model.TaskPluginSnapshot{Revision: 1, FactoryEnabled: true}}
	m := NewManager(s, map[string]string{"alibaba": source})
	h, err := host.Compile(source)
	require.NoError(t, err)
	require.NoError(t, m.BindShadow(t.Context(), 17, ShadowBindingRequest{Key: "alibaba", Version: h.Identity().Version, Factory: true, Enabled: true, SampleEvery: 1}))
	require.NoError(t, m.Configure(t.Context(), Settings{Enabled: flag(true)}))
	return m, s, source
}

func TestShadowPinsVersionAndProtectsReferences(t *testing.T) {
	m, s, source := shadowTestManager(t)
	stop := m.startShadow(t.Context())
	defer stop()
	ticket := m.BeginShadow(17, constant.ChannelTypeAli)
	require.NotNil(t, ticket)
	identity := ticket.entry.handle.Identity()
	override := strings.Replace(source, `version: "1.3.0"`, `version: "9.0.0"`, 1)
	require.NotEqual(t, source, override)
	meta, err := m.Upload(t.Context(), UploadRequest{Source: override})
	require.NoError(t, err)
	require.Equal(t, identity, ticket.entry.handle.Identity())
	require.NoError(t, m.BindShadow(t.Context(), 17, ShadowBindingRequest{Key: "alibaba", Version: meta.Version, Enabled: true, SampleEvery: 1}))
	require.ErrorIs(t, m.Delete(t.Context(), "alibaba", meta.Version), ErrConflict)
	require.False(t, ticket.enabled(), "old binding tickets stop admitting observations")
	require.NoError(t, m.SetStatus(t.Context(), "alibaba", false))
	require.Nil(t, m.BeginShadow(17, constant.ChannelTypeAli))
	require.NoError(t, m.UnbindShadow(t.Context(), 17))
	require.NoError(t, m.Delete(t.Context(), "alibaba", meta.Version))
	require.Empty(t, s.state.Bindings)
}

func TestShadowSamplingQueueBoundAndMasterSwitch(t *testing.T) {
	m, _, _ := shadowTestManager(t)
	// Exercise queue admission without a consumer, proving it does not block.
	m.shadowRunning.Store(true)
	defer m.shadowRunning.Store(false)
	require.Nil(t, m.BeginShadow(17, constant.ChannelTypeAliHappyhorse))
	b := m.view.Load().shadow[17]
	b.binding.SampleEvery = 3
	accepted := 0
	for range 10 {
		if m.BeginShadow(17, constant.ChannelTypeAli) != nil {
			accepted++
		}
	}
	require.Equal(t, 4, accepted)
	b.binding.SampleEvery = 1
	ticket := m.BeginShadow(17, constant.ChannelTypeAli)
	for range 40 {
		ticket.Observe("request", map[string]any{}, nil, nil)
	}
	require.Len(t, m.shadowQueue, 16)
	require.EqualValues(t, 24, m.shadowDropped.Load())
	require.NoError(t, m.Configure(t.Context(), Settings{Enabled: flag(false)}))
	require.Nil(t, m.BeginShadow(17, constant.ChannelTypeAli))
	require.False(t, ticket.enabled())
}

func TestShadowReportsAreBoundedAndContainNoPrivateValues(t *testing.T) {
	m, _, _ := shadowTestManager(t)
	ctx, cancel := context.WithCancel(context.Background())
	stop := m.startShadow(ctx)
	ticket := m.BeginShadow(17, constant.ChannelTypeAli)
	require.NotNil(t, ticket)
	input := map[string]any{"upstreamModel": "wan2.6-t2v", "baseUrl": "https://provider.example", "apiKey": "SECRET-CREDENTIAL", "requestBody": map[string]any{"model": "wan2.6-t2v", "prompt": "PRIVATE PROMPT", "duration": 5}}
	ticket.Observe("request", input, map[string]any{"body": map[string]any{"prompt": "OTHER PRIVATE"}}, nil)
	require.Eventually(t, func() bool { return len(m.ShadowReports()["reports"].([]ShadowReport)) == 1 }, time.Second, 5*time.Millisecond)
	input["apiKey"] = "changed after admission"
	raw, err := json.Marshal(m.ShadowReports())
	require.NoError(t, err)
	for _, secret := range []string{"SECRET-CREDENTIAL", "PRIVATE PROMPT", "OTHER PRIVATE", "provider.example", "changed after admission"} {
		require.NotContains(t, string(raw), secret)
	}
	for range 105 {
		m.recordShadow(ShadowReport{Outcome: "match"})
	}
	require.Len(t, m.ShadowReports()["reports"].([]ShadowReport), 100)
	cancel()
	stop()
	require.False(t, m.shadowRunning.Load())
	require.Empty(t, m.shadowQueue)
}

func TestShadowQueryReportsKnownUnknownStatusDifference(t *testing.T) {
	m, _, _ := shadowTestManager(t)
	entry := m.view.Load().shadow[17]
	obs := observation{Input: map[string]any{"model": "wan3.0-video"}, Expected: map[string]any{"status": "QUEUED", "url": "", "reason": ""}, Response: &host.Response{Status: 200, Body: map[string]any{"output": map[string]any{"task_status": "NEW_STATE"}}}}
	raw, err := json.Marshal(obs)
	require.NoError(t, err)
	report := m.compareShadow(t.Context(), shadowJob{ticket: &Ticket{manager: m, entry: entry}, stage: "query", payload: raw})
	require.Equal(t, "different", report.Outcome)
	require.Contains(t, report.Paths, "/status")
	// No Task or quota references are given to the comparator.
	require.Equal(t, "QUEUED", obs.Expected.(map[string]any)["status"])
}
