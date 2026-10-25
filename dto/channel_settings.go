package dto

import (
	"fmt"
	"strings"
)

type ErrorFilterRule struct {
	// 匹配条件（多条件 AND，同类条件内 OR）
	StatusCodes     []int    `json:"status_codes,omitempty"`
	MessageContains []string `json:"message_contains,omitempty"`
	ErrorCodes      []string `json:"error_codes,omitempty"`

	// 执行动作
	Action string `json:"action"`

	// Action=rewrite：透传上游状态码，改写返回消息
	RewriteMessage string `json:"rewrite_message,omitempty"`

	// Action=replace：完全拦截，自定义状态码和消息
	ReplaceStatusCode int    `json:"replace_status_code,omitempty"`
	ReplaceMessage    string `json:"replace_message,omitempty"`
}

// RiskControlHeaderRule 用于将 new-api 内部的请求级数据（用户名、用户 ID、令牌 ID 等）
// 透传给上游，便于上游基于这些标识做风控/审计/限流。
//
// Source 取值：
//   - "username"     => new-api 内部用户名
//   - "user_id"      => new-api 用户 ID
//   - "user_email"   => 用户邮箱
//   - "user_group"   => 用户所属分组
//   - "using_group"  => 实际使用的分组（可能因 auto 跨组重试而变动）
//   - "token_id"     => 令牌 ID
//   - "request_id"   => 当前请求 ID
//   - "custom"       => 使用 Value 中的内容，支持占位符（详见 risk control 实现）
type RiskControlHeaderRule struct {
	Name   string `json:"name"`
	Source string `json:"source,omitempty"`
	Value  string `json:"value,omitempty"`
}

type ChannelSettings struct {
	ForceFormat            bool                    `json:"force_format,omitempty"`
	ThinkingToContent      bool                    `json:"thinking_to_content,omitempty"`
	Proxy                  string                  `json:"proxy"`
	PassThroughBodyEnabled bool                    `json:"pass_through_body_enabled,omitempty"`
	SystemPrompt           string                  `json:"system_prompt,omitempty"`
	SystemPromptOverride   bool                    `json:"system_prompt_override,omitempty"`
	ErrorFilterRules       []ErrorFilterRule       `json:"error_filter_rules,omitempty"`
	RiskControlHeaders     []RiskControlHeaderRule `json:"risk_control_headers,omitempty"`
	RateLimit              *ChannelRateLimit       `json:"rate_limit,omitempty"`
	HTTPProtocol           string                  `json:"http_protocol,omitempty"`
	HTTP2ConnectionShards  int                     `json:"http2_connection_shards,omitempty"`
}

const (
	HTTPProtocolAuto         = "auto"
	HTTPProtocolHTTP1        = "http1"
	MaxHTTP2ConnectionShards = 8
)

func (s *ChannelSettings) ValidateHTTPTransport() error {
	if s == nil {
		return nil
	}
	protocol := strings.ToLower(strings.TrimSpace(s.HTTPProtocol))
	switch protocol {
	case "", HTTPProtocolAuto, HTTPProtocolHTTP1:
	default:
		return fmt.Errorf("invalid http_protocol: %s", s.HTTPProtocol)
	}
	if s.HTTP2ConnectionShards < 0 || s.HTTP2ConnectionShards > MaxHTTP2ConnectionShards {
		return fmt.Errorf("invalid http2_connection_shards: %d", s.HTTP2ConnectionShards)
	}
	if protocol == HTTPProtocolHTTP1 && s.HTTP2ConnectionShards > 1 {
		return fmt.Errorf("http2_connection_shards must be 1 when http_protocol is http1")
	}
	return nil
}

// ChannelRateLimit 渠道级 RPM / 并发限流配置
//
// OnLimit 取值：
//   - "skip"   => 满载后跳过该渠道，由 retry 路由到同分组其他渠道（默认）
//   - "queue"  => 满载后串行排队等待，超 QueueMaxWaitMs 或队列深度 QueueDepth 后回退为 skip
//   - "reject" => 满载后直接返回 429，不再 retry 其他渠道
type ChannelRateLimit struct {
	Enabled        bool   `json:"enabled,omitempty"`
	RPM            int    `json:"rpm,omitempty"`         // 每分钟最大请求数，0 = 不限
	Concurrency    int    `json:"concurrency,omitempty"` // 同时在飞请求数上限，0 = 不限
	OnLimit        string `json:"on_limit,omitempty"`
	QueueMaxWaitMs int    `json:"queue_max_wait_ms,omitempty"`
	QueueDepth     int    `json:"queue_depth,omitempty"`
}

type VertexKeyType string

const (
	VertexKeyTypeJSON   VertexKeyType = "json"
	VertexKeyTypeAPIKey VertexKeyType = "api_key"
)

type AwsKeyType string

const (
	AwsKeyTypeAKSK   AwsKeyType = "ak_sk" // 默认
	AwsKeyTypeApiKey AwsKeyType = "api_key"
)

// AwsApiType selects the Bedrock interface. Empty keeps InvokeModel.
type AwsApiType string

const (
	AwsApiTypeInvokeModel AwsApiType = "invoke_model"
	AwsApiTypeMantle      AwsApiType = "mantle"
)

type ChannelOtherSettings struct {
	// SupportedEndpointTypes optionally limits which inbound API surfaces may
	// select this channel. An empty value preserves the legacy routing behavior.
	SupportedEndpointTypes                []string      `json:"supported_endpoint_types,omitempty"`
	AzureResponsesVersion                 string        `json:"azure_responses_version,omitempty"`
	VertexKeyType                         VertexKeyType `json:"vertex_key_type,omitempty"`   // "json" or "api_key"
	VertexProjectID                       string        `json:"vertex_project_id,omitempty"` // 仅 vertex_key_type=api_key 时使用；JSON 模式从 service account 解析
	OpenRouterEnterprise                  *bool         `json:"openrouter_enterprise,omitempty"`
	ClaudeBetaQuery                       bool          `json:"claude_beta_query,omitempty"`         // Claude 渠道是否强制追加 ?beta=true
	AllowServiceTier                      bool          `json:"allow_service_tier,omitempty"`        // 是否允许 service_tier 透传（默认过滤以避免额外计费）
	AllowInferenceGeo                     bool          `json:"allow_inference_geo,omitempty"`       // 是否允许 inference_geo 透传（仅 Claude，默认过滤以满足数据驻留合规
	AllowSpeed                            bool          `json:"allow_speed,omitempty"`               // 是否允许 speed 透传（仅 Claude，默认过滤以避免意外切换推理速度模式）
	AllowSafetyIdentifier                 bool          `json:"allow_safety_identifier,omitempty"`   // 是否允许 safety_identifier 透传（默认过滤以保护用户隐私）
	DisableStore                          bool          `json:"disable_store,omitempty"`             // 是否禁用 store 透传（默认允许透传，禁用后可能导致 Codex 无法使用）
	AllowIncludeObfuscation               bool          `json:"allow_include_obfuscation,omitempty"` // 是否允许 stream_options.include_obfuscation 透传（默认过滤以避免关闭流混淆保护）
	AwsKeyType                            AwsKeyType    `json:"aws_key_type,omitempty"`
	AwsApiType                            AwsApiType    `json:"aws_api_type,omitempty"`                               // invoke_model（默认）或 mantle
	UpstreamModelUpdateCheckEnabled       bool          `json:"upstream_model_update_check_enabled,omitempty"`        // 是否检测上游模型更新
	UpstreamModelUpdateAutoSyncEnabled    bool          `json:"upstream_model_update_auto_sync_enabled,omitempty"`    // 是否自动同步上游模型更新
	UpstreamModelUpdateLastCheckTime      int64         `json:"upstream_model_update_last_check_time,omitempty"`      // 上次检测时间
	UpstreamModelUpdateLastDetectedModels []string      `json:"upstream_model_update_last_detected_models,omitempty"` // 上次检测到的可加入模型
	UpstreamModelUpdateLastRemovedModels  []string      `json:"upstream_model_update_last_removed_models,omitempty"`  // 上次检测到的可删除模型
	UpstreamModelUpdateIgnoredModels      []string      `json:"upstream_model_update_ignored_models,omitempty"`       // 手动忽略的模型
	RatioSyncAccessToken                  string        `json:"ratio_sync_access_token,omitempty"`                    // 上游价格接口访问令牌
	RatioSyncUserID                       int           `json:"ratio_sync_user_id,omitempty"`                         // 上游 New-Api-User
	// AdvancedCustom is optional for eflow's legacy Custom channel (type 8).
	// It stays configuration-only; the dedicated upstream type 58 is not adopted yet.
	AdvancedCustom *AdvancedCustomConfig `json:"advanced_custom,omitempty"`
}

func (s *ChannelOtherSettings) IsOpenRouterEnterprise() bool {
	if s == nil || s.OpenRouterEnterprise == nil {
		return false
	}
	return *s.OpenRouterEnterprise
}
