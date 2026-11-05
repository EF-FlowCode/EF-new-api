package operation_setting

import (
	"testing"

	"eflow.local/new-api/common"
	"eflow.local/new-api/constant"
	"eflow.local/new-api/setting/config"
	"github.com/stretchr/testify/require"
)

func resetRuntimeLimitSetting(t *testing.T) {
	t.Helper()
	nullValues := map[string]string{
		"streaming_timeout_seconds":                  "null",
		"error_log_enabled":                          "null",
		"task_timeout_minutes":                       "null",
		"max_request_body_mb":                        "null",
		"notify_limit_count":                         "null",
		"notify_limit_duration_minutes":              "null",
		"response_header_timeout_seconds":            "null",
		"non_stream_response_header_timeout_seconds": "null",
	}
	_, err := config.GlobalConfig.Update("runtime_limit_setting", nullValues)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, err := config.GlobalConfig.Update("runtime_limit_setting", nullValues)
		require.NoError(t, err)
	})
}

func TestRuntimeLimitSettingFallsBackToEnvConstants(t *testing.T) {
	resetRuntimeLimitSetting(t)
	oldTimeout, oldErrorLog, oldTask := constant.StreamingTimeout, constant.ErrorLogEnabled, constant.TaskTimeoutMinutes
	oldBody, oldCount, oldMinutes := constant.MaxRequestBodyMB, constant.NotifyLimitCount, constant.NotificationLimitDurationMinute
	t.Cleanup(func() {
		constant.StreamingTimeout, constant.ErrorLogEnabled, constant.TaskTimeoutMinutes = oldTimeout, oldErrorLog, oldTask
		constant.MaxRequestBodyMB, constant.NotifyLimitCount, constant.NotificationLimitDurationMinute = oldBody, oldCount, oldMinutes
	})
	constant.StreamingTimeout, constant.ErrorLogEnabled, constant.TaskTimeoutMinutes = 7, true, 9
	constant.MaxRequestBodyMB, constant.NotifyLimitCount, constant.NotificationLimitDurationMinute = 11, 13, 17

	require.Equal(t, 7, StreamingTimeoutSeconds())
	require.True(t, ErrorLogEnabled())
	require.Equal(t, 9, TaskTimeoutMinutes())
	require.Equal(t, 11, MaxRequestBodyMB())
	require.Equal(t, 11, common.RequestBodyLimitMB())
	require.Equal(t, 13, NotifyLimitCount())
	require.Equal(t, 17, NotifyLimitDurationMinutes())
}

func TestRuntimeLimitSettingSeededFromEnvThenOverriddenByDatabase(t *testing.T) {
	resetRuntimeLimitSetting(t)
	oldTimeout, oldErrorLog := constant.StreamingTimeout, constant.ErrorLogEnabled
	oldHeader := common.RelayResponseHeaderTimeout
	t.Cleanup(func() {
		constant.StreamingTimeout, constant.ErrorLogEnabled = oldTimeout, oldErrorLog
		common.RelayResponseHeaderTimeout = oldHeader
	})
	constant.StreamingTimeout, constant.ErrorLogEnabled = 300, true
	common.RelayResponseHeaderTimeout = 120

	require.NoError(t, InitRuntimeLimitSettingFromEnv())
	exported := config.GlobalConfig.ExportAllConfigs()
	require.Equal(t, "300", exported["runtime_limit_setting.streaming_timeout_seconds"])
	require.Equal(t, "true", exported["runtime_limit_setting.error_log_enabled"])
	require.Equal(t, "120", exported["runtime_limit_setting.response_header_timeout_seconds"])
	require.Equal(t, "120", exported["runtime_limit_setting.non_stream_response_header_timeout_seconds"])

	// Seeded values no longer follow later constant changes.
	constant.StreamingTimeout = 5
	require.Equal(t, 300, StreamingTimeoutSeconds())

	require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{
		"runtime_limit_setting.streaming_timeout_seconds":                  "180",
		"runtime_limit_setting.error_log_enabled":                          "false",
		"runtime_limit_setting.max_request_body_mb":                        "64",
		"runtime_limit_setting.non_stream_response_header_timeout_seconds": "600",
	}))
	require.Equal(t, 180, StreamingTimeoutSeconds())
	require.False(t, ErrorLogEnabled())
	require.Equal(t, 64, common.RequestBodyLimitMB())
	require.Equal(t, 120, ResponseHeaderTimeoutSeconds())
	require.Equal(t, 600, NonStreamResponseHeaderTimeoutSeconds())
}
