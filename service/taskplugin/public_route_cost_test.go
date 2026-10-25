package taskplugin

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"eflow.local/new-api/common"
	"eflow.local/new-api/constant"
	"eflow.local/new-api/model"
	host "eflow.local/new-api/relay/channel/task/jsplugin"
	"github.com/stretchr/testify/require"
)

type countingStore struct {
	*memoryStore
	reads atomic.Int32
}

func (s *countingStore) Read(ctx context.Context) (model.TaskPluginSnapshot, error) {
	s.reads.Add(1)
	return s.memoryStore.Read(ctx)
}

func countCompiles(t *testing.T) *atomic.Int32 {
	t.Helper()
	var n atomic.Int32
	old := compileHandle
	compileHandle = func(source string) (*host.Handle, error) {
		n.Add(1)
		return old(source)
	}
	t.Cleanup(func() { compileHandle = old })
	return &n
}

func stubPublicChannels(t *testing.T, channels []model.Channel, err error) {
	t.Helper()
	old := listPublicChannels
	listPublicChannels = func(context.Context, string) ([]model.Channel, error) { return channels, err }
	t.Cleanup(func() { listPublicChannels = old })
}

func openAIChannel(id int, models string) model.Channel {
	return model.Channel{Id: id, Type: constant.ChannelTypeOpenAI, Status: common.ChannelStatusEnabled, Models: models}
}

func TestPublicOrdinaryModelSkipsPluginWork(t *testing.T) {
	store := &countingStore{memoryStore: &memoryStore{state: model.TaskPluginSnapshot{Revision: 1, Enabled: true, FactoryEnabled: true}}}
	m := NewManager(store, factorySources())
	require.NoError(t, m.Refresh(t.Context()))
	compiles := countCompiles(t)
	reads := store.reads.Load()
	stubPublicChannels(t, []model.Channel{openAIChannel(1, "gpt-5.5"), openAIChannel(2, "gpt-5.5,sora-2"), openAIChannel(3, "GPT-5.5")}, nil)
	for range 3 {
		route, err := m.ResolvePublicRoute(t.Context(), "gpt-5.5", "/v1/responses")
		require.NoError(t, err)
		require.Nil(t, route)
	}
	_, err := m.ResolvePublicRoute(t.Context(), "sora-2", "/v1/responses")
	require.ErrorIs(t, err, model.ErrTaskPluginDisabled, "factory-declared task models still cannot use the token relay")
	require.Zero(t, compiles.Load(), "public requests never compile plugin source")
	require.Equal(t, reads, store.reads.Load(), "no snapshot read without a relevant binding")
}

func TestPublicRouteDegradesOnStorageErrors(t *testing.T) {
	t.Run("channel_listing", func(t *testing.T) {
		m := NewManager(&memoryStore{state: model.TaskPluginSnapshot{Revision: 1, Enabled: true, FactoryEnabled: true}}, factorySources())
		require.NoError(t, m.Refresh(t.Context()))
		stubPublicChannels(t, nil, errors.New("db down"))
		route, err := m.ResolvePublicRoute(t.Context(), "gpt-5.5", "/v1/responses")
		require.NoError(t, err)
		require.Nil(t, route)
		_, err = m.ResolvePublicRoute(t.Context(), "SORA-2", "/v1/responses")
		require.Error(t, err)
	})
	t.Run("snapshot_before_first_refresh", func(t *testing.T) {
		m := NewManager(&memoryStore{readError: errors.New("db down")}, factorySources())
		stubPublicChannels(t, []model.Channel{openAIChannel(1, "gpt-5.5,sora-2")}, nil)
		route, err := m.ResolvePublicRoute(t.Context(), "gpt-5.5", "/v1/responses")
		require.NoError(t, err)
		require.Nil(t, route)
		_, err = m.ResolvePublicRoute(t.Context(), "sora-2", "/v1/responses")
		require.ErrorIs(t, err, ErrUnavailable)
	})
}

func TestPublicDisabledBindingFailsClosed(t *testing.T) {
	store := &countingStore{memoryStore: &memoryStore{state: model.TaskPluginSnapshot{Revision: 1, Enabled: true, FactoryEnabled: true}}}
	m := NewManager(store, factorySources())
	h, err := host.Compile(m.factories["alibaba"])
	require.NoError(t, err)
	bind := func(enabled bool) {
		require.NoError(t, m.BindProduction(t.Context(), 1, ProductionBindingRequest{Key: "alibaba", Version: h.Identity().Version, Factory: true, Enabled: enabled}))
	}
	stubPublicChannels(t, []model.Channel{publicChannel(1, "alibaba", "Movie,qwen-max", `{"Movie":"wan2.6-t2v"}`)}, nil)
	bind(true)
	route, err := m.ResolvePublicRoute(t.Context(), "movie", "/v1/responses")
	require.NoError(t, err)
	require.NotNil(t, route)

	bind(false)
	store.mu.Lock()
	store.readError = errors.New("db down")
	store.mu.Unlock()
	compiles := countCompiles(t)
	reads := store.reads.Load()
	_, err = m.ResolvePublicRoute(t.Context(), "Movie", "/v1/responses")
	require.ErrorIs(t, err, model.ErrTaskPluginDisabled, "disabling a binding must fail closed")
	route, err = m.ResolvePublicRoute(t.Context(), "qwen-max", "/v1/responses")
	require.NoError(t, err)
	require.Nil(t, route)
	require.Zero(t, compiles.Load())
	require.Equal(t, reads, store.reads.Load())
}
