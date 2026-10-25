package taskplugin

import (
	"errors"
	"testing"

	"eflow.local/new-api/model"
	"eflow.local/new-api/plugins"
	host "eflow.local/new-api/relay/channel/task/jsplugin"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestLegacyTaskAdoptionAndPauseMySQL(t *testing.T) {
	s := taskQuerySQLStore(t)
	require.NoError(t, s.DB().AutoMigrate(&model.Channel{}))
	require.NoError(t, s.DB().Create(&model.Channel{Id: 58, Type: 1001, Name: "happyhorse"}).Error)
	source, err := plugins.Source("happyhorse")
	require.NoError(t, err)
	m := NewManager(s, map[string]string{"happyhorse": source})
	bind := ProductionBindingRequest{Key: "happyhorse", Version: "1.0.0", Factory: true, Enabled: true}
	require.NoError(t, m.BindProduction(t.Context(), 58, bind))
	newTask := func(id string) model.Task {
		task := queryTask(id, "happyhorse-1.0-t2v")
		task.ChannelId, task.Platform = 58, "1001"
		require.NoError(t, s.DB().Create(&task).Error)
		return task
	}
	legacy := newTask("adopt")
	saved, err := m.AdoptLegacyTask(t.Context(), legacy.ID)
	require.NoError(t, err)
	identity := saved.PrivateData.Plugin
	require.NotNil(t, identity)
	saved.PrivateData.Plugin = nil
	require.Equal(t, legacy, *saved, "adoption preserves every business field")
	saved, err = m.AdoptLegacyTask(t.Context(), legacy.ID)
	require.NoError(t, err)
	require.Equal(t, identity, saved.PrivateData.Plugin)
	var count int64
	require.NoError(t, s.DB().Model(&model.TaskPluginTaskReference{}).Count(&count).Error)
	require.EqualValues(t, 1, count)
	require.ErrorIs(t, m.UnbindProduction(t.Context(), 58), ErrConflict)
	// A failed reference insert must roll back the private identity update.
	failing := newTask("rollback")
	require.NoError(t, s.DB().Callback().Create().Before("gorm:create").Register("adopt_reference_failure", func(tx *gorm.DB) {
		if tx.Statement.Table == "task_plugin_task_references" {
			tx.AddError(errors.New("reference failure"))
		}
	}))
	_, err = m.AdoptLegacyTask(t.Context(), failing.ID)
	require.ErrorIs(t, err, ErrUnavailable)
	require.NoError(t, s.DB().Callback().Create().Remove("adopt_reference_failure"))
	var persisted model.Task
	require.NoError(t, s.DB().First(&persisted, failing.ID).Error)
	require.Equal(t, failing, persisted)
	for _, wrong := range []string{"model", "platform", "channel"} {
		task := newTask(wrong)
		switch wrong {
		case "model":
			task.Properties.UpstreamModelName = "wan2.6-t2v"
		case "platform":
			task.Platform = "17"
		case "channel":
			task.ChannelId = 17
		}
		require.NoError(t, s.DB().Save(&task).Error)
		_, err := m.AdoptLegacyTask(t.Context(), task.ID)
		require.Error(t, err)
	}
	// Pausing after query preparation prevents both network and result parsing.
	plan, err := m.PrepareTaskQueries(t.Context(), []int64{legacy.ID}, map[int]TaskQueryEnvironment{58: {BaseURL: "https://provider.example"}})
	require.NoError(t, err)
	require.Len(t, plan.Queries, 1)
	bind.Enabled = false
	require.NoError(t, m.BindProduction(t.Context(), 58, bind))
	q := plan.Queries[0]
	_, err = q.Execute(t.Context(), host.Executor{ValidateURL: func(string) error { t.Fatal("paused query reached HTTP"); return nil }}, "https://provider.example", nil)
	require.ErrorIs(t, err, model.ErrTaskPluginDisabled)
	response := host.Response{Status: 200, Body: map[string]any{"output": map[string]any{"task_status": "SUCCEEDED", "video_url": "https://result.example/video.mp4"}}}
	_, err = q.ParseProduction(t.Context(), response)
	require.ErrorIs(t, err, model.ErrTaskPluginDisabled)
	_, err = m.AdoptLegacyTask(t.Context(), failing.ID)
	require.ErrorIs(t, err, model.ErrTaskPluginDisabled)
	bind.Enabled = true
	require.NoError(t, m.BindProduction(t.Context(), 58, bind))
	candidates, err := q.ParseProduction(t.Context(), response)
	require.NoError(t, err)
	require.Len(t, candidates, 1)
	require.Equal(t, "SUCCESS", candidates[0].Result["status"])
}
