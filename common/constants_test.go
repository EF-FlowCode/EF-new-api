package common

import "testing"

func TestLegacyConsolePath(t *testing.T) {
	tests := map[string]string{
		"/console/topup?amount=10": "/wallet?amount=10",
		"/console/log":             "/usage-logs",
		"/console/personal":        "/profile",
		"/console/token":           "/console/token",
	}
	for input, want := range tests {
		if got := LegacyConsolePath(input); got != want {
			t.Fatalf("LegacyConsolePath(%q) = %q, want %q", input, got, want)
		}
	}
}
