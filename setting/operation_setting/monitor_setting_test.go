package operation_setting

import (
	"testing"

	"eflow.local/new-api/internal/testconfig"
	"eflow.local/new-api/setting/config"
	"github.com/stretchr/testify/assert"
)

func TestGetMonitorSettingEnvAndModeNormalization(t *testing.T) {
	t.Setenv("CHANNEL_TEST_FREQUENCY", "5")
	t.Setenv("CHANNEL_TEST_ENABLED", "false")
	testconfig.Set(t, "monitor_setting", map[string]string{
		"auto_test_channel_enabled": "true",
		"auto_test_channel_minutes": "20",
		"channel_test_mode":         "invalid",
		"channel_test_concurrency":  "999",
	})

	setting := GetMonitorSetting()
	assert.False(t, setting.AutoTestChannelEnabled)
	assert.Equal(t, float64(5), setting.AutoTestChannelMinutes)
	assert.Equal(t, ChannelTestModeScheduledAll, setting.ChannelTestMode)
	assert.Equal(t, MaxChannelTestConcurrency, setting.ChannelTestConcurrency)

	raw := config.MustSnapshot[MonitorSetting]("monitor_setting")
	assert.True(t, raw.AutoTestChannelEnabled)
	assert.Equal(t, float64(20), raw.AutoTestChannelMinutes)
	assert.Equal(t, "invalid", raw.ChannelTestMode)
	assert.Equal(t, 999, raw.ChannelTestConcurrency)

	t.Setenv("CHANNEL_TEST_FREQUENCY", "")
	testconfig.Update(t, "monitor_setting", map[string]string{
		"channel_test_mode": ChannelTestModePassiveRecovery,
	})
	assert.Equal(t, ChannelTestModePassiveRecovery, GetMonitorSetting().ChannelTestMode)
	testconfig.Update(t, "monitor_setting", map[string]string{
		"channel_test_mode": ChannelTestModeAutoBanOnly,
	})
	assert.Equal(t, ChannelTestModeAutoBanOnly, GetMonitorSetting().ChannelTestMode)
	assert.Error(t, ValidateChannelTestConcurrency("0"))
	assert.NoError(t, ValidateChannelTestConcurrency("4"))
}
