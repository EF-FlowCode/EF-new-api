package operation_setting

import (
	"strconv"

	"eflow.local/new-api/common"
	"eflow.local/new-api/constant"
	"eflow.local/new-api/setting/config"
)

// eflow: 原先只能用环境变量配置的运行参数，改为可在后台修改。
// 启动时用环境变量（未设置时为其默认值）填充，数据库里有值时以数据库为准。
// 字段为 nil 时回落到 constant 中的环境变量值：未经 InitRuntimeLimitSettingFromEnv
// 的进程（如单元测试）仍按直接修改 constant 的方式生效。
type RuntimeLimitSetting struct {
	StreamingTimeoutSeconds    *int  `json:"streaming_timeout_seconds"`
	ErrorLogEnabled            *bool `json:"error_log_enabled"`
	TaskTimeoutMinutes         *int  `json:"task_timeout_minutes"`
	MaxRequestBodyMB           *int  `json:"max_request_body_mb"`
	NotifyLimitCount           *int  `json:"notify_limit_count"`
	NotifyLimitDurationMinutes *int  `json:"notify_limit_duration_minutes"`
	// 等待上游响应头的秒数，0 表示不限制。非流式上游要等结果全部生成完才回响应头，单独设上限。
	ResponseHeaderTimeoutSeconds          *int `json:"response_header_timeout_seconds"`
	NonStreamResponseHeaderTimeoutSeconds *int `json:"non_stream_response_header_timeout_seconds"`
}

// 后台可填写的取值范围，model 层保存前按此校验。
const (
	MinStreamingTimeoutSeconds      = 30
	MaxStreamingTimeoutSeconds      = 3600
	MaxTaskTimeoutMinutes           = 43200
	MinMaxRequestBodyMB             = 1
	MaxMaxRequestBodyMB             = 1024
	MaxNotifyLimitCount             = 1000
	MinNotifyLimitDurationMinutes   = 1
	MaxNotifyLimitDurationMinutes   = 10080
	MaxResponseHeaderTimeoutSeconds = 7200
)

var runtimeLimitSetting = RuntimeLimitSetting{}

func init() {
	config.GlobalConfig.Register("runtime_limit_setting", &runtimeLimitSetting)
	common.RequestBodyLimitMB = MaxRequestBodyMB
}

// InitRuntimeLimitSettingFromEnv 在 common.InitEnv 之后、加载数据库设置之前调用，
// 让后台显示的初始值与环境变量一致。
func InitRuntimeLimitSettingFromEnv() error {
	_, err := config.GlobalConfig.Update("runtime_limit_setting", map[string]string{
		"streaming_timeout_seconds":                  strconv.Itoa(constant.StreamingTimeout),
		"error_log_enabled":                          strconv.FormatBool(constant.ErrorLogEnabled),
		"task_timeout_minutes":                       strconv.Itoa(constant.TaskTimeoutMinutes),
		"max_request_body_mb":                        strconv.Itoa(constant.MaxRequestBodyMB),
		"notify_limit_count":                         strconv.Itoa(constant.NotifyLimitCount),
		"notify_limit_duration_minutes":              strconv.Itoa(constant.NotificationLimitDurationMinute),
		"response_header_timeout_seconds":            strconv.Itoa(common.RelayResponseHeaderTimeout),
		"non_stream_response_header_timeout_seconds": strconv.Itoa(common.RelayResponseHeaderTimeout),
	})
	return err
}

func getRuntimeLimitSetting() RuntimeLimitSetting {
	return config.MustSnapshot[RuntimeLimitSetting]("runtime_limit_setting")
}

func intOrFallback(value *int, fallback int) int {
	if value == nil {
		return fallback
	}
	return *value
}

// StreamingTimeoutSeconds 流式请求距上一条有效数据的最长等待秒数。
func StreamingTimeoutSeconds() int {
	return intOrFallback(getRuntimeLimitSetting().StreamingTimeoutSeconds, constant.StreamingTimeout)
}

// ErrorLogEnabled 是否记录错误日志。
func ErrorLogEnabled() bool {
	if enabled := getRuntimeLimitSetting().ErrorLogEnabled; enabled != nil {
		return *enabled
	}
	return constant.ErrorLogEnabled
}

// TaskTimeoutMinutes 异步任务超时分钟数，0 表示不按超时清理。
func TaskTimeoutMinutes() int {
	return intOrFallback(getRuntimeLimitSetting().TaskTimeoutMinutes, constant.TaskTimeoutMinutes)
}

// MaxRequestBodyMB 请求体（解压后）上限。
func MaxRequestBodyMB() int {
	return intOrFallback(getRuntimeLimitSetting().MaxRequestBodyMB, constant.MaxRequestBodyMB)
}

// NotifyLimitCount 同一用户同类通知在一个窗口内的最多发送次数。
func NotifyLimitCount() int {
	return intOrFallback(getRuntimeLimitSetting().NotifyLimitCount, constant.NotifyLimitCount)
}

// NotifyLimitDurationMinutes 通知限频窗口分钟数。
func NotifyLimitDurationMinutes() int {
	return intOrFallback(getRuntimeLimitSetting().NotifyLimitDurationMinutes, constant.NotificationLimitDurationMinute)
}

// ResponseHeaderTimeoutSeconds 流式请求及其它出站请求等待上游响应头的秒数，0 表示不限制。
func ResponseHeaderTimeoutSeconds() int {
	return intOrFallback(getRuntimeLimitSetting().ResponseHeaderTimeoutSeconds, common.RelayResponseHeaderTimeout)
}

// NonStreamResponseHeaderTimeoutSeconds 非流式转发请求等待上游响应头的秒数，0 表示不限制。
func NonStreamResponseHeaderTimeoutSeconds() int {
	return intOrFallback(getRuntimeLimitSetting().NonStreamResponseHeaderTimeoutSeconds, common.RelayResponseHeaderTimeout)
}
