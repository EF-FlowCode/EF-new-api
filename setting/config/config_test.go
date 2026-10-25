package config

import (
	"fmt"
	"sync"
	"testing"
)

type testConfigWithMap struct {
	Modes map[string]string `json:"modes"`
	Exprs map[string]string `json:"exprs"`
	Name  string            `json:"name"`
}

func TestUpdateConfigFromMap_MapReplacement(t *testing.T) {
	cfg := &testConfigWithMap{
		Modes: map[string]string{
			"model-a": "tiered_expr",
			"model-b": "tiered_expr",
		},
		Exprs: map[string]string{
			"model-a": "p * 5 + c * 25",
			"model-b": "p * 10 + c * 50",
		},
		Name: "billing",
	}

	// Simulate removing model-a: new value only has model-b
	err := UpdateConfigFromMap(cfg, map[string]string{
		"modes": `{"model-b": "tiered_expr"}`,
		"exprs": `{"model-b": "p * 10 + c * 50"}`,
	})
	if err != nil {
		t.Fatalf("UpdateConfigFromMap failed: %v", err)
	}

	if _, ok := cfg.Modes["model-a"]; ok {
		t.Errorf("Modes still contains model-a after it was removed from the update; got %v", cfg.Modes)
	}
	if _, ok := cfg.Exprs["model-a"]; ok {
		t.Errorf("Exprs still contains model-a after it was removed from the update; got %v", cfg.Exprs)
	}

	if cfg.Modes["model-b"] != "tiered_expr" {
		t.Errorf("Modes[model-b] = %q, want %q", cfg.Modes["model-b"], "tiered_expr")
	}
	if cfg.Exprs["model-b"] != "p * 10 + c * 50" {
		t.Errorf("Exprs[model-b] = %q, want %q", cfg.Exprs["model-b"], "p * 10 + c * 50")
	}
}

func TestUpdateConfigFromMap_EmptyMapClearsAll(t *testing.T) {
	cfg := &testConfigWithMap{
		Modes: map[string]string{
			"model-a": "tiered_expr",
		},
		Exprs: map[string]string{
			"model-a": "p * 5 + c * 25",
		},
	}

	err := UpdateConfigFromMap(cfg, map[string]string{
		"modes": `{}`,
		"exprs": `{}`,
	})
	if err != nil {
		t.Fatalf("UpdateConfigFromMap failed: %v", err)
	}

	if len(cfg.Modes) != 0 {
		t.Errorf("Modes should be empty after updating with {}, got %v", cfg.Modes)
	}
	if len(cfg.Exprs) != 0 {
		t.Errorf("Exprs should be empty after updating with {}, got %v", cfg.Exprs)
	}
}

func TestUpdateConfigFromMap_ScalarFieldsUnchanged(t *testing.T) {
	cfg := &testConfigWithMap{
		Modes: map[string]string{"m": "v"},
		Name:  "old",
	}

	err := UpdateConfigFromMap(cfg, map[string]string{
		"name": "new",
	})
	if err != nil {
		t.Fatalf("UpdateConfigFromMap failed: %v", err)
	}

	if cfg.Name != "new" {
		t.Errorf("Name = %q, want %q", cfg.Name, "new")
	}
	// modes was not in configMap, should remain unchanged
	if cfg.Modes["m"] != "v" {
		t.Errorf("Modes should be unchanged, got %v", cfg.Modes)
	}
}

func TestUpdateConfigFromMap_InvalidValueDoesNotPartiallyMutate(t *testing.T) {
	type configWithScalars struct {
		Name    string `json:"name"`
		Enabled bool   `json:"enabled"`
	}
	cfg := &configWithScalars{Name: "before", Enabled: true}
	err := UpdateConfigFromMap(cfg, map[string]string{
		"name":    "after",
		"enabled": "not-a-bool",
	})
	if err == nil {
		t.Fatal("expected invalid bool to return an error")
	}
	if cfg.Name != "before" || !cfg.Enabled {
		t.Fatalf("config was partially mutated after validation failure: %+v", cfg)
	}
}

func TestUpdateConfigFromMapRejectsNonFiniteFloat(t *testing.T) {
	type floatConfig struct {
		Ratio float64 `json:"ratio"`
	}

	for _, value := range []string{"NaN", "+Inf", "-Inf"} {
		t.Run(value, func(t *testing.T) {
			cfg := &floatConfig{Ratio: 1.5}
			if err := UpdateConfigFromMap(cfg, map[string]string{"ratio": value}); err == nil {
				t.Fatalf("UpdateConfigFromMap(%q) unexpectedly succeeded", value)
			}
			if cfg.Ratio != 1.5 {
				t.Fatalf("failed update changed Ratio to %v", cfg.Ratio)
			}
		})
	}
}

func TestUpdateConfigFromMapRejectsInvalidCompatibleIntegers(t *testing.T) {
	type integerConfig struct {
		Signed   int  `json:"signed"`
		Unsigned uint `json:"unsigned"`
	}

	for _, test := range []struct {
		name   string
		values map[string]string
	}{
		{name: "signed NaN", values: map[string]string{"signed": "NaN"}},
		{name: "signed infinity", values: map[string]string{"signed": "+Inf"}},
		{name: "signed fraction", values: map[string]string{"signed": "1.5"}},
		{name: "unsigned NaN", values: map[string]string{"unsigned": "NaN"}},
		{name: "unsigned infinity", values: map[string]string{"unsigned": "+Inf"}},
		{name: "unsigned fraction", values: map[string]string{"unsigned": "1.5"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			cfg := &integerConfig{Signed: 2, Unsigned: 3}
			if err := UpdateConfigFromMap(cfg, test.values); err == nil {
				t.Fatal("UpdateConfigFromMap unexpectedly succeeded")
			}
			if cfg.Signed != 2 || cfg.Unsigned != 3 {
				t.Fatalf("failed update changed config: %+v", cfg)
			}
		})
	}
}

func TestSnapshotIsIsolatedFromPublishedConfig(t *testing.T) {
	manager := NewConfigManager()
	cfg := &testConfigWithMap{
		Modes: map[string]string{"left": "old", "right": "old"},
		Exprs: map[string]string{"model": "old"},
		Name:  "before",
	}
	manager.Register("test", cfg)

	var snapshot testConfigWithMap
	found, err := manager.Snapshot("test", &snapshot)
	if err != nil || !found {
		t.Fatalf("Snapshot() = (%v, %v), want (true, nil)", found, err)
	}
	snapshot.Modes["left"] = "mutated"
	snapshot.Exprs["new"] = "value"
	snapshot.Name = "after"

	var published testConfigWithMap
	if _, err = manager.Snapshot("test", &published); err != nil {
		t.Fatalf("Snapshot() failed: %v", err)
	}
	if published.Modes["left"] != "old" || published.Name != "before" {
		t.Fatalf("snapshot mutation leaked into published config: %+v", published)
	}
	if _, exists := published.Exprs["new"]; exists {
		t.Fatalf("snapshot map mutation leaked into published config: %+v", published.Exprs)
	}
}

func TestRegisterDoesNotRetainCallerOwnedPointer(t *testing.T) {
	manager := NewConfigManager()
	cfg := &testConfigWithMap{
		Modes: map[string]string{"model": "before"},
		Name:  "before",
	}
	manager.Register("test", cfg)

	cfg.Modes["model"] = "after"
	cfg.Name = "after"

	var published testConfigWithMap
	found, err := manager.Snapshot("test", &published)
	if err != nil || !found {
		t.Fatalf("Snapshot() = (%v, %v), want (true, nil)", found, err)
	}
	if published.Name != "before" || published.Modes["model"] != "before" {
		t.Fatalf("caller-owned pointer mutated published config: %+v", published)
	}
}

func TestIsBooleanFieldUsesRegisteredJSONFieldType(t *testing.T) {
	type typedConfig struct {
		Enabled bool   `json:"enabled"`
		Name    string `json:"name"`
	}
	manager := NewConfigManager()
	manager.Register("typed", &typedConfig{})

	if !manager.IsBooleanField("typed", "enabled") {
		t.Fatal("registered bool field was not recognized")
	}
	if manager.IsBooleanField("typed", "name") {
		t.Fatal("string field was recognized as bool")
	}
	if manager.IsBooleanField("missing", "enabled") {
		t.Fatal("unregistered module field was recognized as bool")
	}
}

func TestLoadFromDBDoesNotPublishPartialModules(t *testing.T) {
	type scalarConfig struct {
		Name    string `json:"name"`
		Enabled bool   `json:"enabled"`
	}
	manager := NewConfigManager()
	first := &scalarConfig{Name: "first-before", Enabled: true}
	second := &scalarConfig{Name: "second-before", Enabled: true}
	manager.Register("first", first)
	manager.Register("second", second)

	err := manager.LoadFromDB(map[string]string{
		"first.name":     "first-after",
		"second.enabled": "not-a-bool",
	})
	if err == nil {
		t.Fatal("LoadFromDB() expected an error")
	}
	var publishedFirst scalarConfig
	if found, snapshotErr := manager.Snapshot("first", &publishedFirst); snapshotErr != nil || !found {
		t.Fatalf("Snapshot(first) = (%v, %v), want (true, nil)", found, snapshotErr)
	}
	var publishedSecond scalarConfig
	if found, snapshotErr := manager.Snapshot("second", &publishedSecond); snapshotErr != nil || !found {
		t.Fatalf("Snapshot(second) = (%v, %v), want (true, nil)", found, snapshotErr)
	}
	if publishedFirst.Name != "first-before" || !publishedFirst.Enabled {
		t.Fatalf("first module was partially published: %+v", publishedFirst)
	}
	if publishedSecond.Name != "second-before" || !publishedSecond.Enabled {
		t.Fatalf("second module changed after failed load: %+v", publishedSecond)
	}
}

func TestConcurrentSnapshotsObserveWholeVersions(t *testing.T) {
	manager := NewConfigManager()
	cfg := &testConfigWithMap{Modes: map[string]string{"left": "0", "right": "0"}}
	manager.Register("test", cfg)

	const iterations = 300
	start := make(chan struct{})
	errCh := make(chan error, 1)
	var readers sync.WaitGroup
	for range 4 {
		readers.Add(1)
		go func() {
			defer readers.Done()
			<-start
			for range iterations {
				var snapshot testConfigWithMap
				if _, err := manager.Snapshot("test", &snapshot); err != nil {
					select {
					case errCh <- err:
					default:
					}
					return
				}
				if snapshot.Modes["left"] != snapshot.Modes["right"] {
					select {
					case errCh <- fmt.Errorf("torn snapshot: %+v", snapshot.Modes):
					default:
					}
					return
				}
			}
		}()
	}
	close(start)
	for version := 1; version <= iterations; version++ {
		value := fmt.Sprintf("%d", version)
		handled, err := manager.Update("test", map[string]string{
			"modes": fmt.Sprintf(`{"left":%q,"right":%q}`, value, value),
		})
		if err != nil || !handled {
			t.Fatalf("Update() = (%v, %v), want (true, nil)", handled, err)
		}
	}
	readers.Wait()
	select {
	case err := <-errCh:
		t.Fatal(err)
	default:
	}
}

type testSliceRule struct {
	Name     string            `json:"name"`
	Template map[string]string `json:"template,omitempty"`
	Include  []string          `json:"include,omitempty"`
}

type testConfigWithSlice struct {
	Rules []testSliceRule `json:"rules"`
}

func TestUpdateConfigFromMap_SliceElementsDoNotInheritPreviousValues(t *testing.T) {
	cfg := &testConfigWithSlice{Rules: []testSliceRule{
		{Name: "codex", Template: map[string]string{"header": "codex"}, Include: []string{"codex"}},
		{Name: "claude", Template: map[string]string{"header": "claude"}},
	}}

	err := UpdateConfigFromMap(cfg, map[string]string{
		"rules": `[{"name":"custom","template":{"extra":"v"}}]`,
	})
	if err != nil {
		t.Fatalf("UpdateConfigFromMap failed: %v", err)
	}
	if len(cfg.Rules) != 1 {
		t.Fatalf("rules = %+v, want one rule", cfg.Rules)
	}
	rule := cfg.Rules[0]
	if rule.Name != "custom" || len(rule.Include) != 0 {
		t.Errorf("rule inherited the previous element: %+v", rule)
	}
	if len(rule.Template) != 1 || rule.Template["extra"] != "v" {
		t.Errorf("template was merged with the previous element: %v", rule.Template)
	}
}
