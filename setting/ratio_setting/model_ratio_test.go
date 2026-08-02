package ratio_setting

import "testing"

func TestGPT56CompletionRatioUsesConfiguredValue(t *testing.T) {
	original := CompletionRatio2JSONString()
	t.Cleanup(func() {
		if err := UpdateCompletionRatioByJSONString(original); err != nil {
			t.Fatalf("restore completion ratios: %v", err)
		}
	})

	if err := UpdateCompletionRatioByJSONString(`{"gpt-5.6-sol":6}`); err != nil {
		t.Fatalf("update completion ratios: %v", err)
	}
	if got := GetCompletionRatioInfo("gpt-5.6-sol"); got.Ratio != 6 || got.Locked {
		t.Fatalf("gpt-5.6-sol completion ratio = %v, locked = %v; want 6, false", got.Ratio, got.Locked)
	}
}
