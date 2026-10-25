package common

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

type StreamEndReason string

const (
	StreamEndReasonNone        StreamEndReason = ""
	StreamEndReasonDone        StreamEndReason = "done"
	StreamEndReasonTimeout     StreamEndReason = "timeout"
	StreamEndReasonClientGone  StreamEndReason = "client_gone"
	StreamEndReasonScannerErr  StreamEndReason = "scanner_error"
	StreamEndReasonHandlerStop StreamEndReason = "handler_stop"
	StreamEndReasonEOF         StreamEndReason = "eof"
	StreamEndReasonPanic       StreamEndReason = "panic"
	StreamEndReasonPingFail    StreamEndReason = "ping_fail"
)

const maxStreamErrorEntries = 20

type StreamErrorEntry struct {
	Message   string
	Timestamp time.Time
}

type StreamStatus struct {
	EndReason StreamEndReason
	EndError  error
	endOnce   sync.Once

	mu             sync.Mutex
	Errors         []StreamErrorEntry
	ErrorCount     int
	responseFailed bool
	// cancelOnlyFailure reports that responseFailed was set only by a client
	// cancellation, which ReconcileDeliveredCompletion may still undo.
	cancelOnlyFailure bool
}

func NewStreamStatus() *StreamStatus {
	return &StreamStatus{}
}

func (s *StreamStatus) SetEndReason(reason StreamEndReason, err error) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	// Keep late failures even if a scanner's EOF/DONE won the first-end race.
	if err != nil || (reason != StreamEndReasonDone && reason != StreamEndReasonEOF && reason != StreamEndReasonHandlerStop) {
		cancelled := reason == StreamEndReasonClientGone && errors.Is(err, context.Canceled)
		s.cancelOnlyFailure = cancelled && (!s.responseFailed || s.cancelOnlyFailure)
		s.responseFailed = true
	}
	s.endOnce.Do(func() {
		s.EndReason = reason
		s.EndError = err
	})
}

// ResponseFailed is read after stream workers have joined. Soft parsing errors,
// abnormal termination and late errors all prevent success-limit accounting.
func (s *StreamStatus) ResponseFailed() bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.responseFailed || s.ErrorCount > 0 || !s.IsNormalEnd()
}

// ReconcileDeliveredCompletion turns a client cancellation into a normal end
// when the handler had already delivered the protocol's completion. It is only
// called after every stream goroutine has joined, and leaves any other failure
// untouched.
func (s *StreamStatus) ReconcileDeliveredCompletion() {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.EndReason != StreamEndReasonClientGone || !errors.Is(s.EndError, context.Canceled) ||
		s.ErrorCount > 0 || (s.responseFailed && !s.cancelOnlyFailure) {
		return
	}
	s.EndReason = StreamEndReasonDone
	s.EndError = nil
	s.responseFailed = false
	s.cancelOnlyFailure = false
}

func (s *StreamStatus) RecordError(msg string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ErrorCount++
	if len(s.Errors) < maxStreamErrorEntries {
		s.Errors = append(s.Errors, StreamErrorEntry{
			Message:   msg,
			Timestamp: time.Now(),
		})
	}
}

func (s *StreamStatus) HasErrors() bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ErrorCount > 0
}

func (s *StreamStatus) TotalErrorCount() int {
	if s == nil {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ErrorCount
}

func (s *StreamStatus) IsNormalEnd() bool {
	if s == nil {
		return true
	}
	return s.EndReason == StreamEndReasonDone ||
		s.EndReason == StreamEndReasonEOF ||
		s.EndReason == StreamEndReasonHandlerStop
}

// IsServerSideError returns true when the stream ended abnormally due to
// server-side causes (timeout, scanner error, ping failure, panic).
// Client-initiated disconnects return false — the user chose to stop.
func (s *StreamStatus) IsServerSideError() bool {
	if s == nil {
		return false
	}
	return !s.IsNormalEnd() && s.EndReason != StreamEndReasonClientGone
}

func (s *StreamStatus) Summary() string {
	if s == nil {
		return "StreamStatus<nil>"
	}
	b := &strings.Builder{}
	fmt.Fprintf(b, "reason=%s", s.EndReason)
	if s.EndError != nil {
		fmt.Fprintf(b, " end_error=%q", s.EndError.Error())
	}
	s.mu.Lock()
	if s.ErrorCount > 0 {
		fmt.Fprintf(b, " soft_errors=%d", s.ErrorCount)
	}
	s.mu.Unlock()
	return b.String()
}
