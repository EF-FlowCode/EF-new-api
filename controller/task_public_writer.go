package controller

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

const publicWriteTimeout = 5 * time.Second

// All writes remain on the handler goroutine. The cancellation callback only
// changes the transport deadline, so a disconnected/slow peer cannot hold a slot.
type publicDeadlineWriter struct {
	gin.ResponseWriter
	transport *http.ResponseController
	ctx       context.Context
	timeout   time.Duration
	mu        sync.Mutex // serializes deadline changes with cancellation
	err       error      // only accessed by the writer goroutine
	stop      func() bool
	canceled  chan struct{}
}

func newPublicDeadlineWriter(ctx context.Context, w gin.ResponseWriter, timeout time.Duration) (*publicDeadlineWriter, error) {
	var raw http.ResponseWriter = w
	// Gin's Flush discards transport errors. Use its underlying writer for
	// FlushError, retaining Gin for Write/status/size accounting.
	if wrapped, ok := w.(interface{ Unwrap() http.ResponseWriter }); ok {
		raw = wrapped.Unwrap()
	}
	p := &publicDeadlineWriter{ResponseWriter: w, transport: http.NewResponseController(raw), ctx: ctx, timeout: timeout, canceled: make(chan struct{})}
	// Verify support before any task submission. Do not start the write budget
	// while waiting for a provider or a worker.
	if err := p.transport.SetWriteDeadline(time.Time{}); err != nil {
		return nil, err
	}
	p.stop = context.AfterFunc(ctx, func() {
		defer close(p.canceled)
		p.mu.Lock()
		defer p.mu.Unlock()
		_ = p.transport.SetWriteDeadline(time.Now())
	})
	return p, nil
}

func (w *publicDeadlineWriter) beforeWrite() error {
	if w.err != nil {
		return w.err
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if err := w.ctx.Err(); err != nil {
		w.err = err
		return err
	}
	w.err = w.transport.SetWriteDeadline(time.Now().Add(w.timeout))
	return w.err
}

func (w *publicDeadlineWriter) Write(b []byte) (int, error) {
	if err := w.beforeWrite(); err != nil {
		return 0, err
	}
	n, err := w.ResponseWriter.Write(b)
	w.err = err
	return n, err
}

func (w *publicDeadlineWriter) WriteString(s string) (int, error) { return w.Write([]byte(s)) }
func (w *publicDeadlineWriter) Flush()                            { _ = w.FlushError() }
func (w *publicDeadlineWriter) FlushError() error {
	if err := w.beforeWrite(); err != nil {
		return err
	}
	w.ResponseWriter.WriteHeaderNow()
	w.err = w.transport.Flush()
	if w.err == nil {
		w.mu.Lock()
		if w.ctx.Err() == nil {
			w.err = w.transport.SetWriteDeadline(time.Time{})
		}
		w.mu.Unlock()
	}
	return w.err
}

func (w *publicDeadlineWriter) close() {
	// JSON and error responses also have to reach the transport within budget.
	if w.Written() {
		_ = w.FlushError()
	}
	if !w.stop() {
		<-w.canceled
	}
	// net/http still writes the response terminator after the handler returns.
	// Bound that flush too. HTTP/1 resets the deadline before connection reuse;
	// HTTP/2 owns a per-stream deadline, stopped when the stream closes.
	// Never extend a failed/canceled transport's deadline.
	if w.err == nil && w.ctx.Err() == nil {
		_ = w.transport.SetWriteDeadline(time.Now().Add(w.timeout))
	}
}

func flushPublicWriter(w gin.ResponseWriter) error {
	if f, ok := w.(interface{ FlushError() error }); ok {
		return f.FlushError()
	}
	w.Flush()
	return nil
}
