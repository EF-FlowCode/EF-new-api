package deployment

import (
	"bufio"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestDrainPreservesStreamAndWaitsForSettlement(t *testing.T) {
	var pending atomic.Int64
	var flushed atomic.Bool
	l := &Lifecycle{Instance: "blue", Pending: pending.Load, Finalize: func() error { flushed.Store(true); return nil }}
	finish := make(chan struct{})
	stream := httptest.NewServer(l.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: first\n\n"))
		w.(http.Flusher).Flush()
		<-finish
		pending.Add(1)
		_, _ = w.Write([]byte("data: completed\n\n"))
	})))
	defer stream.Close()
	response, err := http.Get(stream.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	scanner := bufio.NewScanner(response.Body)
	if !scanner.Scan() || scanner.Text() != "data: first" {
		t.Fatal("stream did not start")
	}
	l.Drain()
	if s := l.Snapshot(); s.Active != 1 || !s.Draining {
		t.Fatalf("unexpected status: %+v", s)
	}
	denied, err := http.Get(stream.URL)
	if err != nil {
		t.Fatal(err)
	}
	denied.Body.Close()
	if denied.StatusCode != 503 {
		t.Fatal("accepted new work after drain")
	}
	quiesce := func() int {
		w := httptest.NewRecorder()
		l.ControlHandler().ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/quiesce", nil))
		return w.Code
	}
	if quiesce() != 409 {
		t.Fatal("quiesced active stream")
	}
	close(finish)
	completed := false
	for scanner.Scan() {
		if scanner.Text() == "data: completed" {
			completed = true
		}
	}
	if !completed || scanner.Err() != nil {
		t.Fatal("lost stream completion")
	}
	if quiesce() != 409 {
		t.Fatal("quiesced before async settlement")
	}
	pending.Add(-1)
	if quiesce() != 200 || !flushed.Load() {
		t.Fatal("did not flush drained instance")
	}
	l.Resume()
	if l.Snapshot().Draining {
		t.Fatal("rollback did not resume")
	}
}

func TestReadinessAndControlMethods(t *testing.T) {
	l := &Lifecycle{Probe: func(context.Context) error { return errors.New("secret database details") }}
	for _, tc := range []struct {
		method, path string
		status       int
	}{
		{"GET", "/ready", 503}, {"GET", "/status", 200}, {"GET", "/drain", 404}, {"GET", "/resume", 404}, {"POST", "/unknown", 404},
	} {
		w := httptest.NewRecorder()
		l.ControlHandler().ServeHTTP(w, httptest.NewRequest(tc.method, tc.path, nil))
		if w.Code != tc.status {
			t.Fatalf("%s %s: %d", tc.method, tc.path, w.Code)
		}
	}
}
