package operation_setting

import (
	"slices"
	"testing"
)

func TestGroupMonitoringSettingIsAvailabilityExcluded(t *testing.T) {
	setting := GroupMonitoringSetting{
		AvailabilityExcludeStatusCodes: []int{400, 413, 429},
		AvailabilityExcludeKeywords:    []string{"field messages is required", "not implemented"},
		AvailabilityExcludeModels:      []string{"banned-model"},
	}

	tests := []struct {
		name       string
		statusCode int
		modelName  string
		content    string
		want       bool
	}{
		{"keyword substring case-insensitive", 500, "gpt-5.5", "status_code=500, NOT IMPLEMENTED", true},
		{"keyword exact phrase", 500, "gpt-5.5", "status_code=500, not implemented", true},
		{"status code match", 429, "gpt-5.5", "rate limited", true},
		{"model match", 0, "banned-model", "", true},
		{"success no exclusion", 0, "gpt-5.5", "", false},
		{"real 500 not a keyword stays counted", 500, "gpt-5.5", "upstream connection reset", false},
		{"503 not in exclude codes stays counted", 503, "gpt-5.5", "service unavailable", false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := setting.IsAvailabilityExcluded(test.statusCode, test.modelName, test.content)
			if got != test.want {
				t.Errorf("IsAvailabilityExcluded(%d, %q, %q) = %v, want %v",
					test.statusCode, test.modelName, test.content, got, test.want)
			}
		})
	}
}

func TestGroupMonitoringSettingResolveMonitoringGroups(t *testing.T) {
	known := []string{"auto", "0_Tools_Sound", "1_vip_codex", "1_vip_meta", "2_free_claude", "test_codex", "work_china"}
	tests := []struct {
		name  string
		rules []string
		want  []string
	}{
		{"empty", nil, []string{}},
		{"exact names keep legacy behavior", []string{"1_vip_codex", "gone_group"}, []string{"1_vip_codex", "gone_group"}},
		{"prefix rules", []string{"0_*", "1_*", "2_*"}, []string{"0_Tools_Sound", "1_vip_codex", "1_vip_meta", "2_free_claude"}},
		{"negation removes one group", []string{"1_*", "!1_vip_meta"}, []string{"1_vip_codex"}},
		{"negation prefix", []string{"*", "!test_*", "!work_*"}, []string{"0_Tools_Sound", "1_vip_codex", "1_vip_meta", "2_free_claude"}},
		{"exact negation wins over exact", []string{"1_vip_meta", "!1_vip_meta"}, []string{}},
		{"exact include survives prefix negation", []string{"*", "!test_*", "test_codex"}, []string{"0_Tools_Sound", "1_vip_codex", "1_vip_meta", "2_free_claude", "test_codex", "work_china"}},
		{"exact include after prefix", []string{"1_vip_codex", "1_*"}, []string{"1_vip_codex", "1_vip_meta"}},
		{"duplicates and blanks", []string{" 1_vip_codex ", "1_*", "", "!"}, []string{"1_vip_codex", "1_vip_meta"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := GroupMonitoringSetting{MonitoringGroups: test.rules}.ResolveMonitoringGroups(known)
			if !slices.Equal(got, test.want) {
				t.Fatalf("got %v, want %v", got, test.want)
			}
		})
	}
}
