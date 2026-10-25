package service

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"eflow.local/new-api/model"
	"eflow.local/new-api/plugins"
	host "eflow.local/new-api/relay/channel/task/jsplugin"
	management "eflow.local/new-api/service/taskplugin"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestPluginSunoBatchIsolationMySQL(t *testing.T) {
	first, ch := setupAllPluginPoll(t, "sunoapi", "")
	require.NoError(t, model.DB.Create(&model.User{Id: 41, Username: "other-user", AffCode: "other-code", Quota: 1000}).Error)
	seedToken(t, 41, 41, "other-user-token", 1000)
	source, err := plugins.Source("sunoapi")
	require.NoError(t, err)
	m := management.NewManager(model.SQLTaskPluginStore{DB: func() *gorm.DB { return model.DB }}, map[string]string{"sunoapi": source})
	tasks := []*model.Task{first}
	for i := range 3 {
		copy := *first
		copy.ID = 0
		copy.TaskID = model.GenerateTaskID()
		copy.PrivateData.Plugin = nil
		switch i {
		case 0:
			copy.PrivateData.UpstreamTaskID = "second-id"
		case 1:
			copy.UserId = 41
			copy.PrivateData.TokenId = 41
		case 2:
			copy.PrivateData.Key = "other-key"
		}
		saved, err := m.CreatePluginTask(t.Context(), copy, management.TaskVersion{Key: "sunoapi", Version: first.PrivateData.Plugin.Version, Factory: true})
		require.NoError(t, err)
		tasks = append(tasks, saved)
	}
	var groups []int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/suno/fetch", r.URL.Path)
		var body struct {
			IDs []string `json:"ids"`
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		groups = append(groups, len(body.IDs))
		if len(body.IDs) == 2 {
			require.Equal(t, "Bearer bound-key", r.Header.Get("Authorization"))
		}
		var rows []any
		for i := len(body.IDs) - 1; i >= 0; i-- {
			rows = append(rows, map[string]any{"task_id": body.IDs[i], "status": "FAILURE", "fail_reason": "rejected", "submit_time": 123, "start_time": 124, "finish_time": 125, "data": []any{}})
		}
		require.NoError(t, json.NewEncoder(w).Encode(map[string]any{"code": "success", "data": rows}))
	}))
	defer srv.Close()
	ch.BaseURL = &srv.URL
	executor := host.Executor{Client: srv.Client(), ValidateURL: func(raw string) error { require.Equal(t, srv.URL+"/suno/fetch", raw); return nil }}
	for range 2 {
		ids := []string{}
		copies := map[string]*model.Task{}
		for _, task := range tasks {
			copy := *task
			copies[copy.TaskID] = &copy
			ids = append(ids, copy.TaskID)
		}
		require.NoError(t, updatePluginBatchTasksWithExecutor(t.Context(), ch, ids, copies, executor))
	}
	require.ElementsMatch(t, []int{2, 1, 1, 2, 1, 1}, groups, "separate user/key identities cannot share a batch even with repeated upstream IDs")
	require.Equal(t, 2200, getUserQuota(t, 40))
	require.Equal(t, 2200, getTokenRemainQuota(t, 40))
	require.Equal(t, 1400, getUserQuota(t, 41))
	require.Equal(t, 1400, getTokenRemainQuota(t, 41))
	for _, task := range tasks {
		var persisted model.Task
		require.NoError(t, model.DB.First(&persisted, task.ID).Error)
		require.EqualValues(t, 125, persisted.FinishTime)
		require.EqualValues(t, 123, persisted.SubmitTime)
		require.Equal(t, model.TaskStatus(model.TaskStatusFailure), persisted.Status)
	}
}
