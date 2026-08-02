package service

import (
	"testing"
	"time"
)

func TestKeyRingRoundRobin(t *testing.T) {
	ring := NewModerationKeyRing([]string{"a", "b", "c"})
	got := make([]string, 0, 6)
	for i := 0; i < 6; i++ {
		k, _, ok := ring.NextKey()
		if !ok {
			t.Fatalf("NextKey should always succeed when no cooldown; iter=%d", i)
		}
		got = append(got, k)
	}
	want := []string{"a", "b", "c", "a", "b", "c"}
	for i, k := range got {
		if k != want[i] {
			t.Fatalf("iter %d: got %q want %q (full=%v)", i, k, want[i], got)
		}
	}
}

func TestKeyRingSkipsCooldown(t *testing.T) {
	ring := NewModerationKeyRing([]string{"a", "b", "c"})
	// burn key b for 60s
	_, _, _ = ring.NextKey() // returns a
	_, idxB, _ := ring.NextKey()
	if idxB != 1 {
		t.Fatalf("expected idx 1 (b), got %d", idxB)
	}
	ring.MarkFailed(idxB, 60*time.Second)
	// next call must skip b → c
	_, idxC, ok := ring.NextKey()
	if !ok || idxC != 2 {
		t.Fatalf("expected next to be c (idx=2), got idx=%d ok=%v", idxC, ok)
	}
	// then should wrap back to a, still skipping b
	_, idxA, _ := ring.NextKey()
	if idxA != 0 {
		t.Fatalf("expected wrap to a (idx=0), got %d", idxA)
	}
	_, idxNotB, _ := ring.NextKey()
	if idxNotB == 1 {
		t.Fatalf("b is in cooldown, should not be returned, got idx=%d", idxNotB)
	}
}

func TestKeyRingAllCooldownReturnsFalse(t *testing.T) {
	ring := NewModerationKeyRing([]string{"a", "b"})
	_, idxA, _ := ring.NextKey()
	ring.MarkFailed(idxA, 60*time.Second)
	_, idxB, _ := ring.NextKey()
	ring.MarkFailed(idxB, 60*time.Second)
	if _, _, ok := ring.NextKey(); ok {
		t.Fatal("expected ok=false when every key is cooling down")
	}
}

func TestKeyRingResetClearsCooldowns(t *testing.T) {
	ring := NewModerationKeyRing([]string{"a", "b"})
	_, idx, _ := ring.NextKey()
	ring.MarkFailed(idx, 60*time.Second)
	if ring.CooldownAt(idx) == 0 {
		t.Fatal("cooldown should be set")
	}
	ring.Reset([]string{"x", "y", "z"})
	if ring.Size() != 3 {
		t.Fatalf("expected size 3 after reset, got %d", ring.Size())
	}
	for i := 0; i < 3; i++ {
		if ring.CooldownAt(i) != 0 {
			t.Fatalf("cooldown should be cleared after reset, idx=%d got=%d", i, ring.CooldownAt(i))
		}
	}
}

func TestKeyRingEmptyReturnsFalse(t *testing.T) {
	ring := NewModerationKeyRing(nil)
	if _, _, ok := ring.NextKey(); ok {
		t.Fatal("empty ring should return ok=false")
	}
}
