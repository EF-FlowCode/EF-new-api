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
var SystemName = "New API"
var Footer = ""
var Logo = ""
var TopUpLink = ""

// GroupMonitoringURL is the embed target for the public-facing
// /group-monitoring page. Empty hides the top-nav link entirely.
// Set to e.g. https://status-new.e-flowcode.cc/, or to an internal
// docker-network URL like http://status:3000/ when reverse-proxying.
var GroupMonitoringURL = ""

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

// var ChatLink = ""
// var ChatLink2 = ""
var QuotaPerUnit = 500 * 1000.0 // $0.002 / 1K tokens
// 保留旧变量以兼容历史逻辑，实际展示由 general_setting.quota_display_type 控制
var DisplayInCurrencyEnabled = true
var DisplayTokenStatEnabled = true
var DrawingEnabled = true
var TaskEnabled = true
var DataExportEnabled = true
var DataExportInterval = 5         // unit: minute
var DataExportDefaultTime = "hour" // unit: minute
var DefaultCollapseSidebar = false // default value of collapse sidebar

// Any options with "Secret", "Token" in its key won't be return by GetOptions

var SessionSecret = uuid.New().String()
var CryptoSecret = uuid.New().String()

var OptionMap map[string]string
var OptionMapRWMutex sync.RWMutex

var ItemsPerPage = 10
var MaxRecentItems = 1000

var PasswordLoginEnabled = true
var PasswordRegisterEnabled = true
var EmailVerificationEnabled = false
var GitHubOAuthEnabled = false
var LinuxDOOAuthEnabled = false
var WeChatAuthEnabled = false
var TelegramOAuthEnabled = false
var TurnstileCheckEnabled = false
var RegisterEnabled = true
var InvitationCodeEnabled = false
var InvitationCodeOAuthRequired = false
var InvitationCodeUserGenerateEnabled = false

var EmailDomainRestrictionEnabled = false // 是否启用邮箱域名限制
var EmailAliasRestrictionEnabled = false  // 是否启用邮箱别名限制
var EmailDomainWhitelist = []string{
	"gmail.com",
	"163.com",
	"126.com",
	"qq.com",
	"outlook.com",
	"hotmail.com",
	"icloud.com",
	"yahoo.com",
	"foxmail.com",
}
var EmailLoginAuthServerList = []string{
	"smtp.sendcloud.net",
	"smtp.azurecomm.net",
}

var DebugEnabled bool
var MemoryCacheEnabled bool

var LogConsumeEnabled = true

var TLSInsecureSkipVerify bool
var InsecureTLSConfig = &tls.Config{InsecureSkipVerify: true}

var SMTPServer = ""
var SMTPPort = 587
var SMTPSSLEnabled = false
var SMTPForceAuthLogin = false
var SMTPAccount = ""
var SMTPFrom = ""
var SMTPToken = ""

// eflow: 发信归档 Bcc。非空时，每封外发邮件额外盲送一份到该地址（仅加进 RCPT，
// 不写进邮件头，收件人看不到），用于发件留痕——SMTP 本身不会把副本存进"已发送"。
// 多个地址用分号(;)分隔；空 = 关闭。热更新即时生效。
var SMTPArchiveBcc = ""

// 工单邮件通知配置
var TicketNotifyEnabled = false
var TicketAdminEmail = ""                                  // 多个邮箱使用分号(;)分隔
var TicketReplyAttachmentEnabled = true                    // 管理员回复带附件时，附件随邮件一起发给用户（multipart/mixed）。默认开
var TicketReplyAttachmentMaxBytes int64 = 20 * 1024 * 1024 // 单封邮件附件总字节上限（base64 后体积约 +33%），超限自动回退为不带附件

// eflow: 最低开票金额（人民币元）。一次发票申请所选充值订单的合计金额需 >= 此值才允许提交；
// 0 表示不限制。与发票 UI 展示的 ¥ 同口径（TopUp.Money）。
var TicketInvoiceMinTopupMoney float64 = 0

// 支付成功邮件通知配置
var PaymentNotifyUserEnabled = false  // 支付成功后通知下单用户
var PaymentNotifyAdminEnabled = false // 支付成功后通知管理员
var PaymentAdminEmail = ""            // 管理员收件邮箱，多个以分号分隔

var GitHubClientId = ""
var GitHubClientSecret = ""
var LinuxDOClientId = ""
var LinuxDOClientSecret = ""
var LinuxDOMinimumTrustLevel = 0

var WeChatServerAddress = ""
var WeChatServerToken = ""
var WeChatAccountQRCodeImageURL = ""

var TurnstileSiteKey = ""
var TurnstileSecretKey = ""

var TelegramBotToken = ""
var TelegramBotName = ""

var QuotaForNewUser = 0
var QuotaForInviter = 0
var QuotaForInvitee = 0

// AffiliateEnabled controls both the user-facing referral program and whether
// new inviter/invitee rewards are issued. Existing rewards remain untouched.
var AffiliateEnabled = true
var ChannelDisableThreshold = 5.0
var AutomaticDisableChannelEnabled = false
var AutomaticEnableChannelEnabled = false
var QuotaRemindThreshold = 1000
var PreConsumedQuota = 500

var RetryTimes = 0

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

var (
	FileUploadPermission    = RoleGuestUser
	FileDownloadPermission  = RoleGuestUser
	ImageUploadPermission   = RoleGuestUser
	ImageDownloadPermission = RoleGuestUser
)

// All duration's unit is seconds
// Shouldn't larger then RateLimitKeyExpirationDuration
var (
	GlobalApiRateLimitEnable   bool
	GlobalApiRateLimitNum      int
	GlobalApiRateLimitDuration int64

	GlobalWebRateLimitEnable   bool
	GlobalWebRateLimitNum      int
	GlobalWebRateLimitDuration int64

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
