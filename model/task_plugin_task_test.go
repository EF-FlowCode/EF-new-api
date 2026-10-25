package model

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func storedPluginFixture(version string, factory bool) (Task, TaskPluginSourceSnapshot) {
	source := "// immutable fixture " + version
	hash := fmt.Sprintf("%x", sha256.Sum256([]byte(source)))
	id := TaskPluginIdentity{IdentityVersion: 1, APIVersion: 1, Key: "fixture", Version: version, SourceHash: hash, Factory: factory}
	task := Task{TaskID: "public-" + version, UserId: 1, ChannelId: 17, Status: TaskStatusSubmitted, Quota: 400, PrivateData: TaskPrivateData{Plugin: &id, UpstreamTaskID: "upstream-" + version, Key: "private-credential", TokenId: 3}}
	return task, TaskPluginSourceSnapshot{Key: id.Key, Version: id.Version, SourceHash: hash, APIVersion: 1, Source: source}
}

func TestTaskPluginIdentityJSONLegacy(t *testing.T) {
	task, _ := storedPluginFixture("1.0.0", true)
	value, err := task.PrivateData.Value()
	require.NoError(t, err)
	var private TaskPrivateData
	require.NoError(t, private.Scan(value))
	require.Equal(t, task.PrivateData, private)
	raw, err := json.Marshal(task)
	require.NoError(t, err)
	for _, secret := range []string{"private-credential", "source_hash", "identity_version", "private_data"} {
		require.NotContains(t, string(raw), secret)
	}
	require.NoError(t, private.Scan([]byte(`{"key":"legacy"}`)))
	require.Nil(t, private.Plugin, "reused scanner must not inherit a previous row's plugin identity")
	require.Equal(t, "legacy", private.Key)
	require.NoError(t, private.Scan(nil))
	require.Equal(t, TaskPrivateData{}, private)
	require.ErrorIs(t, task.Insert(), ErrTaskPluginIdentity, "generic insertion cannot bypass reference persistence")
	invalid := *task.PrivateData.Plugin
	invalid.IdentityVersion = 2
	require.ErrorIs(t, invalid.Validate(), ErrTaskPluginIdentity)
}

func TestTaskPluginTaskPersistenceAndRollback(t *testing.T) {
	s := taskPluginTestStore(t)
	db := s.DB()
	require.NoError(t, db.AutoMigrate(&Task{}))
	_, err := s.Update(t.Context(), func(state *TaskPluginSnapshot) error { state.Enabled = true; return nil })
	require.NoError(t, err)
	task, source := storedPluginFixture("1.0.0", true)
	saved, err := s.CreatePluginTask(t.Context(), task, source)
	require.NoError(t, err)
	require.Zero(t, task.ID)
	task.TaskID = "second-task"
	second, err := s.CreatePluginTask(t.Context(), task, source)
	require.NoError(t, err)
	var count int64
	require.NoError(t, db.Model(&TaskPluginSourceSnapshot{}).Count(&count).Error)
	require.EqualValues(t, 1, count, "shared immutable source is deduplicated")
	legacy := Task{TaskID: "legacy", UserId: 1}
	require.NoError(t, db.Create(&legacy).Error)
	read, err := s.ReadPluginTasks(t.Context(), []int64{second.ID, legacy.ID, saved.ID})
	require.NoError(t, err)
	require.Equal(t, second.ID, read.Tasks[0].Task.ID)
	require.Nil(t, read.Tasks[1].Task.PrivateData.Plugin)
	require.Equal(t, saved.PrivateData, read.Tasks[2].Task.PrivateData)
	require.Equal(t, source, read.Tasks[2].Source)
	_, err = s.ReadPluginTasks(t.Context(), []int64{saved.ID, saved.ID})
	require.ErrorIs(t, err, ErrTaskPluginIdentity)
	_, err = s.ReadPluginTasks(t.Context(), []int64{999999})
	require.ErrorIs(t, err, gorm.ErrRecordNotFound)

	// Fail after both source and task INSERTs, proving all three rows roll back.
	require.NoError(t, db.Callback().Create().Before("gorm:create").Register("test:plugin_ref_failure", func(tx *gorm.DB) {
		if tx.Statement.Schema != nil && tx.Statement.Schema.Name == "TaskPluginTaskReference" {
			tx.AddError(errors.New("injected reference failure"))
		}
	}))
	badTask, newSource := storedPluginFixture("2.0.0", true)
	_, err = s.CreatePluginTask(t.Context(), badTask, newSource)
	require.ErrorContains(t, err, "injected")
	require.NoError(t, db.Callback().Create().Remove("test:plugin_ref_failure"))
	require.Zero(t, badTask.ID)
	require.NoError(t, db.Model(&Task{}).Where("task_id = ?", badTask.TaskID).Count(&count).Error)
	require.Zero(t, count)
	require.NoError(t, db.Model(&TaskPluginSourceSnapshot{}).Where("source_hash = ?", newSource.SourceHash).Count(&count).Error)
	require.Zero(t, count)

	// Neither tampered associations nor missing source may silently use latest.
	require.NoError(t, db.Model(&TaskPluginTaskReference{}).Where("task_id = ?", saved.ID).Update("version", "wrong").Error)
	_, err = s.ReadPluginTasks(t.Context(), []int64{saved.ID})
	require.ErrorIs(t, err, ErrTaskPluginIdentity)
	require.NoError(t, db.Model(&TaskPluginTaskReference{}).Where("task_id = ?", saved.ID).Update("version", source.Version).Error)
	require.NoError(t, db.Model(&TaskPluginSourceSnapshot{}).Where("source_hash = ?", source.SourceHash).Update("source", "tampered").Error)
	_, err = s.ReadPluginTasks(t.Context(), []int64{saved.ID})
	require.ErrorIs(t, err, ErrTaskPluginIdentity)
	require.NoError(t, db.Delete(&source).Error)
	_, err = s.ReadPluginTasks(t.Context(), []int64{saved.ID})
	require.ErrorIs(t, err, ErrTaskPluginMissing)
}

func TestTaskPluginTaskReferenceDeleteRace(t *testing.T) {
	s := taskPluginTestStore(t)
	require.NoError(t, s.DB().AutoMigrate(&Task{}))
	for n := range 6 {
		task, source := storedPluginFixture(fmt.Sprintf("1.0.%d", n), false)
		before, err := s.Update(t.Context(), func(state *TaskPluginSnapshot) error {
			state.Enabled = true
			state.Plugins = append(state.Plugins, TaskPlugin{Key: source.Key, Version: source.Version, Source: source.Source, SourceHash: source.SourceHash, APIVersion: 1, Enabled: true})
			return nil
		})
		require.NoError(t, err)
		var createErr, deleteErr error
		start := make(chan struct{})
		var wg sync.WaitGroup
		wg.Add(2)
		go func() { defer wg.Done(); <-start; _, createErr = s.CreatePluginTask(t.Context(), task, source) }()
		go func() {
			defer wg.Done()
			<-start
			_, deleteErr = s.Update(t.Context(), func(state *TaskPluginSnapshot) error {
				for i, p := range state.Plugins {
					if p.Version == source.Version {
						state.Plugins = append(state.Plugins[:i], state.Plugins[i+1:]...)
						break
					}
				}
				return nil
			})
		}()
		close(start)
		wg.Wait()
		after, err := s.Read(t.Context())
		require.NoError(t, err)
		if createErr == nil {
			require.ErrorIs(t, deleteErr, ErrTaskPluginReferenced)
			require.Equal(t, before.Revision, after.Revision, "task insert need not rebuild the management registry")
		} else {
			require.ErrorIs(t, createErr, ErrTaskPluginMissing)
			require.NoError(t, deleteErr)
			require.Equal(t, before.Revision+1, after.Revision)
		}
		var dangling int64
		require.NoError(t, s.DB().Table("task_plugin_task_references r").Joins("LEFT JOIN task_plugins p ON p.key = r.key AND p.version = r.version").Where("r.factory = ? AND p.id IS NULL", false).Count(&dangling).Error)
		require.Zero(t, dangling)
	}
}
