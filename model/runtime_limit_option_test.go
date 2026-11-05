package model

import "testing"

func TestValidateConfigOptionRuntimeLimitSetting(t *testing.T) {
	rejected := []struct{ key, value string }{
		{"runtime_limit_setting.streaming_timeout_seconds", "29"},
		{"runtime_limit_setting.streaming_timeout_seconds", "3601"},
		{"runtime_limit_setting.streaming_timeout_seconds", "null"},
		{"runtime_limit_setting.error_log_enabled", "1"},
		{"runtime_limit_setting.task_timeout_minutes", "-1"},
		{"runtime_limit_setting.max_request_body_mb", "0"},
		{"runtime_limit_setting.max_request_body_mb", "1025"},
		{"runtime_limit_setting.notify_limit_count", "-1"},
		{"runtime_limit_setting.notify_limit_duration_minutes", "0"},
		{"runtime_limit_setting.response_header_timeout_seconds", "-1"},
		{"runtime_limit_setting.non_stream_response_header_timeout_seconds", "7201"},
	}
	for _, tt := range rejected {
		if err := validateConfigOption(tt.key, tt.value); err == nil {
			t.Errorf("validateConfigOption(%q, %q) unexpectedly succeeded", tt.key, tt.value)
		}
	}

	accepted := []struct{ key, value string }{
		{"runtime_limit_setting.streaming_timeout_seconds", "30"},
		{"runtime_limit_setting.streaming_timeout_seconds", "300"},
		{"runtime_limit_setting.error_log_enabled", "true"},
		{"runtime_limit_setting.error_log_enabled", "false"},
		{"runtime_limit_setting.task_timeout_minutes", "0"},
		{"runtime_limit_setting.task_timeout_minutes", "1440"},
		{"runtime_limit_setting.max_request_body_mb", "128"},
		{"runtime_limit_setting.notify_limit_count", "0"},
		{"runtime_limit_setting.notify_limit_duration_minutes", "10"},
		{"runtime_limit_setting.response_header_timeout_seconds", "0"},
		{"runtime_limit_setting.response_header_timeout_seconds", "120"},
		{"runtime_limit_setting.non_stream_response_header_timeout_seconds", "600"},
	}
	for _, tt := range accepted {
		if err := validateConfigOption(tt.key, tt.value); err != nil {
			t.Errorf("validateConfigOption(%q, %q) = %v", tt.key, tt.value, err)
		}
	}
}
