package taskplugin

import (
	"encoding/json"
	"strings"
	"testing"

	"eflow.local/new-api/pkg/jsplugin"
	"eflow.local/new-api/plugins"
	host "eflow.local/new-api/relay/channel/task/jsplugin"
	"github.com/stretchr/testify/require"
)

func TestTaskEntryRouteMatching(t *testing.T) {
	meta := jsplugin.Meta{Routes: []jsplugin.Route{{Method: "GET", Path: "/ali/items/:id"}, {Method: "POST", Path: "/jimeng/"}, {Method: "GET", Path: "/ali/content/*path"}}}
	_, params, ok := MatchNativeRoute(meta, "GET", "/ali/items/task-1")
	require.True(t, ok)
	require.Equal(t, "task-1", params["id"])
	_, _, ok = MatchNativeRoute(meta, "GET", "/ali/items/")
	require.False(t, ok)
	_, _, ok = MatchNativeRoute(meta, "POST", "/jimeng")
	require.False(t, ok)
	_, _, ok = MatchNativeRoute(meta, "GET", "/jimeng/")
	require.False(t, ok)
	_, params, ok = MatchNativeRoute(meta, "GET", "/ali/content/a/b")
	require.True(t, ok)
	require.Equal(t, "/a/b", params["path"])
	meta.Routes = append(meta.Routes, jsplugin.Route{Method: "GET", Path: "/ali/items/:other"})
	_, _, ok = MatchNativeRoute(meta, "GET", "/ali/items/task-1")
	require.False(t, ok)
}

func TestTaskEntryNativeDecode(t *testing.T) {
	for _, tc := range []struct {
		key, path, model, body string
		query                  map[string][]string
	}{
		{"doubao", "/doubao/api/v3/contents/generations/tasks", "doubao-seedance-1-0-lite-t2v", `{"model":"MOVIE","content":[{"type":"text","text":"a fox"}],"duration":5}`, nil},
		{"alibaba", "/ali/api/v1/services/aigc/video-generation/video-synthesis", "wan2.6-t2v", `{"model":"MOVIE","input":{"prompt":"a fox"},"parameters":{"duration":5}}`, nil},
		{"kling", "/kling/v1/videos/text2video", "kling-v1", `{"model_name":"MOVIE","prompt":"a fox","duration":"5"}`, nil},
		{"jimeng", "/jimeng/", "jimeng_vgfm_t2v_l20", `{"req_key":"MOVIE","prompt":"a fox"}`, map[string][]string{"Action": {"CVSync2AsyncSubmitTask"}}},
	} {
		t.Run(tc.key, func(t *testing.T) {
			source, err := plugins.Source(tc.key)
			require.NoError(t, err)
			h, err := host.Compile(source)
			require.NoError(t, err)
			var body map[string]any
			require.NoError(t, json.Unmarshal([]byte(tc.body), &body))
			input := map[string]any{"model": "MOVIE", "upstreamModel": tc.model, "requestBody": body, "protocolBody": map[string]any{"kind": "json", "value": body}, "nativePath": tc.path, "nativeMethod": "POST", "nativeQuery": tc.query, "baseUrl": "https://example.com", "apiKey": "fixture-access|fixture-secret"}
			require.NoError(t, decodeTaskEntry(t.Context(), h, input, "native"))
			require.Equal(t, "MOVIE", input["requestBody"].(map[string]any)["model"])
			if tc.key == "kling" || tc.key == "doubao" {
				d, err := h.Build(t.Context(), "buildSubmitRequest", input)
				require.NoError(t, err)
				raw, err := json.Marshal(d.Body)
				require.NoError(t, err)
				require.NotContains(t, string(raw), `MOVIE`, "public alias must not overwrite machine model")
			}
		})
	}
}

func TestTaskEntryRejectReferencesAndImages(t *testing.T) {
	source, err := plugins.Source("alibaba")
	require.NoError(t, err)
	h, err := host.Compile(source)
	require.NoError(t, err)
	for _, body := range []map[string]any{{"originTaskId": "foreign"}, {"metadata": map[string]any{"draft_task": "foreign"}}} {
		err := decodeTaskEntry(t.Context(), h, map[string]any{"model": "Movie", "upstreamModel": "wan2.6-t2v", "requestBody": body}, "task")
		require.Error(t, err)
	}
	require.Error(t, decodeTaskEntry(t.Context(), h, map[string]any{"model": "image", "upstreamModel": "wan2.6-image", "requestBody": map[string]any{}}, "task"))
	_, _, matched := MatchNativeRoute(h.Meta(), "POST", "/api/option")
	require.False(t, matched)
}

func TestTaskEntrySunoAction(t *testing.T) {
	source, err := plugins.Source("sunoapi")
	require.NoError(t, err)
	h, err := host.Compile(source)
	require.NoError(t, err)
	for _, action := range []string{"music", ""} {
		body := map[string]any{"prompt": "lyrics", "action": action}
		input := map[string]any{"model": "LYRIC-ALIAS", "upstreamModel": "suno_lyrics", "requestBody": body, "baseUrl": "https://example.com", "apiKey": "fixture-key"}
		err := decodeTaskEntry(t.Context(), h, input, "task")
		if action == "music" {
			require.Error(t, err, "cannot submit music at the lyrics price")
		} else {
			require.NoError(t, err)
			d, e := h.Build(t.Context(), "buildSubmitRequest", input)
			require.NoError(t, e)
			require.Equal(t, "LYRICS", d.Action)
		}
	}
	for _, action := range []string{"music", "lyrics"} {
		for _, protocol := range []string{"native", "task"} {
			body := map[string]any{"model": "suno_" + action, "action": action, "prompt": "a song"}
			input := map[string]any{"model": "suno_" + action, "upstreamModel": "suno_" + action, "requestBody": body, "protocolBody": map[string]any{"kind": "json", "value": body}, "nativePath": "/suno/submit/" + action, "nativeMethod": "POST", "baseUrl": "https://example.com", "apiKey": "fixture-key"}
			require.NoError(t, decodeTaskEntry(t.Context(), h, input, protocol))
			d, err := h.Build(t.Context(), "buildSubmitRequest", input)
			require.NoError(t, err)
			require.Equal(t, strings.ToUpper(action), d.Action)
		}
	}
}
