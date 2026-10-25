package drainpool

import (
	"testing"
	"time"
)

func TestQueuedWorkIsCountedBeforeItStarts(t *testing.T) {
	gate := make(chan struct{})
	finished := make(chan struct{})
	Go(func() { <-gate; close(finished) })
	if Pending() != 1 {
		t.Fatal("submitted work is invisible to drain")
	}
	close(gate)
	<-finished
	deadline := time.Now().Add(time.Second)
	for Pending() != 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if Pending() != 0 {
		t.Fatal("completed work still pending")
	}
}
