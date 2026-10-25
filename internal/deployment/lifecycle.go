package deployment

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
)

// Bump when old workers/web instances cannot share the new schema or semantics.
// Rolling releases never run migrations; under an unchanged contract they may
// hand the worker off in place (drain, quiesce, exit, then start the new one).
const Contract = "eflow-rolling-v10"

type Lifecycle struct {
	mu       sync.Mutex
	draining bool
	active   int64
	Instance string
	Version  string
	Role     string
	Probe    func(context.Context) error
	Pending  func() int64
	Finalize func() error
}

type Status struct {
	Ready    bool   `json:"ready"`
	Draining bool   `json:"draining"`
	Active   int64  `json:"active"`
	Pending  int64  `json:"pending"`
	Instance string `json:"instance"`
	Version  string `json:"version"`
	Role     string `json:"role"`
	Contract string `json:"contract"`
}

// The outer net/http handler also accounts for upgraded connections until the
// relay handler returns. Do not wrap ResponseWriter: Gin needs Hijacker/Flusher.
func (l *Lifecycle) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		l.mu.Lock()
		if l.draining {
			l.mu.Unlock()
			w.Header().Set("Connection", "close")
			http.Error(w, "instance draining", http.StatusServiceUnavailable)
			return
		}
		l.active++
		l.mu.Unlock()
		defer func() { l.mu.Lock(); l.active--; l.mu.Unlock() }()
		w.Header().Set("X-New-Api-Instance", l.Instance)
		next.ServeHTTP(w, r)
	})
}

func (l *Lifecycle) Drain()  { l.mu.Lock(); l.draining = true; l.mu.Unlock() }
func (l *Lifecycle) Resume() { l.mu.Lock(); l.draining = false; l.mu.Unlock() }

func (l *Lifecycle) Snapshot() Status {
	l.mu.Lock()
	s := Status{Draining: l.draining, Active: l.active, Instance: l.Instance, Version: l.Version, Role: l.Role, Contract: Contract}
	l.mu.Unlock()
	if l.Pending != nil {
		s.Pending = l.Pending()
	}
	return s
}

// ControlHandler must be served on a separate loopback-only listener.
func (l *Lifecycle) ControlHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/drain":
			l.Drain()
		case r.Method == http.MethodPost && r.URL.Path == "/resume":
			l.Resume()
		case r.Method == http.MethodPost && r.URL.Path == "/quiesce":
			s := l.Snapshot()
			if !s.Draining || s.Active != 0 || s.Pending != 0 {
				http.Error(w, "busy", http.StatusConflict)
				return
			}
			if l.Finalize != nil {
				if l.Finalize() != nil {
					http.Error(w, "flush failed", http.StatusServiceUnavailable)
					return
				}
			}
		case r.Method == http.MethodGet && (r.URL.Path == "/ready" || r.URL.Path == "/status"):
		default:
			http.NotFound(w, r)
			return
		}
		s := l.Snapshot()
		s.Ready = !s.Draining
		if r.URL.Path == "/ready" && l.Probe != nil {
			if l.Probe(r.Context()) != nil {
				s.Ready = false
			}
		}
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/ready" && !s.Ready {
			w.WriteHeader(http.StatusServiceUnavailable)
		}
		_ = json.NewEncoder(w).Encode(s)
	})
}
