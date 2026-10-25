package taskplugin

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"eflow.local/new-api/internal/testdb"
	"eflow.local/new-api/model"
	"eflow.local/new-api/plugins"
	host "eflow.local/new-api/relay/channel/task/jsplugin"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func taskQuerySQLStore(t *testing.T) model.SQLTaskPluginStore {
	t.Helper()
	db := testdb.OpenMySQL8(t, &model.Task{}, &model.TaskPlugin{}, &model.TaskPluginControl{}, &model.TaskPluginShadowBinding{}, &model.TaskPluginSourceSnapshot{}, &model.TaskPluginTaskReference{}, &model.TaskPluginProductionBinding{})
	require.NoError(t, db.Create(&model.TaskPluginControl{Id: 1, Revision: 1, Enabled: true, FactoryEnabled: true, DisabledFactory: "[]"}).Error)
	return model.SQLTaskPluginStore{DB: func() *gorm.DB { return db }}
}

func queryTask(id, name string) model.Task {
	return model.Task{TaskID: "public-" + id, UserId: 7, ChannelId: 17, Status: model.TaskStatusSubmitted, Quota: 400, Properties: model.Properties{OriginModelName: name, UpstreamModelName: name}, PrivateData: model.TaskPrivateData{UpstreamTaskID: id, Key: "test-ak|test-sk", TokenId: 3, BillingContext: &model.TaskBillingContext{ModelPrice: 0.2, GroupRatio: 1}}}
}

func queryEnvironment() map[int]TaskQueryEnvironment {
	return map[int]TaskQueryEnvironment{17: {BaseURL: "https://provider.example", AuthHeader: "Bearer prepared-test-token", Auth: map[string]any{"authHeader": "Bearer prepared-test-token", "projectId": "test-project"}}}
}

func TestTaskPluginQueryStorageUnavailable(t *testing.T) {
	source, err := plugins.Source("alibaba")
	require.NoError(t, err)
	m := NewManager(model.SQLTaskPluginStore{}, map[string]string{"alibaba": source})
	plan, err := m.PrepareTaskQueries(t.Context(), []int64{1}, queryEnvironment())
	require.ErrorIs(t, err, ErrUnavailable)
	require.Nil(t, plan, "no partial query plan escapes storage failure")
	task := queryTask("unavailable", "wan2.6-t2v")
	saved, err := m.CreatePluginTask(t.Context(), task, TaskVersion{Key: "alibaba", Version: "1.3.0", Factory: true})
	require.ErrorIs(t, err, ErrUnavailable)
	require.Nil(t, saved)
	require.Nil(t, task.PrivateData.Plugin)
	require.Equal(t, 400, task.Quota)
	require.Equal(t, model.TaskStatus(model.TaskStatusSubmitted), task.Status)
}

func TestTaskPluginQueriesAllTenPersistAndRestoreMySQL(t *testing.T) {
	s := taskQuerySQLStore(t)
	factories := map[string]string{}
	keys := []string{"alibaba", "doubao", "google", "hailuo", "jimeng", "kling", "sora", "sunoapi", "vertex-ai", "vidu"}
	for _, key := range keys {
		source, err := plugins.Source(key)
		require.NoError(t, err)
		factories[key] = source
	}
	creator := NewManager(s, factories)
	// No factory code at all in the new manager: restoration must use MySQL.
	restarted := NewManager(s, nil)
	var ids []int64
	for _, key := range keys {
		t.Run(key, func(t *testing.T) {
			h, err := host.Compile(factories[key])
			require.NoError(t, err)
			name := h.Meta().Models[0]
			if key == "alibaba" {
				name = "wan2.6-t2v"
			}
			input := map[string]any{"model": name, "upstreamModel": name, "baseUrl": "https://provider.example", "action": "text_to_video"}
			ack := map[string]any{"task_id": "upstream-" + key, "id": "upstream-" + key, "output": map[string]any{"task_id": "upstream-" + key}, "data": map[string]any{"task_id": "upstream-" + key}, "code": 0, "base_resp": map[string]any{"status_code": 0}, "name": "projects/test-project/locations/us-central1/publishers/google/models/veo-3.0-generate-001/operations/test-task"}
			if key == "jimeng" {
				ack["code"] = 10000
			}
			if key == "sunoapi" {
				ack["code"] = "success"
				ack["data"] = "upstream-sunoapi"
			}
			parsed, err := h.ParseSubmit(t.Context(), input, host.Response{Status: 200, Body: ack})
			require.NoError(t, err)
			task := queryTask(parsed["taskId"].(string), name)
			task.TaskID = "public-" + key
			task.Action = "text_to_video"
			saved, err := creator.CreatePluginTask(t.Context(), task, TaskVersion{Key: key, Version: h.Identity().Version, Factory: true})
			require.NoError(t, err)
			ids = append(ids, saved.ID)
			plan, err := restarted.PrepareTaskQueries(t.Context(), []int64{saved.ID}, queryEnvironment())
			require.NoError(t, err)
			require.Len(t, plan.Queries, 1)
			require.Empty(t, plan.LegacyTaskIDs)
			d := plan.Queries[0].Descriptor()
			require.NotEmpty(t, d.URL)
			require.Equal(t, h.Identity().SourceHash, plan.Queries[0].members[0].identity.SourceHash)
			d.Headers["Authorization"] = "caller-mutated"
			require.NotEqual(t, "caller-mutated", plan.Queries[0].Descriptor().Headers["Authorization"])
			var persisted model.Task
			require.NoError(t, s.DB().First(&persisted, saved.ID).Error)
			require.Equal(t, task.Status, persisted.Status)
			require.Equal(t, task.Quota, persisted.Quota)
			require.Equal(t, task.PrivateData.BillingContext, persisted.PrivateData.BillingContext)
		})
	}
	legacy := queryTask("legacy", "legacy-model")
	require.NoError(t, s.DB().Create(&legacy).Error)
	ids = append(ids, legacy.ID)
	plan, err := restarted.PrepareTaskQueries(t.Context(), ids, queryEnvironment())
	require.NoError(t, err)
	require.Len(t, plan.Queries, 10)
	require.Equal(t, []int64{legacy.ID}, plan.LegacyTaskIDs)
}

func TestTaskPluginQueriesVersionSwitchesAndCandidatesMySQL(t *testing.T) {
	s := taskQuerySQLStore(t)
	source, err := plugins.Source("alibaba")
	require.NoError(t, err)
	m := NewManager(s, map[string]string{"alibaba": source})
	old, err := m.CreatePluginTask(t.Context(), queryTask("factory-task", "wan2.6-t2v"), TaskVersion{Key: "alibaba", Version: "1.3.0", Factory: true})
	require.NoError(t, err)
	for _, version := range []string{"9.0.0", "10.0.0"} {
		_, err = m.Upload(t.Context(), UploadRequest{Source: strings.Replace(source, `version: "1.3.0"`, `version: "`+version+`"`, 1)})
		require.NoError(t, err)
	}
	saved, err := m.CreatePluginTask(t.Context(), queryTask("override-task", "wan2.6-t2v"), TaskVersion{Key: "alibaba", Version: "9.0.0"})
	require.NoError(t, err)
	ids := []int64{old.ID, saved.ID}
	require.NoError(t, m.Activate(t.Context(), "alibaba", "10.0.0"))
	_, err = m.Upload(t.Context(), UploadRequest{Source: strings.Replace(source, `version: "1.3.0"`, `version: "9.0.0"`, 1), Enabled: flag(false)})
	require.NoError(t, err)
	_, err = m.PrepareTaskQueries(t.Context(), []int64{saved.ID}, queryEnvironment())
	require.ErrorIs(t, err, model.ErrTaskPluginDisabled, "disabling an inactive but referenced version pauses its own tasks")
	_, err = m.Upload(t.Context(), UploadRequest{Source: strings.Replace(source, `version: "1.3.0"`, `version: "9.0.0"`, 1), Enabled: flag(true)})
	require.NoError(t, err)
	plan, err := m.PrepareTaskQueries(t.Context(), ids, queryEnvironment())
	require.NoError(t, err)
	require.Len(t, plan.Queries, 2)
	require.Equal(t, "9.0.0", plan.Queries[1].members[0].identity.Version)
	require.ErrorIs(t, m.Delete(t.Context(), "alibaba", "9.0.0"), ErrConflict)
	response := host.Response{Status: 200, Body: map[string]any{"output": map[string]any{"task_status": "SUCCEEDED", "video_url": "https://result.example/video"}}}
	candidates, err := plan.Queries[1].Parse(t.Context(), response)
	require.NoError(t, err)
	require.Equal(t, saved.ID, candidates[0].TaskID)
	require.Equal(t, "SUCCESS", candidates[0].Result["status"])
	var persisted model.Task
	require.NoError(t, s.DB().First(&persisted, saved.ID).Error)
	require.Equal(t, model.TaskStatus(model.TaskStatusSubmitted), persisted.Status)
	require.Equal(t, 400, persisted.Quota, "a candidate cannot settle quota or change status")

	require.NoError(t, m.SetStatus(t.Context(), "alibaba", false))
	_, err = plan.Queries[1].Parse(t.Context(), response)
	require.ErrorIs(t, err, model.ErrTaskPluginDisabled)
	_, err = m.PrepareTaskQueries(t.Context(), ids, queryEnvironment())
	require.ErrorIs(t, err, model.ErrTaskPluginDisabled)
	require.NoError(t, m.SetStatus(t.Context(), "alibaba", true))
	require.NoError(t, m.Activate(t.Context(), "alibaba", "9.0.0"))
	// The already prepared query remains bound across rollback.
	_, err = plan.Queries[1].Parse(t.Context(), response)
	require.NoError(t, err)
	require.NoError(t, m.Configure(t.Context(), Settings{Enabled: flag(false)}))
	_, err = plan.Queries[0].Parse(t.Context(), response)
	require.ErrorIs(t, err, model.ErrTaskPluginDisabled)
	require.NoError(t, m.Configure(t.Context(), Settings{Enabled: flag(true), FactoryEnabled: flag(false)}))
	_, err = m.PrepareTaskQueries(t.Context(), []int64{old.ID}, queryEnvironment())
	require.ErrorIs(t, err, model.ErrTaskPluginDisabled)
	_, err = m.PrepareTaskQueries(t.Context(), []int64{saved.ID}, queryEnvironment())
	require.NoError(t, err, "factory switch must not disable overrides")
	require.NoError(t, m.Configure(t.Context(), Settings{FactoryEnabled: flag(true)}))

	// Updating the embedded source is irrelevant to an already persisted task.
	m = NewManager(s, map[string]string{"alibaba": strings.Replace(source, `version: "1.3.0"`, `version: "12.0.0"`, 1)})
	plan, err = m.PrepareTaskQueries(t.Context(), ids, queryEnvironment())
	require.NoError(t, err)
	require.Equal(t, "1.3.0", plan.Queries[0].members[0].identity.Version)
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = m.PrepareTaskQueries(canceled, ids, queryEnvironment())
	require.ErrorIs(t, err, context.Canceled)
}

func TestTaskPluginQueriesBatchIsolationMySQL(t *testing.T) {
	s := taskQuerySQLStore(t)
	source, err := plugins.Source("sunoapi")
	require.NoError(t, err)
	m := NewManager(s, map[string]string{"sunoapi": source})
	_, err = m.Upload(t.Context(), UploadRequest{Source: strings.Replace(source, `version: "1.0.3"`, `version: "9.0.0"`, 1)})
	require.NoError(t, err)
	var ids []int64
	for n, id := range []string{"one", "two", "other-version", "other-user", "other-key"} {
		task := queryTask(id, "suno_music")
		task.Action = "music"
		version := TaskVersion{Key: "sunoapi", Version: "1.0.3", Factory: true}
		if n == 2 {
			version = TaskVersion{Key: "sunoapi", Version: "9.0.0"}
		}
		if n == 3 {
			task.UserId++
		}
		if n == 4 {
			task.PrivateData.Key = "different-key"
		}
		saved, err := m.CreatePluginTask(t.Context(), task, version)
		require.NoError(t, err)
		ids = append(ids, saved.ID)
	}
	env := queryEnvironment()
	plan, err := m.PrepareTaskQueries(t.Context(), ids, env)
	require.NoError(t, err)
	require.Len(t, plan.Queries, 4)
	q := plan.Queries[0]
	require.Equal(t, ids[:2], q.TaskIDs())
	require.Equal(t, []any{"one", "two"}, q.Descriptor().Body.(map[string]any)["ids"])
	env[17].Auth["projectId"] = "modified-after-prepare"
	body := map[string]any{"code": "success", "data": []any{map[string]any{"task_id": "two", "status": "IN_PROGRESS"}, map[string]any{"task_id": "one", "status": "SUCCESS"}}}
	response := host.Response{Status: 200, Body: body}
	candidates, err := q.Parse(t.Context(), response)
	require.NoError(t, err)
	require.Equal(t, ids[1], candidates[0].TaskID, "response order must not decide task ownership")
	require.Equal(t, ids[0], candidates[1].TaskID)
	body["data"] = []any{map[string]any{"task_id": "other-user", "status": "SUCCESS"}}
	_, err = q.Parse(t.Context(), response)
	require.ErrorIs(t, err, ErrTaskQueryHook)
	body["data"] = []any{map[string]any{"task_id": "one", "status": "SUCCESS"}, map[string]any{"task_id": "one", "status": "SUCCESS"}}
	_, err = q.Parse(t.Context(), response)
	require.ErrorIs(t, err, ErrTaskQueryHook)
	body["data"] = []any{map[string]any{"task_id": "one", "status": "SUCCESS"}}
	// Persisted identity mismatch is rejected before the script can interpret data.
	var task model.Task
	require.NoError(t, s.DB().First(&task, ids[0]).Error)
	task.PrivateData.Plugin.SourceHash = strings.Repeat("a", 64)
	raw, err := json.Marshal(task.PrivateData)
	require.NoError(t, err)
	require.NoError(t, s.DB().Model(&task).Update("private_data", string(raw)).Error)
	_, err = q.Parse(t.Context(), response)
	require.ErrorIs(t, err, model.ErrTaskPluginIdentity)
}
