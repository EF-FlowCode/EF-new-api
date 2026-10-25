package service

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"eflow.local/new-api/model"
	host "eflow.local/new-api/relay/channel/task/jsplugin"
	management "eflow.local/new-api/service/taskplugin"
	"github.com/stretchr/testify/require"
)

func TestPluginContentMySQL(t *testing.T) {
	for _, p := range []struct{ key, name, data, path, header, value string }{
		{"sora", "", `{"id":"provider-id","status":"completed"}`, "/v1/videos/provider-id/content", "Authorization", "Bearer bound-key"},
		{"hailuo", "MiniMax-Hailuo-02", `{"status":"Success","file_id":"file-id"}`, "/v1/files/download?file_id=file-id", "Authorization", "Bearer bound-key"},
		{"google", "", `{"response":{"generateVideoResponse":{"generatedVideos":[{"video":{"uri":"BASE/video"}}]}}}`, "/video", "x-goog-api-key", "bound-key"},
		{"vertex-ai", "", `{"response":{"video":"BASE/video"}}`, "/video", "Authorization", ""},
		{"alibaba", "wan2.6-t2v", `{"output":{"video_url":"BASE/video"}}`, "/video", "Authorization", ""},
		{"happyhorse", "", `{"output":{"video_url":"BASE/video"}}`, "/video", "Authorization", ""},
		{"doubao", "", `{"content":{"video_url":"BASE/video"}}`, "/video", "Authorization", ""},
		{"jimeng", "", `{"data":{"video_url":"BASE/video"}}`, "/video", "Authorization", ""},
		{"kling", "", `{"data":{"task_result":{"videos":[{"url":"BASE/video"}]}}}`, "/video", "Authorization", ""},
		{"vidu", "", `{"creations":[{"url":"BASE/video"}]}`, "/video", "Authorization", ""},
	} {
		t.Run(p.key, func(t *testing.T) {
			task, ch := setupAllPluginPoll(t, p.key, p.name)
			calls := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				require.Equal(t, p.path, r.URL.RequestURI())
				require.Equal(t, p.value, r.Header.Get(p.header))
				w.Header().Set("Content-Type", "video/mp4")
				_, _ = io.WriteString(w, "video-bytes")
			}))
			defer srv.Close()
			task.Status = model.TaskStatusSuccess
			task.Data = []byte(strings.ReplaceAll(p.data, "BASE", srv.URL))
			require.NoError(t, model.DB.Save(task).Error)
			plan, err := management.Default.PrepareTaskQueries(t.Context(), []int64{task.ID}, map[int]management.TaskQueryEnvironment{ch.Id: {BaseURL: srv.URL, Auth: map[string]any{"projectId": "test-project"}}})
			require.NoError(t, err)
			require.Len(t, plan.Queries, 1)
			executor := host.Executor{Client: srv.Client(), ValidateURL: func(raw string) error { require.Equal(t, srv.URL+p.path, raw); return nil }}
			resp, err := plan.Queries[0].OpenContent(t.Context(), executor)
			require.NoError(t, err)
			raw, err := io.ReadAll(resp.Body)
			resp.Body.Close()
			require.NoError(t, err)
			require.Equal(t, "video-bytes", string(raw))
			require.NoError(t, model.DB.Model(&model.TaskPluginControl{}).Where("id = 1").Update("enabled", false).Error)
			_, err = plan.Queries[0].OpenContent(t.Context(), executor)
			require.ErrorIs(t, err, model.ErrTaskPluginDisabled)
			require.Equal(t, 1, calls)
		})
	}
}

func TestPublicSunoContentMySQL(t *testing.T) {
	task, ch := setupAllPluginPoll(t, "sunoapi", "suno_music")
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		require.Equal(t, "/song", r.URL.Path)
		require.Empty(t, r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "audio/mpeg")
		_, _ = io.WriteString(w, "audio-bytes")
	}))
	defer srv.Close()
	task.Status = model.TaskStatusSuccess
	task.Data = []byte(`[{"id":"song-1","audio_url":"` + srv.URL + `/song"}]`)
	task.PrivateData.PluginContext = &model.TaskPluginContext{PublicProtocol: "openai_responses"}
	require.NoError(t, model.DB.Save(task).Error)
	_, h, err := management.Default.PublicTask(t.Context(), task.TaskID, task.UserId, "openai_responses")
	require.NoError(t, err)
	pc, _, err := management.PublicTaskPresentation(t.Context(), h, task)
	require.NoError(t, err)
	artifacts := pc["artifacts"].(map[string]any)
	require.Len(t, artifacts, 1)
	key := ""
	for k := range artifacts {
		key = k
	}
	plan, err := management.Default.PrepareTaskQueries(t.Context(), []int64{task.ID}, map[int]management.TaskQueryEnvironment{ch.Id: {BaseURL: srv.URL}})
	require.NoError(t, err)
	require.Len(t, plan.Queries, 1)
	executor := host.Executor{Client: srv.Client(), ValidateURL: func(raw string) error { require.Equal(t, srv.URL+"/song", raw); return nil }}
	resp, err := plan.Queries[0].OpenContent(t.Context(), executor, key)
	require.NoError(t, err)
	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	require.NoError(t, err)
	require.Equal(t, "audio-bytes", string(body))
	_, err = plan.Queries[0].OpenContent(t.Context(), executor, "unknown")
	require.Error(t, err)
	require.Equal(t, 1, calls)
}
