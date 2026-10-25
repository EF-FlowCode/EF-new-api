package common

import (
	"math"
	"strconv"
	"testing"
	"time"

	"eflow.local/new-api/constant"
	"github.com/stretchr/testify/require"
)

func TestTaskProtocolEnv(t *testing.T) {
	old := constant.TaskProtocol
	t.Cleanup(func() { constant.TaskProtocol = old })
	for _, tc := range []struct {
		value string
		want  time.Duration
	}{
		{"0", 600 * time.Second}, {"-1", 600 * time.Second}, {"abc", 600 * time.Second}, {"", 600 * time.Second},
		{"9223372036854775807", 600 * time.Second}, {"9223372036854775808", 600 * time.Second}, {"7", 7 * time.Second},
	} {
		t.Run(tc.value, func(t *testing.T) {
			t.Setenv("TASK_PLUGIN_PROTOCOL_TIMEOUT_SECONDS", tc.value)
			initTaskProtocolEnv()
			require.Equal(t, tc.want, constant.TaskProtocol.Timeout)
		})
	}
	t.Setenv("TASK_PLUGIN_PROTOCOL_TICK_JITTER_MILLISECONDS", "0")
	initTaskProtocolEnv()
	require.Zero(t, constant.TaskProtocol.Jitter)
	t.Setenv("TASK_PLUGIN_PROTOCOL_TICK_MILLISECONDS", strconv.FormatInt(math.MaxInt64/int64(time.Millisecond), 10))
	t.Setenv("TASK_PLUGIN_PROTOCOL_TICK_JITTER_MILLISECONDS", "500")
	initTaskProtocolEnv()
	require.Equal(t, 2*time.Second, constant.TaskProtocol.Tick)
	require.Equal(t, 500*time.Millisecond, constant.TaskProtocol.Jitter)
}
