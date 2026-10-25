package testdb

import (
	"encoding/hex"
	"strings"
	"testing"
)

func TestUniqueDatabaseName(t *testing.T) {
	first := uniqueDatabaseName(t)
	second := uniqueDatabaseName(t)

	if first == second {
		t.Fatalf("generated duplicate database names: %q", first)
	}
	for _, name := range []string{first, second} {
		if !strings.HasPrefix(name, databasePrefix) {
			t.Fatalf("database name %q does not start with %q", name, databasePrefix)
		}
		suffix := strings.TrimPrefix(name, databasePrefix)
		if len(suffix) != 24 {
			t.Fatalf("database name %q has suffix length %d, want 24", name, len(suffix))
		}
		if _, err := hex.DecodeString(suffix); err != nil {
			t.Fatalf("database name %q has non-hex suffix: %v", name, err)
		}
	}
}

func TestIsManagedDatabaseName(t *testing.T) {
	valid := databasePrefix + strings.Repeat("a1", 12)
	tests := []struct {
		name string
		want bool
	}{
		{name: valid, want: true},
		{name: databasePrefix, want: false},
		{name: databasePrefix + strings.Repeat("a", 23), want: false},
		{name: databasePrefix + strings.Repeat("z", 24), want: false},
		{name: "production", want: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := isManagedDatabaseName(test.name); got != test.want {
				t.Fatalf("isManagedDatabaseName(%q) = %v, want %v", test.name, got, test.want)
			}
		})
	}
}
