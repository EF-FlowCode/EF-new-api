package taskplugin

import (
	"sync"
	"testing"

	"eflow.local/new-api/plugins"
	host "eflow.local/new-api/relay/channel/task/jsplugin"
	"github.com/stretchr/testify/require"
)

func TestPublicHandleCacheReusesCompiledSource(t *testing.T) {
	source, err := plugins.Source("doubao")
	require.NoError(t, err)
	compiled, err := host.Compile(source)
	require.NoError(t, err)
	want := compiled.Identity()
	m := NewManager(nil, nil)

	first, err := m.publicHandle(want, 1, source)
	require.NoError(t, err)
	var wg sync.WaitGroup
	got := make(chan *host.Handle, 8)
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			h, err := m.publicHandle(want, 1, source)
			if err != nil {
				got <- nil
				return
			}
			got <- h
			_, err = h.Protocol(t.Context(), "openai_video", "render", map[string]any{}, map[string]any{"status": "SUCCESS", "data": map[string]any{}})
			_ = err // shared handles must tolerate concurrent hook calls
		}()
	}
	wg.Wait()
	close(got)
	for h := range got {
		require.Same(t, first, h, "same source hash must not be recompiled")
	}

	pinned, err := m.publicHandle(want, 1, source, compiled)
	require.NoError(t, err)
	require.Same(t, compiled, pinned)

	forged := want
	forged.SourceHash = "0000"
	_, err = m.publicHandle(forged, 1, source)
	require.ErrorIs(t, err, ErrNotFound, "a handle is only reused or cached under its own identity")
	require.Nil(t, publicHandles.get(forged))
}

func TestHandleCacheEvictsLeastRecentlyUsed(t *testing.T) {
	sources := map[string]*host.Handle{}
	for _, key := range []string{"doubao", "kling", "vidu"} {
		source, err := plugins.Source(key)
		require.NoError(t, err)
		h, err := host.Compile(source)
		require.NoError(t, err)
		sources[key] = h
	}
	c := newHandleCache(2)
	c.put(sources["doubao"])
	c.put(sources["kling"])
	require.NotNil(t, c.get(sources["doubao"].Identity()))
	c.put(sources["vidu"])
	require.Nil(t, c.get(sources["kling"].Identity()))
	require.Same(t, sources["doubao"], c.get(sources["doubao"].Identity()))
	require.Same(t, sources["vidu"], c.get(sources["vidu"].Identity()))
	require.Equal(t, 2, c.order.Len())
}
