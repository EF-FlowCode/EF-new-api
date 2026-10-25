package service

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"eflow.local/new-api/constant"
	"eflow.local/new-api/internal/testconfig"
	"eflow.local/new-api/model"
	host "eflow.local/new-api/relay/channel/task/jsplugin"
	"github.com/stretchr/testify/require"
)

func TestTaskPollingLoopFinalizesVideoTaskBillingAtomically(t *testing.T) {
	task, _ := setupAllPluginPoll(t, "alibaba", "wan2.6-t2v")
	task.SubmitTime = time.Now().Unix()
	require.NoError(t, model.DB.Save(task).Error)
	originalLimit := constant.TaskQueryLimit
	constant.TaskQueryLimit = 1
	t.Cleanup(func() { constant.TaskQueryLimit = originalLimit })
	testconfig.Set(t, "fetch_setting", map[string]string{"enable_ssrf_protection": "false"})
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer cancel()
		_, _ = io.WriteString(w, `{"output":{"task_status":"FAILED","message":"upstream failed"}}`)
	}))
	defer srv.Close()
	originalClient := httpClient
	httpClient = srv.Client()
	t.Cleanup(func() { httpClient = originalClient })
	require.NoError(t, model.DB.Model(&model.Channel{}).Where("id = 40").Update("base_url", srv.URL).Error)
	TaskPollingLoop(ctx, time.Millisecond)
	var saved model.Task
	require.NoError(t, model.DB.First(&saved, task.ID).Error)
	require.EqualValues(t, model.TaskStatusFailure, saved.Status)
	require.Zero(t, saved.Quota)
	require.Equal(t, 1400, getUserQuota(t, 40))
	require.Equal(t, 1400, getTokenRemainQuota(t, 40))
}

func TestUnadoptedTaskCannotReachProvider(t *testing.T) {
	task := &model.Task{TaskID: "legacy", Status: model.TaskStatusInProgress, Quota: 400}
	ch := &model.Channel{Id: 17, Type: 17}
	err := updateVideoSingleTaskWithExecutor(t.Context(), ch, task.TaskID, map[string]*model.Task{task.TaskID: task}, host.Executor{ValidateURL: func(string) error { t.Fatal("unadopted task reached HTTP"); return nil }})
	require.ErrorContains(t, err, "explicit plugin identity migration")
	require.EqualValues(t, model.TaskStatusInProgress, task.Status)
	require.Equal(t, 400, task.Quota)
}
