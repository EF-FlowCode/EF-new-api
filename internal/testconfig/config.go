package testconfig

import (
	"testing"

	"eflow.local/new-api/common"
	settingconfig "eflow.local/new-api/setting/config"
)

// Set applies a partial global configuration update and restores the complete
// module after the test. Tests should use this instead of mutating getter
// snapshots, which are intentionally isolated from published configuration.
func Set(t testing.TB, name string, values map[string]string) {
	t.Helper()

	current := settingconfig.GlobalConfig.Get(name)
	if current == nil {
		t.Fatalf("configuration module %q is not registered", name)
	}
	original, err := settingconfig.ConfigToMap(current)
	if err != nil {
		t.Fatalf("snapshot configuration module %q: %v", name, err)
	}

	t.Cleanup(func() {
		t.Helper()
		apply(t, name, original)
	})
	apply(t, name, values)
}

// Update applies a partial update without registering cleanup. Use Set once
// first when a test needs to switch the same module through several states.
func Update(t testing.TB, name string, values map[string]string) {
	t.Helper()
	apply(t, name, values)
}

func JSON(t testing.TB, value any) string {
	t.Helper()
	encoded, err := common.Marshal(value)
	if err != nil {
		t.Fatalf("encode configuration value: %v", err)
	}
	return string(encoded)
}

func apply(t testing.TB, name string, values map[string]string) {
	t.Helper()
	handled, err := settingconfig.GlobalConfig.Update(name, values)
	if err != nil {
		t.Fatalf("update configuration module %q: %v", name, err)
	}
	if !handled {
		t.Fatalf("configuration module %q is not registered", name)
	}
}
