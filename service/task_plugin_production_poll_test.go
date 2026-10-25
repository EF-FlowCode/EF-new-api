package service

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"eflow.local/new-api/constant"
	"eflow.local/new-api/model"
	"eflow.local/new-api/plugins"
	host "eflow.local/new-api/relay/channel/task/jsplugin"
	management "eflow.local/new-api/service/taskplugin"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestLegacyPluginMigrationDoesNotTimeoutMySQL(t *testing.T) {
	truncate(t)
	oldTimeout := constant.TaskTimeoutMinutes
	constant.TaskTimeoutMinutes = 1
	t.Cleanup(func() { constant.TaskTimeoutMinutes = oldTimeout })
	for _, platform := range []constant.TaskPlatform{"17", "1001"} {
		task := model.Task{TaskID: "migration-" + string(platform), Platform: platform, Status: model.TaskStatusInProgress, SubmitTime: time.Now().Add(-2 * time.Hour).Unix(), Quota: 500}
		require.NoError(t, model.DB.Create(&task).Error)
		sweepTimedOutTasks(t.Context())
		var persisted model.Task
		require.NoError(t, model.DB.First(&persisted, task.ID).Error)
		require.Equal(t, task, persisted, "migration delay must not change status or refund")
	}
	active := model.Task{TaskID: "other-pollable-task", Platform: "29", Status: model.TaskStatusInProgress, SubmitTime: time.Now().Add(-time.Hour).Unix()}
	require.NoError(t, model.DB.Create(&active).Error)
	for _, tasks := range [][]*model.Task{model.GetTimedOutUnfinishedTasks(time.Now().Unix(), 1), model.GetAllUnFinishSyncTasks(1)} {
		require.Len(t, tasks, 1)
		require.Equal(t, active.TaskID, tasks[0].TaskID, "unadopted rows cannot consume the query limit")
	}
}

func TestTaskPluginProductionPollingMySQL(t *testing.T) {
	for _, provider := range []struct {
		key, name string
		channel   int
	}{
		{"alibaba", "wan2.6-t2v", 17}, {"happyhorse", "happyhorse-1.0-t2v", 1001},
	} {
		t.Run(provider.key, func(t *testing.T) { testPluginProductionPolling(t, provider.key, provider.name, provider.channel) })
	}
}

func testPluginProductionPolling(t *testing.T, provider, name string, channelType int) {
	for _, tc := range []struct {
		name, response, status         string
		httpStatus, finalQuota, wallet int
		disabled, wantError            bool
	}{
		{"success", `{"output":{"task_status":"SUCCEEDED","video_url":"https://result.example/video.mp4"}}`, model.TaskStatusSuccess, 200, 400, 1000, false, false},
		{"failure", `{"output":{"task_status":"FAILED","message":"rejected"}}`, model.TaskStatusFailure, 200, 0, 1400, false, false},
		{"unrecognized", `{"output":{"task_status":"NEW_STATE"}}`, model.TaskStatusInProgress, 200, 400, 1000, false, true},
		{"non_2xx", `{"output":{"task_status":"FAILED"}}`, model.TaskStatusInProgress, 503, 400, 1000, false, true},
		{"disabled", `{}`, model.TaskStatusInProgress, 200, 400, 1000, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if provider == "happyhorse" && tc.name == "unrecognized" {
				tc.status, tc.wantError = model.TaskStatusQueued, false
			}
			truncate(t)
			require.NoError(t, model.DB.AutoMigrate(&model.QuotaData{}, &model.TaskPlugin{}, &model.TaskPluginControl{}, &model.TaskPluginShadowBinding{}, &model.TaskPluginProductionBinding{}, &model.TaskPluginSourceSnapshot{}, &model.TaskPluginTaskReference{}))
			seedUser(t, 40, 1000)
			seedToken(t, 40, 40, "poll-test-token", 1000)
			seedChannel(t, 40)
			require.NoError(t, model.DB.Create(&model.TaskPluginControl{Id: 1, Revision: 1, Enabled: true, FactoryEnabled: true, DisabledFactory: "[]"}).Error)
			source, err := plugins.Source(provider)
			require.NoError(t, err)
			h, err := host.Compile(source)
			require.NoError(t, err)
			store := model.SQLTaskPluginStore{DB: func() *gorm.DB { return model.DB }}
			manager := management.NewManager(store, map[string]string{provider: source})
			require.NoError(t, model.DB.Model(&model.Channel{}).Where("id = 40").Update("type", channelType).Error)
			require.NoError(t, manager.BindProduction(t.Context(), 40, management.ProductionBindingRequest{Key: provider, Version: h.Identity().Version, Factory: true, Enabled: true}))
			task := makeTask(40, 40, 400, 40)
			task.Platform = constant.TaskPlatform(strconv.Itoa(channelType))
			task.PrivateData.UpstreamTaskID = "provider-id"
			task.PrivateData.Key = "bound-task-key"
			task.Properties.OriginModelName, task.Properties.UpstreamModelName = name, name
			saved, err := manager.CreatePluginTask(t.Context(), *task, management.TaskVersion{Key: provider, Version: h.Identity().Version, Factory: true})
			require.NoError(t, err)
			// Restart without embedded factory code: persisted source is sufficient.
			oldManager := management.Default
			management.Default = management.NewManager(store, nil)
			t.Cleanup(func() { management.Default = oldManager })
			if tc.disabled {
				require.NoError(t, model.DB.Model(&model.TaskPluginControl{}).Where("id = 1").Update("enabled", false).Error)
			}
			var calls atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.Header.Get("Authorization") != "Bearer bound-task-key" || r.URL.Path != "/api/v1/tasks/provider-id" {
					w.WriteHeader(400)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.httpStatus)
				_, _ = io.WriteString(w, tc.response)
			}))
			defer srv.Close()
			ch := &model.Channel{Id: 40, Type: channelType, Key: "different-channel-key", BaseURL: &srv.URL}
			executor := host.Executor{Client: srv.Client(), ValidateURL: func(raw string) error { require.Equal(t, srv.URL+"/api/v1/tasks/provider-id", raw); return nil }}
			// Two workers holding the same pre-terminal snapshot: only one CAS
			// may apply a terminal refund. Unknown states must leave SQL unchanged.
			for range 2 {
				copy := *saved
				err := updateVideoSingleTaskWithExecutor(t.Context(), ch, "provider-id", map[string]*model.Task{"provider-id": &copy}, executor)
				if tc.wantError {
					require.Error(t, err)
				} else {
					require.NoError(t, err)
				}
			}
			if tc.disabled {
				require.Zero(t, calls.Load())
			} else {
				require.EqualValues(t, 2, calls.Load())
			}
			var persisted model.Task
			require.NoError(t, model.DB.First(&persisted, saved.ID).Error)
			require.Equal(t, model.TaskStatus(tc.status), persisted.Status)
			require.Equal(t, tc.finalQuota, persisted.Quota)
			require.Equal(t, tc.wallet, getUserQuota(t, 40))
			require.Equal(t, tc.wallet, getTokenRemainQuota(t, 40))
			if tc.name == "success" {
				require.Equal(t, "https://result.example/video.mp4", persisted.PrivateData.ResultURL)
			}
		})
	}
}
