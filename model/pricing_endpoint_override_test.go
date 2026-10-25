package model

import "testing"

func TestParsePricingGroupEndpointOverridesNormalizesBatchConfig(t *testing.T) {
	overrides, err := ParsePricingGroupEndpointOverrides(`{
  "1_vip_china": {"type": "openai", "path": "/v1/chat/completions", "method": "post"},
  "0_Tools_WOS": "/v1/chat/completions"
}`)
	if err != nil {
		t.Fatalf("ParsePricingGroupEndpointOverrides() error = %v", err)
	}

	if got := overrides["1_vip_china"]; got.Type != "openai" || got.Path != "/v1/chat/completions" || got.Method != "POST" {
		t.Fatalf("unexpected OpenAI override: %+v", got)
	}
	if got := overrides["0_Tools_WOS"]; got.Path != "/v1/chat/completions" || got.Method != "POST" {
		t.Fatalf("unexpected shorthand override: %+v", got)
	}
}

func TestParsePricingGroupEndpointOverridesRejectsInvalidPath(t *testing.T) {
	if _, err := ParsePricingGroupEndpointOverrides(`{"1_vip_china":{"path":"https://upstream.example/v1/messages"}}`); err == nil {
		t.Fatal("expected absolute upstream URL to be rejected")
	}

	if _, err := ParsePricingGroupEndpointOverrides(`{"1_vip_china":{"method":"POST"}}`); err == nil {
		t.Fatal("expected missing path to be rejected")
	}
}

func TestParseModelContextWindowsNormalizesBatchConfig(t *testing.T) {
	windows, err := ParseModelContextWindows(`{"gpt-5.6-sol":262144,"claude-sonnet-4-6":200000}`)
	if err != nil {
		t.Fatalf("ParseModelContextWindows() error = %v", err)
	}
	if windows["gpt-5.6-sol"] != 262144 || windows["claude-sonnet-4-6"] != 200000 {
		t.Fatalf("unexpected context windows: %#v", windows)
	}
}

func TestParseModelContextWindowsRejectsNonPositiveValues(t *testing.T) {
	if _, err := ParseModelContextWindows(`{"broken-model":0}`); err == nil {
		t.Fatal("expected zero context window to be rejected")
	}
}

func TestResolvePricingGroupEndpointOverrideUsesOpenAIFallback(t *testing.T) {
	got := ResolvePricingGroupEndpointOverride(nil, "1_vip_china")
	if got.Type != "openai" || got.Path != "/v1/chat/completions" || got.Method != "POST" {
		t.Fatalf("unexpected fallback endpoint: %+v", got)
	}
}

func TestResolvePricingGroupEndpointOverridePrefersGroupThenDefault(t *testing.T) {
	overrides, err := ParsePricingGroupEndpointOverrides(`{
  "default": {"type": "openai", "path": "/v1/chat/completions"},
  "1_vip_china": {"type": "openai", "path": "/v1/chat/completions"}
}`)
	if err != nil {
		t.Fatalf("ParsePricingGroupEndpointOverrides() error = %v", err)
	}
	if got := ResolvePricingGroupEndpointOverride(overrides, "1_vip_china"); got.Type != "openai" {
		t.Fatalf("group override was not preferred: %+v", got)
	}
	if got := ResolvePricingGroupEndpointOverride(overrides, "0_Tools_WOS"); got.Type != "openai" {
		t.Fatalf("default override was not used: %+v", got)
	}
}

func TestResolveModelContextWindowPrefersModelThenDefault(t *testing.T) {
	windows, err := ParseModelContextWindows(`{"default":128000,"gpt-5.6-sol":262144}`)
	if err != nil {
		t.Fatalf("ParseModelContextWindows() error = %v", err)
	}
	if got := resolveModelContextWindow(windows, "gpt-5.6-sol"); got != 262144 {
		t.Fatalf("model-specific context window = %d, want 262144", got)
	}
	if got := resolveModelContextWindow(windows, "unknown-model"); got != 128000 {
		t.Fatalf("default context window = %d, want 128000", got)
	}
	if got := resolveModelContextWindow(map[string]int64{}, "unknown-model"); got != 0 {
		t.Fatalf("missing context window = %d, want 0", got)
	}
}
