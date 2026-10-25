// Package drainpool accounts for work that can outlive an HTTP handler.
package drainpool

import (
	"sync/atomic"

	"github.com/bytedance/gopkg/util/gopool"
)

var pending atomic.Int64

// Add must run before work is queued, so an idle worker cannot hide queued work.
func Add()           { pending.Add(1) }
func Done()          { pending.Add(-1) }
func Pending() int64 { return pending.Load() }

func Go(f func()) {
	Add()
	gopool.Go(func() {
		defer Done()
		f()
	})
}
