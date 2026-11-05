package model_setting

import (
	"slices"
	"strings"

	"eflow.local/new-api/setting/config"
)

type ChatCompletionsToResponsesPolicy struct {
	Enabled       bool     `json:"enabled"`
	AllChannels   bool     `json:"all_channels"`
	ChannelIDs    []int    `json:"channel_ids,omitempty"`
	ChannelTypes  []int    `json:"channel_types,omitempty"`
	ModelPatterns []string `json:"model_patterns,omitempty"`
}

func (p ChatCompletionsToResponsesPolicy) IsChannelEnabled(channelID int, channelType int) bool {
	if !p.Enabled {
		return false
	}
	if p.AllChannels {
		return true
	}

	if channelID > 0 && len(p.ChannelIDs) > 0 && slices.Contains(p.ChannelIDs, channelID) {
		return true
	}
	if channelType > 0 && len(p.ChannelTypes) > 0 && slices.Contains(p.ChannelTypes, channelType) {
		return true
	}
	return false
}

// ProtocolRoutingSettings controls eflow protocol routing: native-first channel
// ordering and same-channel Chat fallback.
// Keyword lists classify upstream errors; matching is case-insensitive.
type ProtocolRoutingSettings struct {
	Enabled bool `json:"enabled"`
	// FormatErrorKeywords mark a 400 as a protocol/format error, which allows
	// the same-channel Chat fallback.
	FormatErrorKeywords []string `json:"format_error_keywords"`
	// ContextLengthKeywords and ContentPolicyKeywords mark errors that another
	// protocol or channel cannot fix; they are returned to the client.
	ContextLengthKeywords []string `json:"context_length_keywords"`
	ContentPolicyKeywords []string `json:"content_policy_keywords"`
	// ClientRestrictedKeywords mark a probe 403 caused by an upstream client
	// whitelist (e.g. Claude Code only).
	ClientRestrictedKeywords []string `json:"client_restricted_keywords"`
}

type GlobalSettings struct {
	PassThroughRequestEnabled        bool                             `json:"pass_through_request_enabled"`
	ThinkingModelBlacklist           []string                         `json:"thinking_model_blacklist"`
	ChatCompletionsToResponsesPolicy ChatCompletionsToResponsesPolicy `json:"chat_completions_to_responses_policy"`
	ProtocolRouting                  ProtocolRoutingSettings          `json:"protocol_routing"`
}

func DefaultProtocolRoutingSettings() ProtocolRoutingSettings {
	return ProtocolRoutingSettings{
		Enabled: true,
		FormatErrorKeywords: []string{
			"unsupported", "not supported", "unknown field", "unrecognized",
			"invalid request format", "no route", "not found",
		},
		ContextLengthKeywords: []string{
			"context_length_exceeded", "maximum context length", "prompt is too long", "too many tokens",
		},
		ContentPolicyKeywords: []string{
			"content_policy", "content_filter", "sensitive", "safety",
		},
		ClientRestrictedKeywords: []string{
			"claude code 客户端", "standard claude code client", "client anomaly", "客户端存在异常",
		},
	}
}

// 默认配置
var defaultOpenaiSettings = GlobalSettings{
	PassThroughRequestEnabled: false,
	ThinkingModelBlacklist: []string{
		"moonshotai/kimi-k2-thinking",
		"kimi-k2-thinking",
	},
	ChatCompletionsToResponsesPolicy: ChatCompletionsToResponsesPolicy{
		Enabled:     false,
		AllChannels: true,
	},
	ProtocolRouting: DefaultProtocolRoutingSettings(),
}

// 全局实例
var globalSettings = defaultOpenaiSettings

func init() {
	// 注册到全局配置管理器
	config.GlobalConfig.Register("global", &globalSettings)
}

func GetGlobalSettings() *GlobalSettings {
	setting := config.MustSnapshot[GlobalSettings]("global")
	return &setting
}

// ShouldPreserveThinkingSuffix 判断模型是否配置为保留 thinking/-nothinking/-low/-high/-medium 后缀
func ShouldPreserveThinkingSuffix(modelName string) bool {
	target := strings.TrimSpace(modelName)
	if target == "" {
		return false
	}

	for _, entry := range GetGlobalSettings().ThinkingModelBlacklist {
		if strings.TrimSpace(entry) == target {
			return true
		}
	}
	return false
}
