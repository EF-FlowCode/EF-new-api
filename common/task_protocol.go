package common

import (
	"math"
	"os"
	"strconv"
	"time"

	"eflow.local/new-api/constant"
)

func taskProtocolDuration(name string, unit, fallback time.Duration, zero bool) time.Duration {
	raw, exists := os.LookupEnv(name)
	if !exists {
		return fallback
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || n < 0 || (!zero && n == 0) || n > math.MaxInt64/int64(unit) {
		SysLog(name + " is invalid; using default")
		return fallback
	}
	return time.Duration(n) * unit
}

func initTaskProtocolEnv() {
	c := constant.TaskProtocolConfig{
		Timeout:   taskProtocolDuration("TASK_PLUGIN_PROTOCOL_TIMEOUT_SECONDS", time.Second, 600*time.Second, false),
		Tick:      taskProtocolDuration("TASK_PLUGIN_PROTOCOL_TICK_MILLISECONDS", time.Millisecond, 2000*time.Millisecond, false),
		Jitter:    taskProtocolDuration("TASK_PLUGIN_PROTOCOL_TICK_JITTER_MILLISECONDS", time.Millisecond, 500*time.Millisecond, true),
		Heartbeat: taskProtocolDuration("TASK_PLUGIN_PROTOCOL_HEARTBEAT_SECONDS", time.Second, 15*time.Second, false),
	}
	if c.Jitter > time.Duration(math.MaxInt64)-c.Tick {
		SysLog("TASK_PLUGIN_PROTOCOL_TICK_MILLISECONDS + jitter overflows; using default tick and jitter")
		c.Tick, c.Jitter = 2000*time.Millisecond, 500*time.Millisecond
	}
	constant.TaskProtocol = c
}
