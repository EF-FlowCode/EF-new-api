package service

import (
	"encoding/json"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"eflow.local/new-api/model"
	host "eflow.local/new-api/relay/channel/task/jsplugin"
	relaycommon "eflow.local/new-api/relay/common"
	management "eflow.local/new-api/service/taskplugin"
	"eflow.local/new-api/setting/ratio_setting"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func completionTask(t *testing.T, name string) (*model.Task, *model.Channel) {
	t.Helper()
	task, ch := setupAllPluginPoll(t, "alibaba", name)
	p, err := NewTaskPluginBillingSnapshot("alibaba", name, 100, false, true)
	require.NoError(t, err)
	task.PrivateData.PluginContext = &model.TaskPluginContext{Billing: p, Usage: map[string]any{"resolution": "480P", "seconds": float64(4)}}
	require.NoError(t, model.DB.Save(task).Error)
	return task, ch
}

func reloadCompletion(t *testing.T, id int64) *model.Task {
	t.Helper()
	var task model.Task
	require.NoError(t, model.DB.First(&task, id).Error)
	return &task
}

func TestWANCompletionQuota(t *testing.T) {
	for _, name := range []string{"wan2.2-s2v", "wan3.0-video", "wan3.0-video-prime"} {
		_, err := NewTaskPluginBillingSnapshot("alibaba", name, 100, false, false)
		require.Error(t, err, "token ratio is not a seconds price")
	}
	for _, tc := range []struct {
		name    string
		seconds any
		invalid bool
		want    int
		reason  string
	}{
		{"actual", 2.5, false, 450, ""},
		{"missing", nil, false, 0, "missing_completion_usage"},
		{"string", "2", false, 0, "missing_completion_usage"},
		{"zero", float64(0), false, 0, "invalid_completion_usage"},
		{"negative", float64(-1), false, 0, "invalid_completion_usage"},
		{"nan", math.NaN(), false, 0, "invalid_completion_usage"},
		{"infinite", math.Inf(1), false, 0, "invalid_completion_usage"},
		{"excess", float64(3601), false, 0, "invalid_completion_usage"},
		{"schema", float64(2), true, 0, "invalid_completion_usage"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, err := NewTaskPluginBillingSnapshot("alibaba", "wan2.2-s2v", 100, false, true)
			require.NoError(t, err)
			task := &model.Task{PrivateData: model.TaskPrivateData{PluginContext: &model.TaskPluginContext{Billing: p, Usage: map[string]any{"resolution": "720P"}, CompletionUsage: map[string]any{"seconds": tc.seconds}, CompletionUsageInvalid: tc.invalid}}}
			quota, reason := pluginCompletionQuota(task)
			require.Equal(t, tc.want, quota)
			require.Equal(t, tc.reason, reason)
			p.Mode = "fixed"
			quota, reason = pluginCompletionQuota(task)
			require.Equal(t, 100, quota)
			require.Empty(t, reason)
		})
	}
}

func TestWANCompletionPollingMySQL(t *testing.T) {
	for _, tc := range []struct {
		name, model, usage, mode string
		want                     int
		pending                  string
	}{
		{"refund", "wan2.2-s2v", `{"duration":2}`, "duration", 200, ""},
		{"debit", "wan2.2-s2v", `{"duration":6}`, "duration", 600, ""},
		{"wan3_sum", "wan3.0-video", `{"input_video_duration":2,"output_video_duration":3}`, "duration", 500, ""},
		{"prime_resolution", "wan3.0-video-prime", `{"input_video_duration":0,"duration":2,"SR":"720P"}`, "duration", 400, ""},
		{"partial", "wan3.0-video", `{"output_video_duration":3}`, "duration", 400, "missing_completion_usage"},
		{"missing", "wan2.2-s2v", `{}`, "duration", 400, "missing_completion_usage"},
		{"illegal", "wan2.2-s2v", `{"duration":-1}`, "duration", 400, "invalid_completion_usage"},
		{"fixed", "wan2.2-s2v", `{}`, "fixed", 400, ""},
		{"historical", "wan2.2-s2v", `{"duration":2}`, "historical", 400, "missing_price_snapshot"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			task, ch := completionTask(t, tc.model)
			if tc.mode == "fixed" {
				task.PrivateData.PluginContext.Billing.Mode = "fixed"
				task.PrivateData.PluginContext.Billing.BaseQuota = 400
			}
			if tc.mode == "historical" {
				task.PrivateData.PluginContext.Billing = nil
			}
			require.NoError(t, model.DB.Save(task).Error)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.WriteString(w, `{"output":{"task_status":"SUCCEEDED","video_url":"https://result.example/video.mp4"},"usage":`+tc.usage+`}`)
			}))
			defer srv.Close()
			ch.BaseURL = &srv.URL
			executor := host.Executor{Client: srv.Client(), ValidateURL: func(string) error { return nil }}
			for range 2 {
				copy := *task
				require.NoError(t, updateVideoSingleTaskWithExecutor(t.Context(), ch, task.TaskID, map[string]*model.Task{task.TaskID: &copy}, executor))
			}
			saved := reloadCompletion(t, task.ID)
			require.Equal(t, tc.want, saved.Quota)
			require.Equal(t, 1400-tc.want, getUserQuota(t, 40))
			require.Equal(t, 1400-tc.want, getTokenRemainQuota(t, 40))
			require.Equal(t, "https://result.example/video.mp4", saved.PrivateData.ResultURL)
			if tc.pending == "" {
				require.Equal(t, model.TaskStatus(model.TaskStatusSuccess), saved.Status)
			} else {
				require.Equal(t, model.TaskStatus(model.TaskStatusBillingPending), saved.Status)
				require.Equal(t, tc.pending, saved.PrivateData.PluginContext.BillingPendingReason)
				require.Empty(t, model.GetTimedOutUnfinishedTasks(time.Now().Add(time.Hour).Unix(), 100))
				require.Empty(t, model.GetAllUnFinishSyncTasks(1))
			}
		})
	}
}

func TestWANCompletionRetryMySQL(t *testing.T) {
	oldPrices := ratio_setting.ModelPrice2JSONString()
	t.Cleanup(func() { require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(oldPrices)) })
	for _, insufficient := range []string{"user", "token"} {
		t.Run(insufficient, func(t *testing.T) {
			task, ch := completionTask(t, "wan2.2-s2v")
			// Task billing allows negative wallet and token balances. Inject a real
			// write failure on one side instead of relying on a balance floor.
			failingTable := map[string]string{"user": "users", "token": "tokens"}[insufficient]
			require.NoError(t, model.DB.Exec("CREATE TRIGGER completion_write_failure BEFORE UPDATE ON "+failingTable+" FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'injected write failure'").Error)
			var calls atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				_, _ = io.WriteString(w, `{"output":{"task_status":"SUCCEEDED","video_url":"https://result.example/video.mp4"},"usage":{"duration":6}}`)
			}))
			defer srv.Close()
			ch.BaseURL = &srv.URL
			executor := host.Executor{Client: srv.Client(), ValidateURL: func(string) error { return nil }}
			poll := func(task *model.Task) error {
				return updateVideoSingleTaskWithExecutor(t.Context(), ch, task.TaskID, map[string]*model.Task{task.TaskID: task}, executor)
			}
			require.Error(t, poll(task))
			saved := reloadCompletion(t, task.ID)
			require.Equal(t, model.TaskStatus(model.TaskStatusBillingPending), saved.Status)
			require.True(t, saved.PrivateData.PluginContext.CompletionReady)
			require.Equal(t, 400, saved.Quota)
			// The other half of the wallet/token transaction was rolled back.
			if insufficient == "user" {
				require.Equal(t, 1000, getTokenRemainQuota(t, 40))
			} else {
				require.Equal(t, 1000, getUserQuota(t, 40))
			}
			// Recreate the manager from persistent sources, with factory unavailable.
			management.Default = management.NewManager(model.SQLTaskPluginStore{DB: func() *gorm.DB { return model.DB }}, nil)
			require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(`{"wan2.2-s2v":999}`))
			require.NoError(t, model.DB.Exec("DROP TRIGGER completion_write_failure").Error)
			require.NoError(t, model.DB.Model(&model.User{}).Where("id = 40").Update("quota", 1000).Error)
			require.NoError(t, model.DB.Model(&model.Token{}).Where("id = 40").Update("remain_quota", 1000).Error)
			require.NoError(t, model.DB.Model(&model.TaskPluginControl{}).Where("id = 1").Update("enabled", false).Error)
			require.Error(t, poll(saved), "paused plugin must not resume settlement")
			require.Empty(t, model.GetAllUnFinishSyncTasks(1), "paused pending tasks also back off")
			require.NoError(t, model.DB.Model(&model.TaskPluginControl{}).Where("id = 1").Update("enabled", true).Error)
			// Saved success cannot be overwritten by a stale incomplete response.
			copy := *saved
			require.NoError(t, checkpointPluginCompletion(t.Context(), &copy, model.TaskStatusBillingPending, &relaycommon.TaskInfo{}))
			require.True(t, reloadCompletion(t, task.ID).PrivateData.PluginContext.CompletionReady)
			var wg sync.WaitGroup
			errs := make(chan error, 2)
			for range 2 {
				copy := *saved
				wg.Add(1)
				go func() { defer wg.Done(); errs <- poll(&copy) }()
			}
			wg.Wait()
			close(errs)
			for err := range errs {
				require.NoError(t, err)
			}
			require.EqualValues(t, 1, calls.Load(), "local retries never requery or resubmit provider")
			require.Equal(t, 800, getUserQuota(t, 40))
			require.Equal(t, 800, getTokenRemainQuota(t, 40))
			require.Equal(t, 600, reloadCompletion(t, task.ID).Quota)
		})
	}
}

func TestWANMissingUsageRecoveryMySQL(t *testing.T) {
	task, ch := completionTask(t, "wan3.0-video")
	stale := *task
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		usage := map[string]any{"output_video_duration": 3}
		if calls.Add(1) > 1 {
			usage["input_video_duration"] = 2
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"output": map[string]any{"task_status": "SUCCEEDED", "video_url": "https://result.example/video.mp4"}, "usage": usage})
	}))
	defer srv.Close()
	ch.BaseURL = &srv.URL
	executor := host.Executor{Client: srv.Client(), ValidateURL: func(string) error { return nil }}
	require.NoError(t, updateVideoSingleTaskWithExecutor(t.Context(), ch, task.TaskID, map[string]*model.Task{task.TaskID: task}, executor))
	saved := reloadCompletion(t, task.ID)
	require.Equal(t, model.TaskStatus(model.TaskStatusBillingPending), saved.Status)
	stale.Status = model.TaskStatusFailure
	won, err := FinalizeTaskWithBilling(t.Context(), &stale, model.TaskStatusInProgress, 0, "stale timeout")
	require.NoError(t, err)
	require.False(t, won)
	copy := *saved
	copy.Status = model.TaskStatusFailure
	_, err = FinalizeTaskWithBilling(t.Context(), &copy, model.TaskStatusBillingPending, 0, "invalid refund")
	require.Error(t, err)
	// Retry filtering occurs before LIMIT, so a deferred task cannot starve others.
	other := makeTask(40, 40, 10, 40)
	other.TaskID = "another-task"
	require.NoError(t, model.DB.Create(other).Error)
	ready := model.GetAllUnFinishSyncTasks(1)
	require.Len(t, ready, 1)
	require.Equal(t, other.ID, ready[0].ID)
	require.NoError(t, model.DeferPluginBillingRetry(t.Context(), task.ID, 1))
	require.NoError(t, updateVideoSingleTaskWithExecutor(t.Context(), ch, saved.TaskID, map[string]*model.Task{saved.TaskID: saved}, executor))
	require.Equal(t, model.TaskStatus(model.TaskStatusSuccess), reloadCompletion(t, task.ID).Status)
	require.Equal(t, 900, getUserQuota(t, 40))
	require.EqualValues(t, 2, calls.Load())
}
