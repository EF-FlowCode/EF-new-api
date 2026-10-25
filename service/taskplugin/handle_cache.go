package taskplugin

import (
	"container/list"
	"sync"

	host "eflow.local/new-api/relay/channel/task/jsplugin"
)

// Compiled task sources are immutable and keyed by their content hash. A
// Handle never mutates after Compile and its engine pools runtimes behind a
// semaphore, so concurrent requests may share one (production bindings
// already share theirs). Public task reads otherwise recompile the full
// source per task: a 100-id native query or a polling client would pay that
// cost on every request.
const publicHandleCacheSize = 32

type handleCache struct {
	mu    sync.Mutex
	limit int
	order *list.List // front = most recently used
	items map[host.Identity]*list.Element
}

func newHandleCache(limit int) *handleCache {
	return &handleCache{limit: limit, order: list.New(), items: map[host.Identity]*list.Element{}}
}

var publicHandles = newHandleCache(publicHandleCacheSize)

func (c *handleCache) get(id host.Identity) *host.Handle {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.items[id]
	if !ok {
		return nil
	}
	c.order.MoveToFront(e)
	return e.Value.(*host.Handle)
}

func (c *handleCache) put(h *host.Handle) {
	id := h.Identity()
	c.mu.Lock()
	defer c.mu.Unlock()
	if e, ok := c.items[id]; ok {
		c.order.MoveToFront(e)
		return
	}
	c.items[id] = c.order.PushFront(h)
	for c.order.Len() > c.limit {
		oldest := c.order.Back()
		c.order.Remove(oldest)
		delete(c.items, oldest.Value.(*host.Handle).Identity())
	}
}

// publicHandle resolves the fixed renderer for a stored task identity without
// recompiling it on every read. Only a handle whose identity equals the stored
// identity is ever reused; the source itself was already hash-checked.
func (m *Manager) publicHandle(want host.Identity, channelID int, source string, pinned ...*host.Handle) (*host.Handle, error) {
	if len(pinned) > 0 && pinned[0] != nil && pinned[0].Identity() == want {
		return pinned[0], nil
	}
	if h, _, ok := m.ProductionHandle(channelID); ok && h != nil && h.Identity() == want {
		return h, nil
	}
	if h := publicHandles.get(want); h != nil {
		return h, nil
	}
	h, err := host.Compile(source)
	if err != nil {
		return nil, err
	}
	if h.Identity() != want {
		return nil, ErrNotFound
	}
	publicHandles.put(h)
	return h, nil
}
