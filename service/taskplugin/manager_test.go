package taskplugin

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"eflow.local/new-api/model"
	"github.com/stretchr/testify/require"
)

// This fake preserves the transactional contract, including errors occurring
// after preflight but before commit. SQL locking is tested against MySQL.
type memoryStore struct {
	mu                     sync.Mutex
	state                  model.TaskPluginSnapshot
	readError, commitError error
	nextID                 int64
}

func (s *memoryStore) Read(context.Context) (model.TaskPluginSnapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state.Clone(), s.readError
}
func (s *memoryStore) Update(_ context.Context, change func(*model.TaskPluginSnapshot) error) (model.TaskPluginSnapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	candidate := s.state.Clone()
	if err := change(&candidate); err != nil {
		return model.TaskPluginSnapshot{}, err
	}
	if s.commitError != nil {
		return model.TaskPluginSnapshot{}, s.commitError
	}
	for i := range candidate.Plugins {
		if candidate.Plugins[i].Id == 0 {
			s.nextID++
			candidate.Plugins[i].Id = s.nextID
		}
	}
	candidate.Revision++
	s.state = candidate.Clone()
	return candidate, nil
}

func testSource(key, version string, channel int) string {
	return fmt.Sprintf(`export const meta = {apiVersion:1,key:%q,name:"Test",version:%q,author:{name:"Test"},models:["test-model"],channelTypes:[%d],fetchMode:"per_task"};
export function buildSubmitRequest() {return {version:meta.version};}
export function parseSubmitResponse() {return {};}
export function buildQueryRequest() {return {};}
export function parseTaskResult() {return {};}`, key, version, channel)
}
func flag(value bool) *bool { return &value }
func newTestManager() (*Manager, *memoryStore) {
	store := &memoryStore{state: model.TaskPluginSnapshot{Revision: 1, FactoryEnabled: true}}
	return NewManager(store, map[string]string{"test": testSource("test", "1.0.0", 20)}), store
}
func currentVersion(t *testing.T, m *Manager, key string) string {
	t.Helper()
	p, ok := m.view.Load().registry.Get(key)
	require.True(t, ok)
	return p.Meta.Version
}

func TestVersionsActivationDisableAndDeleteFallback(t *testing.T) {
	ctx := context.Background()
	m, _ := newTestManager()
	require.NoError(t, m.Refresh(ctx))
	require.False(t, m.Status().Enabled)
	require.NoError(t, m.Configure(ctx, Settings{Enabled: flag(true)}))
	require.Equal(t, "1.0.0", currentVersion(t, m, "test"))
	_, err := m.Upload(ctx, UploadRequest{Source: testSource("test", "2.0.0", 20)})
	require.NoError(t, err)
	_, err = m.Upload(ctx, UploadRequest{Source: testSource("test", "3.0.0", 20)})
	require.NoError(t, err)
	require.Equal(t, "2.0.0", currentVersion(t, m, "test"), "subsequent uploads are staged")
	require.NoError(t, m.Activate(ctx, "test", "3.0.0"))
	require.Equal(t, "3.0.0", currentVersion(t, m, "test"))
	require.NoError(t, m.Activate(ctx, "test", "2.0.0"))
	require.Equal(t, "2.0.0", currentVersion(t, m, "test"), "rollback uses the same activation path")
	require.NoError(t, m.SetStatus(ctx, "test", false))
	_, ok := m.view.Load().registry.Get("test")
	require.False(t, ok, "disabled override must mask its factory")
	require.NoError(t, m.Activate(ctx, "test", "3.0.0"))
	require.NoError(t, m.Delete(ctx, "test", "3.0.0"))
	_, ok = m.view.Load().registry.Get("test")
	require.False(t, ok, "promotion preserves a version's disabled state")
	require.NoError(t, m.Delete(ctx, "test", "2.0.0"))
	require.Equal(t, "1.0.0", currentVersion(t, m, "test"))
}

func TestCommitFailureNeverPublishesCandidate(t *testing.T) {
	ctx := context.Background()
	m, store := newTestManager()
	require.NoError(t, m.Configure(ctx, Settings{Enabled: flag(true)}))
	before := m.view.Load()
	store.commitError = errors.New("injected commit failure")
	_, err := m.Upload(ctx, UploadRequest{Source: testSource("test", "2.0.0", 20)})
	require.ErrorContains(t, err, "commit failure")
	require.Same(t, before, m.view.Load())
	require.Empty(t, store.state.Plugins)
}

func TestImmutableVersionAndUploadValidation(t *testing.T) {
	ctx := context.Background()
	m, store := newTestManager()
	source := testSource("test", "2.0.0", 20)
	_, err := m.Upload(ctx, UploadRequest{Source: source})
	require.NoError(t, err)
	_, err = m.Upload(ctx, UploadRequest{Source: source + "\n// modified"})
	require.ErrorIs(t, err, ErrConflict)
	require.Equal(t, source, store.state.Plugins[0].Source)
	for _, req := range []UploadRequest{
		{Source: source, SourceSha256: strings.Repeat("0", 64)},
		{Source: source, Icon: "data:image/svg+xml;base64,bad"},
		{Source: source, Force: true},
		{Source: strings.Repeat("x", MaxSourceBytes+1)},
		{Source: "invalid JavaScript"},
	} {
		_, err = m.Upload(ctx, req)
		require.Error(t, err)
	}
}

func TestRoutingConflictRollsBackAndCanBeStagedDisabled(t *testing.T) {
	ctx := context.Background()
	m, store := newTestManager()
	require.NoError(t, m.Configure(ctx, Settings{Enabled: flag(true)}))
	before := m.view.Load()
	source := testSource("collision", "1.0.0", 20)
	_, err := m.Upload(ctx, UploadRequest{Source: source})
	require.ErrorIs(t, err, ErrConflict)
	require.Same(t, before, m.view.Load())
	require.Empty(t, store.state.Plugins)
	_, err = m.Upload(ctx, UploadRequest{Source: source, Enabled: flag(false)})
	require.NoError(t, err)
	require.ErrorIs(t, m.Activate(ctx, "collision", "1.0.0"), ErrConflict)
	require.NoError(t, m.SetStatus(ctx, "test", false))
	require.NoError(t, m.Activate(ctx, "collision", "1.0.0"))
	require.Equal(t, "1.0.0", currentVersion(t, m, "collision"))
}

func TestRefreshFailureRetainsLastGoodAndMasterOffRecovers(t *testing.T) {
	ctx := context.Background()
	m, store := newTestManager()
	_, err := m.Upload(ctx, UploadRequest{Source: testSource("test", "2.0.0", 20)})
	require.NoError(t, err)
	require.NoError(t, m.Configure(ctx, Settings{Enabled: flag(true)}))
	before := m.view.Load()
	store.readError = errors.New("database offline")
	require.Error(t, m.Refresh(ctx))
	require.Same(t, before, m.view.Load())
	require.Contains(t, m.Status().LastError, "offline")
	store.readError = nil
	store.state.Revision++
	store.state.Plugins[0].Source = "damaged"
	require.Error(t, m.Refresh(ctx))
	require.Same(t, before, m.view.Load())
	require.Greater(t, m.Status().ObservedRevision, m.Status().AppliedRevision)
	require.NoError(t, m.Configure(ctx, Settings{Enabled: flag(false)}))
	require.False(t, m.Status().Enabled)
	require.Empty(t, m.Status().LastError)
	require.Error(t, m.Configure(ctx, Settings{Enabled: flag(true)}))
	require.False(t, m.Status().Enabled)
}

func TestNodesConvergeAndFactorySwitchPreservesOverrides(t *testing.T) {
	ctx := context.Background()
	m, store := newTestManager()
	other := NewManager(store, m.factories)
	require.NoError(t, other.Refresh(ctx))
	_, err := m.Upload(ctx, UploadRequest{Source: testSource("custom", "1.0.0", 21)})
	require.NoError(t, err)
	require.NoError(t, m.Configure(ctx, Settings{Enabled: flag(true), FactoryEnabled: flag(false)}))
	require.NoError(t, other.Refresh(ctx))
	require.Equal(t, m.Status().AppliedRevision, other.Status().AppliedRevision)
	require.Equal(t, "1.0.0", currentVersion(t, other, "custom"))
	_, ok := other.view.Load().registry.Get("test")
	require.False(t, ok)
	before := other.view.Load()
	require.NoError(t, other.Refresh(ctx))
	require.Same(t, before, other.view.Load())
}

func TestDryRunHasNoNetworkAndHonorsTimeout(t *testing.T) {
	ctx := context.Background()
	m, _ := newTestManager()
	output, err := m.DryRun(ctx, "test", "", "buildSubmitRequest", "", nil)
	require.NoError(t, err)
	require.Equal(t, map[string]any{"version": "1.0.0"}, output)
	source := testSource("test", "2.0.0", 20) + ` export function spin() {while(true){}} export function globals() {return [typeof fetch,typeof require,typeof process];}`
	_, err = m.Upload(ctx, UploadRequest{Source: source})
	require.NoError(t, err)
	output, err = m.DryRun(ctx, "test", "", "globals", "", nil)
	require.NoError(t, err)
	require.Equal(t, []any{"undefined", "undefined", "undefined"}, output)
	short, cancel := context.WithTimeout(ctx, 25*time.Millisecond)
	defer cancel()
	_, err = m.DryRun(short, "test", "", "spin", "", nil)
	require.Error(t, err)
}

func TestBuiltinManagementRegistryCanBeEnabled(t *testing.T) {
	m := NewManager(&memoryStore{state: model.TaskPluginSnapshot{Revision: 1, FactoryEnabled: true}}, factorySources())
	require.Len(t, m.factories, 11)
	require.NoError(t, m.Configure(context.Background(), Settings{Enabled: flag(true)}))
	for key := range m.factories {
		_, ok := m.view.Load().registry.Get(key)
		require.True(t, ok, key)
	}
}
