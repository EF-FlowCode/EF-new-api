package service

import (
	relaycommon "eflow.local/new-api/relay/common"
	"eflow.local/new-api/types"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"eflow.local/new-api/constant"
	"eflow.local/new-api/model"
	"eflow.local/new-api/plugins"
	host "eflow.local/new-api/relay/channel/task/jsplugin"
	management "eflow.local/new-api/service/taskplugin"
	"eflow.local/new-api/setting/ratio_setting"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestPluginCompletionTokenBillingMySQL(t *testing.T) {
	old := ratio_setting.ModelRatio2JSONString()
	t.Cleanup(func() { require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(old)) })
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"local-token-model":0.5}`))
	for _, fixed := range []bool{false, true} {
		t.Run(strconv.FormatBool(fixed), func(t *testing.T) {
			task, ch := setupAllPluginPoll(t, "doubao", "")
			task.PrivateData.BillingContext.OriginModelName = "local-token-model"
			task.PrivateData.BillingContext.PerCallBilling = fixed
			require.NoError(t, model.DB.Save(task).Error)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.WriteString(w, `{"status":"succeeded","content":{"video_url":"https://result.example/video.mp4"},"usage":{"total_tokens":1000}}`)
			}))
			defer srv.Close()
			ch.BaseURL = &srv.URL
			for range 2 {
				copy := *task
				require.NoError(t, updateVideoSingleTaskWithExecutor(t.Context(), ch, task.TaskID, map[string]*model.Task{task.TaskID: &copy}, host.Executor{Client: srv.Client(), ValidateURL: func(string) error { return nil }}))
			}
			var saved model.Task
			require.NoError(t, model.DB.First(&saved, task.ID).Error)
			if fixed {
				require.Equal(t, 400, saved.Quota)
				require.Equal(t, 1000, getUserQuota(t, 40))
				require.Equal(t, 1000, getTokenRemainQuota(t, 40))
			} else {
				require.Equal(t, 500, saved.Quota)
				require.Equal(t, 900, getUserQuota(t, 40))
				require.Equal(t, 900, getTokenRemainQuota(t, 40))
			}
		})
	}
}

func TestPluginTokenSnapshotCompletionMySQL(t *testing.T) {
	oldRatios, oldGroups := ratio_setting.ModelRatio2JSONString(), ratio_setting.GroupRatio2JSONString()
	t.Cleanup(func() {
		require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(oldRatios))
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(oldGroups))
	})
	for _, mode := range []string{"changed_price", "removed_price", "unset", "free_group", "retry", "invalid"} {
		t.Run(mode, func(t *testing.T) {
			task, ch := setupAllPluginPoll(t, "doubao", "")
			price := types.PriceData{ModelRatio: 0.5, TaskModelRatioConfigured: true, GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 0.5}, OtherRatios: map[string]float64{"video_input": 0.8}}
			if mode == "unset" {
				price.TaskModelRatioConfigured = false
			}
			if mode == "free_group" {
				price.GroupRatioInfo.GroupRatio = 0
			}
			snapshot, err := NewTaskPluginTokenBilling(price, false)
			require.NoError(t, err)
			if mode == "invalid" {
				snapshot.Version = 99
			}
			if task.PrivateData.PluginContext == nil {
				task.PrivateData.PluginContext = &model.TaskPluginContext{}
			}
			task.PrivateData.PluginContext.TokenBilling = snapshot
			task.PrivateData.BillingContext.OriginModelName = "local-token-model"
			task.CreatedAt = time.Now().Add(-24 * time.Hour).Unix()
			require.NoError(t, model.DB.Save(task).Error)
			require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"local-token-model":99}`))
			if mode == "removed_price" {
				require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{}`))
			}
			require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":99}`))
			var calls atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				_, _ = io.WriteString(w, `{"status":"succeeded","content":{"video_url":"https://result.example/video.mp4"},"usage":{"total_tokens":1000}}`)
			}))
			defer srv.Close()
			ch.BaseURL = &srv.URL
			executor := host.Executor{Client: srv.Client(), ValidateURL: func(string) error { return nil }}
			poll := func(task *model.Task) error {
				return updateVideoSingleTaskWithExecutor(t.Context(), ch, task.TaskID, map[string]*model.Task{task.TaskID: task}, executor)
			}
			if mode == "retry" {
				require.NoError(t, model.DB.Exec("CREATE TRIGGER token_settle_failure BEFORE UPDATE ON tokens FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'injected token write failure'").Error)
			}
			err = poll(task)
			if mode == "retry" {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			saved := reloadCompletion(t, task.ID)
			if mode == "retry" || mode == "invalid" {
				require.Equal(t, model.TaskStatus(model.TaskStatusBillingPending), saved.Status)
				require.Equal(t, 1000, *saved.PrivateData.PluginContext.CompletionTokens)
				require.Equal(t, 1000, getUserQuota(t, 40))
				require.Equal(t, 1000, getTokenRemainQuota(t, 40))
				sweepTimedOutTasks(t.Context())
				require.Equal(t, model.TaskStatus(model.TaskStatusBillingPending), reloadCompletion(t, task.ID).Status)
				stale := *saved
				require.NoError(t, checkpointPluginCompletion(t.Context(), &stale, model.TaskStatusBillingPending, &relaycommon.TaskInfo{TotalTokens: 1}))
				require.Equal(t, 1000, *reloadCompletion(t, task.ID).PrivateData.PluginContext.CompletionTokens)
				management.Default = management.NewManager(model.SQLTaskPluginStore{DB: func() *gorm.DB { return model.DB }}, nil)
				if mode == "invalid" {
					require.Error(t, poll(saved))
					require.EqualValues(t, 1, calls.Load())
					return
				}
				require.NoError(t, model.DB.Exec("DROP TRIGGER token_settle_failure").Error)
				require.NoError(t, model.DB.Model(&model.TaskPluginControl{}).Where("id = 1").Update("enabled", false).Error)
				require.Error(t, poll(saved))
				require.EqualValues(t, 1, calls.Load())
				require.NoError(t, model.DB.Model(&model.TaskPluginControl{}).Where("id = 1").Update("enabled", true).Error)
				require.NoError(t, poll(saved))
				require.EqualValues(t, 1, calls.Load(), "retry only settles the stored completion")
			}
			saved = reloadCompletion(t, task.ID)
			want := 200
			if mode == "unset" {
				want = 400
			}
			if mode == "free_group" {
				want = 0
			}
			require.Equal(t, model.TaskStatus(model.TaskStatusSuccess), saved.Status)
			require.Equal(t, want, saved.Quota)
			require.Equal(t, 1400-want, getUserQuota(t, 40))
			require.Equal(t, 1400-want, getTokenRemainQuota(t, 40))
			require.NoError(t, poll(task))
			require.Equal(t, 1400-want, getUserQuota(t, 40))
		})
	}
}

func setupAllPluginPoll(t *testing.T, provider, name string) (*model.Task, *model.Channel) {
	t.Helper()
	truncate(t)
	require.NoError(t, model.DB.AutoMigrate(&model.QuotaData{}, &model.TaskPlugin{}, &model.TaskPluginControl{}, &model.TaskPluginShadowBinding{}, &model.TaskPluginProductionBinding{}, &model.TaskPluginSourceSnapshot{}, &model.TaskPluginTaskReference{}))
	seedUser(t, 40, 1000)
	seedToken(t, 40, 40, "all-plugin-token", 1000)
	seedChannel(t, 40)
	require.NoError(t, model.DB.Create(&model.TaskPluginControl{Id: 1, Revision: 1, Enabled: true, FactoryEnabled: true, DisabledFactory: "[]"}).Error)
	source, err := plugins.Source(provider)
	require.NoError(t, err)
	h, err := host.Compile(source)
	require.NoError(t, err)
	if name == "" {
		name = h.Meta().Models[0]
	}
	channelType := model.TaskPluginChannelType(provider)
	require.NoError(t, model.DB.Model(&model.Channel{}).Where("id = 40").Update("type", channelType).Error)
	store := model.SQLTaskPluginStore{DB: func() *gorm.DB { return model.DB }}
	m := management.NewManager(store, map[string]string{provider: source})
	require.NoError(t, m.BindProduction(t.Context(), 40, management.ProductionBindingRequest{Key: provider, Version: h.Identity().Version, Factory: true, Enabled: true}))
	task := makeTask(40, 40, 400, 40)
	task.Platform = constant.TaskPlatform(strconv.Itoa(channelType))
	task.PrivateData.UpstreamTaskID = "provider-id"
	task.PrivateData.Key = "bound-key"
	task.Action = "text_to_video"
	task.Properties.OriginModelName, task.Properties.UpstreamModelName = name, name
	if provider == "jimeng" {
		task.PrivateData.Key = "ak|sk"
		task.PrivateData.PluginContext = &model.TaskPluginContext{State: map[string]any{"req_key": "jimeng_t2v_v30_1080p"}}
	}
	if provider == "kling" {
		task.PrivateData.Key = "ak|sk"
	}
	if provider == "sunoapi" {
		task.Action = "MUSIC"
		task.Platform = constant.TaskPlatformSuno
	}
	if provider == "google" || provider == "vertex-ai" {
		task.PrivateData.UpstreamTaskID = base64.RawURLEncoding.EncodeToString([]byte("projects/test-project/locations/us-central1/publishers/google/models/veo-3.0-generate-001/operations/provider-id"))
	}
	if provider == "vertex-ai" {
		task.PrivateData.Key, _ = pluginTestAccount(t)
	}
	saved, err := m.CreatePluginTask(t.Context(), *task, management.TaskVersion{Key: provider, Version: h.Identity().Version, Factory: true})
	require.NoError(t, err)
	old := management.Default
	management.Default = management.NewManager(store, nil)
	t.Cleanup(func() { management.Default = old })
	return saved, &model.Channel{Id: 40, Type: channelType, Key: "wrong-current-key"}
}

func TestAllPluginPollingMySQL(t *testing.T) {
	for _, p := range []struct{ key, name, success, failure string }{
		{"alibaba", "wan2.6-t2v", `{"output":{"task_status":"SUCCEEDED","video_url":"https://result.example/video.mp4"}}`, `{"output":{"task_status":"FAILED","message":"rejected"}}`},
		{"happyhorse", "", `{"output":{"task_status":"SUCCEEDED","video_url":"https://result.example/video.mp4"}}`, `{"output":{"task_status":"FAILED","message":"rejected"}}`},
		{"doubao", "", `{"status":"succeeded","content":{"video_url":"https://result.example/video.mp4"}}`, `{"status":"failed","error":{"message":"rejected"}}`},
		{"google", "", `{"name":"operation","done":true,"response":{"generateVideoResponse":{"generatedVideos":[{"video":{"uri":"https://result.example/video.mp4"}}]}}}`, `{"error":{"message":"rejected"}}`},
		{"vertex-ai", "", `{"name":"operation","done":true,"response":{"videos":[{"bytesBase64Encoded":"dmlkZW8=","mimeType":"video/mp4"}]}}`, `{"error":{"message":"rejected"}}`},
		{"hailuo", "MiniMax-Hailuo-02", `{"status":"Success","file_id":"file-id","base_resp":{"status_code":0}}`, `{"status":"Fail","base_resp":{"status_code":0}}`},
		{"jimeng", "", `{"code":10000,"data":{"status":"done","video_url":"https://result.example/video.mp4"}}`, `{"code":400,"message":"rejected"}`},
		{"kling", "", `{"code":0,"data":{"task_id":"provider-id","task_status":"succeed","task_result":{"videos":[{"url":"https://result.example/video.mp4"}]}}}`, `{"code":0,"data":{"task_id":"provider-id","task_status":"failed","task_status_msg":"rejected"}}`},
		{"sora", "", `{"id":"provider-id","status":"completed"}`, `{"id":"provider-id","status":"failed","error":{"message":"rejected"}}`},
		{"sunoapi", "", `{"code":"success","data":[{"task_id":"provider-id","status":"SUCCESS","data":[{"id":"song-id","audio_url":"https://result.example/song.mp3"}]}]}`, `{"code":"success","data":[{"task_id":"provider-id","status":"FAILURE","fail_reason":"rejected","data":[]}]}`},
		{"vidu", "", `{"state":"success","creations":[{"url":"https://result.example/video.mp4"}]}`, `{"state":"failed","err_code":"rejected"}`},
	} {
		for _, failed := range []bool{false, true} {
			t.Run(p.key+"/failure="+strconv.FormatBool(failed), func(t *testing.T) {
				saved, ch := setupAllPluginPoll(t, p.key, p.name)
				calls := 0
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					calls++
					switch p.key {
					case "vertex-ai":
						require.Equal(t, "Bearer local-token", r.Header.Get("Authorization"))
					case "google":
						require.Equal(t, "bound-key", r.Header.Get("x-goog-api-key"))
					case "jimeng":
						var body map[string]any
						require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
						require.Equal(t, "jimeng_t2v_v30_1080p", body["req_key"])
						require.Contains(t, r.Header.Get("Authorization"), "Credential=ak/")
					case "vidu":
						require.Equal(t, "Token bound-key", r.Header.Get("Authorization"))
					case "kling":
						require.True(t, strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ey"))
					default:
						require.Equal(t, "Bearer bound-key", r.Header.Get("Authorization"))
					}
					if failed {
						_, _ = io.WriteString(w, p.failure)
					} else {
						_, _ = io.WriteString(w, p.success)
					}
				}))
				defer srv.Close()
				ch.BaseURL = &srv.URL
				transport := srv.Client().Transport
				executor := host.Executor{Client: &http.Client{Transport: pluginRoundTripper(func(r *http.Request) (*http.Response, error) {
					if r.URL.String() == "https://www.googleapis.com/oauth2/v4/token" {
						return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"access_token":"local-token"}`))}, nil
					}
					return transport.RoundTrip(r)
				})}, ValidateURL: func(raw string) error {
					require.True(t, strings.HasPrefix(raw, srv.URL+"/") || raw == "https://www.googleapis.com/oauth2/v4/token")
					return nil
				}}
				for range 2 {
					copy := *saved
					tasks := map[string]*model.Task{saved.TaskID: &copy}
					if p.key == "sunoapi" {
						require.NoError(t, updatePluginBatchTasksWithExecutor(t.Context(), ch, []string{saved.TaskID}, tasks, executor))
					} else {
						require.NoError(t, updateVideoSingleTaskWithExecutor(t.Context(), ch, saved.TaskID, tasks, executor))
					}
				}
				require.Equal(t, 2, calls)
				var persisted model.Task
				require.NoError(t, model.DB.First(&persisted, saved.ID).Error)
				if failed {
					require.Equal(t, model.TaskStatus(model.TaskStatusFailure), persisted.Status)
					require.Zero(t, persisted.Quota)
					require.Equal(t, 1400, getUserQuota(t, 40))
					require.Equal(t, 1400, getTokenRemainQuota(t, 40))
				} else {
					require.Equal(t, model.TaskStatus(model.TaskStatusSuccess), persisted.Status)
					require.Equal(t, 400, persisted.Quota)
					require.Equal(t, 1000, getUserQuota(t, 40))
					require.Equal(t, 1000, getTokenRemainQuota(t, 40))
					if p.key == "vertex-ai" {
						require.Contains(t, string(persisted.Data), "dmlkZW8=")
					}
					if p.key == "sunoapi" {
						require.JSONEq(t, `[{"id":"song-id","audio_url":"https://result.example/song.mp3"}]`, string(persisted.Data))
						require.NotNil(t, persisted.PrivateData.PluginContext)
						require.Equal(t, float64(1), persisted.PrivateData.PluginContext.CompletionUsage["clips"])
						require.Equal(t, "music", persisted.PrivateData.PluginContext.CompletionUsage["action"])
					}
				}
			})
		}
	}
}

func TestPluginTokenSnapshotBatchResumeMySQL(t *testing.T) {
	task, ch := setupAllPluginPoll(t, "sunoapi", "")
	snapshot, err := NewTaskPluginTokenBilling(types.PriceData{ModelRatio: 0.5, TaskModelRatioConfigured: true, GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 1}}, false)
	require.NoError(t, err)
	tokens := 1000
	task.PrivateData.PluginContext = &model.TaskPluginContext{TokenBilling: snapshot, CompletionTokens: &tokens, CompletionReady: true, BillingRetryAfter: time.Now().Unix(), BillingPendingReason: "settlement_pending"}
	task.Status = model.TaskStatusBillingPending
	require.NoError(t, model.DB.Save(task).Error)
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); w.WriteHeader(500) }))
	defer srv.Close()
	ch.BaseURL = &srv.URL
	for range 2 {
		copy := *task
		require.NoError(t, updatePluginBatchTasksWithExecutor(t.Context(), ch, []string{task.TaskID}, map[string]*model.Task{task.TaskID: &copy}, host.Executor{Client: srv.Client(), ValidateURL: func(string) error { return nil }}))
	}
	require.Zero(t, calls.Load())
	require.Equal(t, 500, reloadCompletion(t, task.ID).Quota)
	require.Equal(t, 900, getUserQuota(t, 40))
	require.Equal(t, 900, getTokenRemainQuota(t, 40))
}
