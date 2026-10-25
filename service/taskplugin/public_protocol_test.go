package taskplugin

import (
	"strings"
	"testing"

	"eflow.local/new-api/common"
	"eflow.local/new-api/model"
	"eflow.local/new-api/plugins"
	host "eflow.local/new-api/relay/channel/task/jsplugin"
	"github.com/stretchr/testify/require"
)

func publicTestManager(t *testing.T) *Manager {
	t.Helper()
	m := NewManager(&memoryStore{state: model.TaskPluginSnapshot{Revision: 1, Enabled: true, FactoryEnabled: true}}, factorySources())
	for id, key := range map[int]string{1: "alibaba", 2: "doubao", 3: "alibaba", 4: "happyhorse"} {
		h, err := host.Compile(m.factories[key])
		require.NoError(t, err)
		require.NoError(t, m.BindProduction(t.Context(), id, ProductionBindingRequest{Key: key, Version: h.Identity().Version, Factory: true, Enabled: true}))
	}
	return m
}

func TestPublicAliasRebindRevokesPin(t *testing.T) {
	m := publicTestManager(t)
	ch := publicChannel(1, "alibaba", "Movie", `{"Movie":"wan2.6-t2v"}`)
	route, err := m.resolvePublicRoute([]model.Channel{ch}, "Movie", "/v1/responses")
	require.NoError(t, err)
	require.NotNil(t, route)
	source := strings.Replace(m.factories["alibaba"], `version: "1.3.0"`, `version: "2.0.0"`, 1)
	_, err = m.Upload(t.Context(), UploadRequest{Source: source})
	require.NoError(t, err)
	require.NoError(t, m.Activate(t.Context(), "alibaba", "2.0.0"))
	require.True(t, m.Allows(route, &ch), "global activation cannot replace the production binding")
	require.NoError(t, m.BindProduction(t.Context(), 1, ProductionBindingRequest{Key: "alibaba", Version: "2.0.0", Enabled: true}))
	require.False(t, m.Allows(route, &ch), "rebind revokes the request's exact binding")
	newRoute, err := m.resolvePublicRoute([]model.Channel{ch}, "Movie", "/v1/responses")
	require.NoError(t, err)
	require.True(t, m.Allows(newRoute, &ch))
}

func publicChannel(id int, key, name, mapping string) model.Channel {
	return model.Channel{Id: id, Type: model.TaskPluginChannelType(key), Status: common.ChannelStatusEnabled, Models: name, ModelMapping: &mapping}
}

func TestPublicAliasRouting(t *testing.T) {
	m := publicTestManager(t)
	alias := publicChannel(1, "alibaba", "Movie", `{"Movie":"middle","middle":"WAN2.6-T2V"}`)
	for _, name := range []string{"Movie", "MOVIE", "movie"} {
		route, err := m.resolvePublicRoute([]model.Channel{alias}, name, "/v1/responses")
		require.NoError(t, err)
		require.NotNil(t, route)
		require.Equal(t, name, route.Model)
		require.Equal(t, "Movie", route.RoutingModel)
		require.Equal(t, "wan2.6-t2v", route.Targets[1].Model)
		require.True(t, m.Allows(route, &alias))
		changed := alias
		changed.Status = common.ChannelStatusManuallyDisabled
		require.False(t, m.Allows(route, &changed))
		changed = alias
		mapping := `{"Movie":"wan2.2-t2v-plus"}`
		changed.ModelMapping = &mapping
		require.False(t, m.Allows(route, &changed))
	}
	for _, name := range []string{"chat", "Kovie", " Movie"} {
		r, e := m.resolvePublicRoute([]model.Channel{alias}, name, "/v1/responses")
		require.NoError(t, e)
		require.Nil(t, r)
	}
	t.Run("same_plugin_multiple_tails", func(t *testing.T) {
		second := publicChannel(3, "alibaba", "Movie", `{"Movie":"wan2.2-t2v-plus"}`)
		r, e := m.resolvePublicRoute([]model.Channel{alias, second}, "movie", "/v1/responses")
		require.NoError(t, e)
		require.Len(t, r.Targets, 2)
	})
	t.Run("cross_plugin_conflict", func(t *testing.T) {
		other := publicChannel(2, "doubao", "Movie", `{"Movie":"doubao-seedance-1-0-lite-t2v"}`)
		_, e := m.resolvePublicRoute([]model.Channel{alias, other}, "movie", "/v1/responses")
		require.Error(t, e)
	})
	t.Run("declaration_before_alias", func(t *testing.T) {
		declared := publicChannel(1, "alibaba", "wan2.6-t2v", `{}`)
		alias := publicChannel(2, "doubao", "WAN2.6-T2V", `{"WAN2.6-T2V":"doubao-seedance-1-0-lite-t2v"}`)
		r, e := m.resolvePublicRoute([]model.Channel{declared, alias}, "WAN2.6-T2V", "/v1/responses")
		require.NoError(t, e)
		require.Len(t, r.Targets, 1)
		require.Contains(t, r.Targets, 1)
	})
	t.Run("exact_before_fold", func(t *testing.T) {
		other := publicChannel(2, "doubao", "MOVIE", `{"MOVIE":"doubao-seedance-1-0-lite-t2v"}`)
		r, e := m.resolvePublicRoute([]model.Channel{alias, other}, "Movie", "/v1/responses")
		require.NoError(t, e)
		require.Len(t, r.Targets, 1)
		require.Contains(t, r.Targets, 1)
		_, e = m.resolvePublicRoute([]model.Channel{alias, other}, "movie", "/v1/responses")
		require.Error(t, e)
	})
	t.Run("cycle", func(t *testing.T) {
		mapping := `{"Movie":"middle","middle":"Movie"}`
		alias.ModelMapping = &mapping
		_, e := m.resolvePublicRoute([]model.Channel{alias}, "Movie", "/v1/responses")
		require.Error(t, e)
	})
	t.Run("ordinary_and_legacy", func(t *testing.T) {
		r, e := m.resolvePublicRoute([]model.Channel{publicChannel(4, "happyhorse", "happyhorse-1.0-t2v", `{}`)}, "happyhorse-1.0-t2v", "/v1/videos")
		require.NoError(t, e)
		require.Nil(t, r)
	})
}

func TestPublicProtocolDecode(t *testing.T) {
	for _, key := range []string{"alibaba", "doubao"} {
		t.Run(key, func(t *testing.T) {
			source, err := plugins.Source(key)
			require.NoError(t, err)
			h, err := host.Compile(source)
			require.NoError(t, err)
			tail := "wan2.6-t2v"
			if key == "doubao" {
				tail = "doubao-seedance-1-0-lite-t2v"
			}
			for _, mode := range []string{"sync", "background", "stream"} {
				body := map[string]any{"model": "MyAlias", "input": "a fox", "seconds": 5}
				if mode != "sync" {
					body[mode] = true
				}
				input := map[string]any{"model": "MyAlias", "upstreamModel": tail, "protocolBody": map[string]any{"kind": "json", "value": body}}
				require.NoError(t, decodePublicSubmit(t.Context(), h, input, "openai_responses"))
				require.Equal(t, mode, input["publicMode"])
				decoded := input["requestBody"].(map[string]any)
				require.Equal(t, "MyAlias", decoded["model"])
				require.Equal(t, "a fox", decoded["prompt"])
				require.NotContains(t, body, "prompt")
			}
			for _, body := range []map[string]any{
				{"model": "MyAlias", "input": "x", "stream": "true"},
				{"model": "MyAlias", "input": "x", "previous_response_id": "foreign"},
				{"model": "MyAlias", "input": "x", "metadata": map[string]any{"content": []any{map[string]any{"type": "draft_task", "draft_task": map[string]any{"id": "foreign"}}}}},
			} {
				require.Error(t, decodePublicSubmit(t.Context(), h, map[string]any{"model": "MyAlias", "upstreamModel": tail, "protocolBody": map[string]any{"kind": "json", "value": body}}, "openai_responses"))
			}
			input := map[string]any{"model": "MyAlias", "upstreamModel": tail, "protocolBody": map[string]any{"kind": "multipart", "fields": map[string][]string{"model": {"MyAlias"}, "prompt": {"fox"}, "seconds": {"5"}}, "files": []any{}}}
			require.NoError(t, decodePublicSubmit(t.Context(), h, input, "openai_video"))
			require.Equal(t, "fox", input["requestBody"].(map[string]any)["prompt"])
			input["protocolBody"].(map[string]any)["fields"].(map[string][]string)["seconds"] = []string{"5", "6"}
			require.Error(t, decodePublicSubmit(t.Context(), h, input, "openai_video"))
			limited := strings.Replace(source, `["stream", "sync", "background"]`, `["stream", "background"]`, 1)
			if key == "alibaba" {
				require.ErrorContains(t, decodePublicSubmit(t.Context(), h, map[string]any{"model": "MyAlias", "upstreamModel": "wan2.6-image", "protocolBody": map[string]any{"kind": "json", "value": map[string]any{"model": "MyAlias", "input": "fox"}}}, "openai_responses"), "unsupported result lifecycle")
			}
			h, err = host.Compile(limited)
			require.NoError(t, err)
			require.ErrorContains(t, decodePublicSubmit(t.Context(), h, map[string]any{"model": "MyAlias", "upstreamModel": tail, "protocolBody": map[string]any{"kind": "json", "value": map[string]any{"model": "MyAlias", "input": "fox"}}}, "openai_responses"), "not supported")
		})
	}
}

func TestPublicCombinedModeRequiresBoth(t *testing.T) {
	source, err := plugins.Source("doubao")
	require.NoError(t, err)
	for _, supports := range []string{`["stream", "sync"]`, `["sync", "background"]`, `["stream", "sync", "background"]`} {
		t.Run(supports, func(t *testing.T) {
			fixture := strings.Replace(source, `["stream", "sync", "background"]`, supports, 1)
			if !strings.Contains(supports, "stream") {
				fixture += "\ndelete protocols.openai_responses.renderEvents;"
			}
			h, err := host.Compile(fixture)
			require.NoError(t, err)
			input := map[string]any{"model": "Movie", "upstreamModel": "doubao-seedance-1-0-lite-t2v", "protocolBody": map[string]any{"kind": "json", "value": map[string]any{"model": "Movie", "input": "fox", "stream": true, "background": true}}}
			err = decodePublicSubmit(t.Context(), h, input, "openai_responses")
			if strings.Contains(supports, "stream") && strings.Contains(supports, "background") {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, "not supported")
			}
		})
	}
}
