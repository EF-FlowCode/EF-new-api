package perfmetrics

import (
	"testing"

	"eflow.local/new-api/setting/operation_setting"
)

func TestShouldRecordRelaySample(t *testing.T) {
	setting := operation_setting.GroupMonitoringSetting{
		AvailabilityExcludeStatusCodes: []int{400, 429},
		AvailabilityExcludeKeywords:    []string{"not implemented"},
		AvailabilityExcludeModels:      []string{"excluded-model"},
	}

	tests := []struct {
		name       string
		modelName  string
		statusCode int
		content    string
		want       bool
	}{
		{"normal success", "gpt-5.6", 0, "", true},
		{"normal server failure", "gpt-5.6", 503, "service unavailable", true},
		{"excluded status code", "gpt-5.6", 429, "rate limited", false},
		{"excluded keyword", "gpt-5.6", 500, "Feature NOT IMPLEMENTED", false},
		{"excluded model success", "excluded-model", 0, "", false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := shouldRecordRelaySample(setting, test.modelName, test.statusCode, test.content)
			if got != test.want {
				t.Errorf("shouldRecordRelaySample(%q, %d, %q) = %v, want %v",
					test.modelName, test.statusCode, test.content, got, test.want)
			}
		})
	}
}
