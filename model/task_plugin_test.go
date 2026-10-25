package model

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"eflow.local/new-api/internal/testdb"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func taskPluginTestStore(t *testing.T) SQLTaskPluginStore {
	t.Helper()
	db := testdb.OpenMySQL8(t, &TaskPlugin{}, &TaskPluginControl{}, &TaskPluginShadowBinding{}, &TaskPluginSourceSnapshot{}, &TaskPluginTaskReference{}, &TaskPluginProductionBinding{})
	require.NoError(t, ensureTaskPluginControl(db))
	return SQLTaskPluginStore{DB: func() *gorm.DB { return db }}
}

func TestTaskPluginStoreLargeSourceRollbackAndVersionCase(t *testing.T) {
	s := taskPluginTestStore(t)
	ctx := context.Background()
	initial, err := s.Read(ctx)
	require.NoError(t, err)
	require.False(t, initial.Enabled)
	require.True(t, initial.FactoryEnabled)
	unchanged, err := s.Update(ctx, func(*TaskPluginSnapshot) error { return nil })
	require.NoError(t, err)
	require.Equal(t, initial, unchanged, "a no-op must not advance the revision")
	source := strings.Repeat("大文本源码", 20000)
	committed, err := s.Update(ctx, func(state *TaskPluginSnapshot) error {
		state.Plugins = append(state.Plugins, TaskPlugin{Key: "test", Version: "1.0.0-A", APIVersion: 1, Source: source, SourceHash: strings.Repeat("a", 64), Active: true, Enabled: true})
		return nil
	})
	require.NoError(t, err)
	require.Greater(t, committed.Plugins[0].Id, int64(0))
	read, err := s.Read(ctx)
	require.NoError(t, err)
	require.Equal(t, source, read.Plugins[0].Source)
	_, err = s.Update(ctx, func(state *TaskPluginSnapshot) error {
		state.Plugins[0].Active = false
		return errors.New("validation failed")
	})
	require.Error(t, err)
	read, err = s.Read(ctx)
	require.NoError(t, err)
	require.Equal(t, committed, read)
	_, err = s.Update(ctx, func(state *TaskPluginSnapshot) error {
		state.Plugins[0].Source = "overwrite"
		return nil
	})
	require.ErrorContains(t, err, "immutable")
	committed, err = s.Update(ctx, func(state *TaskPluginSnapshot) error {
		state.Plugins = append(state.Plugins, TaskPlugin{Key: "test", Version: "1.0.0-a", APIVersion: 1, Source: "case", SourceHash: strings.Repeat("b", 64)})
		return nil
	})
	require.NoError(t, err)
	require.Len(t, committed.Plugins, 2)
	_, err = s.Update(ctx, func(state *TaskPluginSnapshot) error {
		state.Enabled = true
		duplicate := state.Plugins[1]
		duplicate.Id = 0
		state.Plugins = append(state.Plugins, duplicate)
		return nil
	})
	require.Error(t, err)
	read, err = s.Read(ctx)
	require.NoError(t, err)
	require.Equal(t, committed, read, "SQL failure rolls back state and revision")
}

func TestTaskPluginStoreConcurrentActivation(t *testing.T) {
	s := taskPluginTestStore(t)
	ctx := context.Background()
	const workers = 8
	var wg sync.WaitGroup
	errorsCh := make(chan error, workers)
	for i := range workers {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := s.Update(ctx, func(state *TaskPluginSnapshot) error {
				for j := range state.Plugins {
					state.Plugins[j].Active = false
				}
				state.Plugins = append(state.Plugins, TaskPlugin{Key: "concurrent", Version: fmt.Sprintf("1.0.%d", i), APIVersion: 1, Source: "test", SourceHash: strings.Repeat("a", 64), Active: true})
				return nil
			})
			errorsCh <- err
		}(i)
	}
	wg.Wait()
	close(errorsCh)
	for err := range errorsCh {
		require.NoError(t, err)
	}
	state, err := s.Read(ctx)
	require.NoError(t, err)
	require.Len(t, state.Plugins, workers)
	require.Equal(t, uint64(workers+1), state.Revision)
	active := 0
	for _, row := range state.Plugins {
		if row.Active {
			active++
		}
	}
	require.Equal(t, 1, active)
	_, err = s.Update(ctx, func(state *TaskPluginSnapshot) error {
		for i := range state.Plugins {
			state.Plugins[i].Active = true
		}
		return nil
	})
	require.ErrorContains(t, err, "multiple active")
}

func TestTaskPluginShadowBindingReferenceTransaction(t *testing.T) {
	s := taskPluginTestStore(t)
	db := s.DB()
	require.NoError(t, db.AutoMigrate(&Channel{}))
	require.NoError(t, db.Create(&Channel{Id: 17, Type: 17, Name: "shadow-ali"}).Error)
	require.NoError(t, db.Create(&Channel{Id: 18, Type: 1001, Name: "happyhorse"}).Error)
	_, err := s.Update(t.Context(), func(state *TaskPluginSnapshot) error {
		state.Plugins = append(state.Plugins, TaskPlugin{Key: "alibaba", APIVersion: 1, Version: "1.0.0", Source: "test", SourceHash: strings.Repeat("a", 64), Enabled: true, Active: true})
		return nil
	})
	require.NoError(t, err)
	binding := TaskPluginShadowBinding{ChannelID: 17, Key: "alibaba", Version: "1.0.0", SourceHash: strings.Repeat("a", 64), Enabled: true, SampleEvery: 1}
	before, err := s.Read(t.Context())
	require.NoError(t, err)
	// These contenders use the same cross-node lock. Whichever wins, no
	// committed snapshot may contain a reference to a deleted version.
	var wg sync.WaitGroup
	results := make(chan error, 2)
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, err := s.Update(t.Context(), func(state *TaskPluginSnapshot) error { state.Bindings = append(state.Bindings, binding); return nil })
		results <- err
	}()
	go func() {
		defer wg.Done()
		_, err := s.Update(t.Context(), func(state *TaskPluginSnapshot) error { state.Plugins = nil; return nil })
		results <- err
	}()
	wg.Wait()
	close(results)
	successes := 0
	for err := range results {
		if err == nil {
			successes++
		}
	}
	require.Equal(t, 1, successes)
	after, err := s.Read(t.Context())
	require.NoError(t, err)
	require.Equal(t, before.Revision+1, after.Revision)
	if len(after.Bindings) > 0 {
		require.Len(t, after.Plugins, 1)
	} else {
		require.Empty(t, after.Plugins)
	}
	_, err = s.Update(t.Context(), func(state *TaskPluginSnapshot) error {
		state.Bindings = []TaskPluginShadowBinding{{ChannelID: 18, Key: "alibaba", Version: "1.0.0", Factory: true, FactorySource: "fixture", SourceHash: strings.Repeat("b", 64), Enabled: true, SampleEvery: 1}}
		return nil
	})
	require.ErrorContains(t, err, "Alibaba channel")
	unchanged, err := s.Read(t.Context())
	require.NoError(t, err)
	require.Equal(t, after, unchanged)
}
