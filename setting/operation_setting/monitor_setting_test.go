package operation_setting

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGetMonitorSettingEnvAndModeNormalization(t *testing.T) {
	original := monitorSetting
	t.Cleanup(func() { monitorSetting = original })
	t.Setenv("CHANNEL_TEST_FREQUENCY", "5")
	t.Setenv("CHANNEL_TEST_ENABLED", "false")
	monitorSetting = MonitorSetting{AutoTestChannelEnabled: true, AutoTestChannelMinutes: 20, ChannelTestMode: "invalid"}

	setting := GetMonitorSetting()
	assert.False(t, setting.AutoTestChannelEnabled)
	assert.Equal(t, float64(5), setting.AutoTestChannelMinutes)
	assert.Equal(t, ChannelTestModeScheduledAll, setting.ChannelTestMode)

	t.Setenv("CHANNEL_TEST_FREQUENCY", "")
	monitorSetting.ChannelTestMode = ChannelTestModePassiveRecovery
	assert.Equal(t, ChannelTestModePassiveRecovery, GetMonitorSetting().ChannelTestMode)
}
