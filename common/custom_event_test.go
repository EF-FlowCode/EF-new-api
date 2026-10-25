package common

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCustomEventRenderWritesSSEHeadersAndData(t *testing.T) {
	recorder := httptest.NewRecorder()
	event := CustomEvent{Data: "data: hello"}

	if err := event.Render(recorder); err != nil {
		t.Fatalf("render custom event: %v", err)
	}

	if got := recorder.Header().Get("Content-Type"); got != "text/event-stream" {
		t.Fatalf("Content-Type = %q, want text/event-stream", got)
	}
	if got := recorder.Header().Get("Cache-Control"); got != "no-cache" {
		t.Fatalf("Cache-Control = %q, want no-cache", got)
	}
	if got := recorder.Body.String(); got != "data: hello\n\n" {
		t.Fatalf("body = %q, want SSE data frame", got)
	}
}

func TestCustomEventRenderPreservesExistingCacheControl(t *testing.T) {
	recorder := httptest.NewRecorder()
	recorder.Header().Set("Cache-Control", "private")

	if err := (CustomEvent{Data: "event: ping"}).Render(recorder); err != nil {
		t.Fatalf("render custom event: %v", err)
	}

	if got := recorder.Header().Get("Cache-Control"); got != "private" {
		t.Fatalf("Cache-Control = %q, want private", got)
	}
	if got := recorder.Code; got != http.StatusOK {
		t.Fatalf("status = %d, want %d", got, http.StatusOK)
	}
}
