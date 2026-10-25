package constant

import "time"

// Observation timeouts only end an HTTP request; they never refund a task.
type TaskProtocolConfig struct {
	Timeout, Tick, Jitter, Heartbeat time.Duration
}

var TaskProtocol = TaskProtocolConfig{
	Timeout:   600 * time.Second,
	Tick:      2000 * time.Millisecond,
	Jitter:    500 * time.Millisecond,
	Heartbeat: 15 * time.Second,
}
