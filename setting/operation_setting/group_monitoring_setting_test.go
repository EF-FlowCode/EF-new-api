package operation_setting

import "testing"

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
