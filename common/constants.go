package common

import (
	"crypto/tls"
	//"os"
	//"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

var StartTime = time.Now().Unix() // unit: second
var Version = "v0.0.0"            // this hard coding will be replaced automatically when building, no need to manually change

// LegacyConsolePath rewrites known legacy /console/* paths to the current
// frontend routes. Unknown paths and query strings are preserved.
func LegacyConsolePath(suffix string) string {
	switch {
	case strings.HasPrefix(suffix, "/console/topup"):
		return strings.Replace(suffix, "/console/topup", "/wallet", 1)
	case strings.HasPrefix(suffix, "/console/log"):
		return strings.Replace(suffix, "/console/log", "/usage-logs", 1)
	case strings.HasPrefix(suffix, "/console/personal"):
		return strings.Replace(suffix, "/console/personal", "/profile", 1)
	}
	return suffix
}

// Any options with "Secret", "Token" in its key won't be return by GetOptions

var SessionSecret = uuid.New().String()
var CryptoSecret = uuid.New().String()
var SessionCookieSecure = false
var SessionCookieTrustedURLs []string

var OptionMap map[string]string
var OptionMapRWMutex sync.RWMutex

var ItemsPerPage = 10
var MaxRecentItems = 1000

var DebugEnabled bool
var MemoryCacheEnabled bool

var TLSInsecureSkipVerify bool
var InsecureTLSConfig = &tls.Config{InsecureSkipVerify: true}

//var RootUserEmail = ""

var IsMasterNode bool

// NodeName 节点名称，从 NODE_NAME 环境变量读取；
// 用于审计日志中标识节点身份，在容器/K8s 部署时比自动探测到的容器内网 IP 更具可读性。
var NodeName = ""

var requestInterval int
var RequestInterval time.Duration

var SyncFrequency int // unit is second

var BatchUpdateEnabled = false
var BatchUpdateInterval int

var RelayTimeout int // unit is second

// RelayResponseHeaderTimeout 限制 relay http client 从发完请求到收到上游响应头的等待时长（秒）。
// 0 = 不限制（Go 默认）。仅覆盖"首字节/响应头之前"的等待，不影响收到响应头之后的流式生成，
// 因此即便全局开启也不会误杀合法的慢生成（健康上游响应头通常秒级返回）。
// 用于根治百炼/DashScope 偶发"接了连接但永不吐响应头"导致请求挂死数十分钟、占满 key 并发槽的问题。
var RelayResponseHeaderTimeout int // unit is second

var RelayIdleConnTimeout int // unit is second
var RelayMaxIdleConns int
var RelayMaxIdleConnsPerHost int

var GeminiSafetySetting string

// https://docs.cohere.com/docs/safety-modes Type; NONE/CONTEXTUAL/STRICT
var CohereSafetySetting string

const (
	RequestIdKey         = "X-Oneapi-Request-Id"
	UpstreamRequestIdKey = "X-Upstream-Request-Id"
)

const (
	RoleGuestUser           = 0
	RoleCommonUser          = 1
	RoleCustomerServiceUser = 5 // 客服：只能访问工单相关管理功能
	RoleFinanceUser         = 7 // 财务：处理发票/退款工单
	RoleAdminUser           = 10
	RoleRootUser            = 100
)

func IsValidateRole(role int) bool {
	return role == RoleGuestUser || role == RoleCommonUser || role == RoleCustomerServiceUser || role == RoleFinanceUser || role == RoleAdminUser || role == RoleRootUser
}

// RoleLabel 返回角色的中文可读名称，主要用于日志/通知场景。
func RoleLabel(role int) string {
	switch role {
	case RoleGuestUser:
		return "访客"
	case RoleCommonUser:
		return "普通用户"
	case RoleCustomerServiceUser:
		return "客服"
	case RoleFinanceUser:
		return "财务"
	case RoleAdminUser:
		return "管理员"
	case RoleRootUser:
		return "超级管理员"
	default:
		return "未知"
	}
}

// All duration's unit is seconds
// Shouldn't larger then RateLimitKeyExpirationDuration
var (
	GlobalApiRateLimitEnable   bool
	GlobalApiRateLimitNum      int
	GlobalApiRateLimitDuration int64

	GlobalWebRateLimitEnable   bool
	GlobalWebRateLimitNum      int
	GlobalWebRateLimitDuration int64

	// 前端构建文件（JS/CSS/图标等）单独计数，默认关闭，不再挤占 Web 页面额度。
	GlobalStaticRateLimitEnable   bool
	GlobalStaticRateLimitNum      int
	GlobalStaticRateLimitDuration int64

	CriticalRateLimitEnable   bool
	CriticalRateLimitNum            = 20
	CriticalRateLimitDuration int64 = 20 * 60

	UploadRateLimitNum            = 10
	UploadRateLimitDuration int64 = 60

	DownloadRateLimitNum            = 10
	DownloadRateLimitDuration int64 = 60

	// Per-user search rate limit (applies after authentication, keyed by user ID)
	SearchRateLimitEnable         = true
	SearchRateLimitNum            = 20
	SearchRateLimitDuration int64 = 60

	// Per-user limit for task status/result polling. These reads never select a
	// channel, so they stay outside the model request/success limits; the
	// separate bucket only bounds lookup and plugin-render CPU.
	TaskQueryRateLimitEnable         = true
	TaskQueryRateLimitNum            = 600
	TaskQueryRateLimitDuration int64 = 60
)

var RateLimitKeyExpirationDuration = 20 * time.Minute

const (
	UserStatusEnabled  = 1 // don't use 0, 0 is the default value!
	UserStatusDisabled = 2 // also don't use 0
)

const (
	TokenStatusEnabled   = 1 // don't use 0, 0 is the default value!
	TokenStatusDisabled  = 2 // also don't use 0
	TokenStatusExpired   = 3
	TokenStatusExhausted = 4
)

const (
	RedemptionCodeStatusEnabled  = 1 // don't use 0, 0 is the default value!
	RedemptionCodeStatusDisabled = 2 // also don't use 0
	RedemptionCodeStatusUsed     = 3 // also don't use 0
)

const (
	ChannelStatusUnknown          = 0
	ChannelStatusEnabled          = 1 // don't use 0, 0 is the default value!
	ChannelStatusManuallyDisabled = 2 // also don't use 0
	ChannelStatusAutoDisabled     = 3
)

const (
	TopUpStatusPending = "pending"
	TopUpStatusSuccess = "success"
	TopUpStatusFailed  = "failed"
	TopUpStatusExpired = "expired"
)
