package operation_setting

import (
	"strings"

	"github.com/QuantumNous/new-api/setting/config"
)

const (
	ModerationModeOff         = "off"
	ModerationModeObserveOnly = "observe_only"
	// ModerationModeEnforce is reserved for future versions where the relay
	// path waits up to a short timeout for moderation results before allowing
	// the request through. The current engine still runs asynchronously even
	// in enforce mode — the relay simply consults a shared waiter rather than
	// blocking on a synchronous HTTP call. See PreflightModerationHook stub.
	ModerationModeEnforce = "enforce"

	ModerationDefaultBaseURL = "https://api.openai.com"
	ModerationDefaultModel   = "omni-moderation-latest"

	// ModerationAutoGroup mirrors RiskControlAutoGroup — auto resolves to a
	// real group during distribute, never directly observed.
	ModerationAutoGroup = "auto"
)

type ModerationSetting struct {
	Enabled              bool              `json:"enabled"`
	Mode                 string            `json:"mode"`
	BaseURL              string            `json:"base_url"`
	Model                string            `json:"model"`
	APIKeys              []string          `json:"api_keys"`
	EventQueueSize       int               `json:"event_queue_size"`
	WorkerCount          int               `json:"worker_count"`
	HTTPTimeoutMS        int               `json:"http_timeout_ms"`
	MaxRetries           int               `json:"max_retries"`
	// RedisQueueEnabled writes events to a Redis-backed write-ahead log
	// so multi-instance deployments don't lose in-flight events on
	// rolling restart. Falls back to memory-only when Redis is
	// unreachable. Strongly recommended for multi-instance setups.
	RedisQueueEnabled    bool              `json:"redis_queue_enabled"`
	SamplingRatePercent  int               `json:"sampling_rate_percent"`
	ImageMaxSizeKB       int               `json:"image_max_size_kb"`
	EnabledGroups        []string          `json:"enabled_groups"`
	GroupModes           map[string]string `json:"group_modes"`
	DebugResultRetainMin int               `json:"debug_result_retain_minutes"`
	// FlaggedRetentionHours keeps moderation_incidents rows where Flagged=true
	// or MaxScore>=FlagScoreThreshold for a longer window than benign rows so
	// downstream client-side handling (e.g. dashboards or remediation
	// pipelines) can pick them up. Default 720h (30 days).
	FlaggedRetentionHours int `json:"flagged_retention_hours"`
	// BenignRetentionHours bounds how long below-threshold incidents stick
	// around. Default 72h.
	BenignRetentionHours    int  `json:"benign_retention_hours"`
	RecordUnmatchedInputs   bool `json:"record_unmatched_inputs"`
}

var moderationSetting = ModerationSetting{
	Enabled:               false,
	Mode:                  ModerationModeOff,
	BaseURL:               ModerationDefaultBaseURL,
	Model:                 ModerationDefaultModel,
	APIKeys:               []string{},
	// 3000 RPM peak / multi-instance sizing: 50 RPS × 100% sampling.
	// 16 workers × OpenAI ~140ms RTT ≈ 114 req/s capacity per instance
	// (≈2.3x single-instance headroom; with 3 instances ≈ 7x). Queue
	// 16384 absorbs ~5min of OpenAI outage at 50 RPS before drops.
	// RedisQueueEnabled defaults on so multi-instance rolling restarts
	// don't lose in-flight events.
	EventQueueSize:        16384,
	WorkerCount:           16,
	HTTPTimeoutMS:         3000,
	MaxRetries:            3,
	RedisQueueEnabled:     true,
	SamplingRatePercent:   100,
	ImageMaxSizeKB:        2048,
	EnabledGroups:         []string{},
	GroupModes:            map[string]string{},
	DebugResultRetainMin:  10,
	FlaggedRetentionHours: 720,
	BenignRetentionHours:  72,
	RecordUnmatchedInputs: false,
}

func isValidModerationMode(mode string) bool {
	switch mode {
	case ModerationModeOff, ModerationModeObserveOnly, ModerationModeEnforce, "":
		return true
	default:
		return false
	}
}

func normalizeModerationGroups(groups []string) []string {
	if len(groups) == 0 {
		return []string{}
	}
	seen := make(map[string]struct{}, len(groups))
	out := make([]string, 0, len(groups))
	for _, g := range groups {
		g = strings.TrimSpace(g)
		if g == "" || g == ModerationAutoGroup {
			continue
		}
		if _, dup := seen[g]; dup {
			continue
		}
		seen[g] = struct{}{}
		out = append(out, g)
	}
	return out
}

func normalizeModerationGroupModes(modes map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range modes {
		k = strings.TrimSpace(k)
		if k == "" || k == ModerationAutoGroup {
			continue
		}
		v = strings.TrimSpace(v)
		if !isValidModerationMode(v) {
			continue
		}
		out[k] = v
	}
	return out
}

func normalizeModerationKeys(keys []string) []string {
	if len(keys) == 0 {
		return []string{}
	}
	seen := make(map[string]struct{}, len(keys))
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		k = strings.TrimSpace(k)
		if k == "" {
			continue
		}
		if _, dup := seen[k]; dup {
			continue
		}
		seen[k] = struct{}{}
		out = append(out, k)
	}
	return out
}

// NormalizeModerationSetting clamps every config field into a sane range and
// strips invalid groups/modes/keys. Idempotent.
func NormalizeModerationSetting(setting *ModerationSetting) {
	if setting == nil {
		return
	}
	if setting.EventQueueSize <= 0 {
		setting.EventQueueSize = 4096
	}
	if setting.WorkerCount <= 0 {
		setting.WorkerCount = 2
	}
	if setting.HTTPTimeoutMS <= 0 {
		setting.HTTPTimeoutMS = 5000
	}
	if setting.MaxRetries < 0 {
		setting.MaxRetries = 3
	}
	if setting.SamplingRatePercent < 0 {
		setting.SamplingRatePercent = 0
	}
	if setting.SamplingRatePercent > 100 {
		setting.SamplingRatePercent = 100
	}
	if setting.ImageMaxSizeKB <= 0 {
		setting.ImageMaxSizeKB = 2048
	}
	if setting.DebugResultRetainMin <= 0 {
		setting.DebugResultRetainMin = 10
	}
	if setting.FlaggedRetentionHours <= 0 {
		setting.FlaggedRetentionHours = 720
	}
	if setting.BenignRetentionHours <= 0 {
		setting.BenignRetentionHours = 72
	}
	setting.BaseURL = strings.TrimRight(strings.TrimSpace(setting.BaseURL), "/")
	if setting.BaseURL == "" {
		setting.BaseURL = ModerationDefaultBaseURL
	}
	setting.Model = strings.TrimSpace(setting.Model)
	if setting.Model == "" {
		setting.Model = ModerationDefaultModel
	}
	if !isValidModerationMode(setting.Mode) || setting.Mode == "" {
		setting.Mode = ModerationModeOff
	}
	setting.EnabledGroups = normalizeModerationGroups(setting.EnabledGroups)
	setting.GroupModes = normalizeModerationGroupModes(setting.GroupModes)
	setting.APIKeys = normalizeModerationKeys(setting.APIKeys)
}

func init() {
	config.GlobalConfig.Register("moderation", &moderationSetting)
}

func GetModerationSetting() *ModerationSetting {
	NormalizeModerationSetting(&moderationSetting)
	return &moderationSetting
}

// IsModerationEnabledForGroup mirrors IsRiskControlEnabledForGroup but with
// independent semantics. The two systems share the truth-table shape on
// purpose so admins do not need to learn two mental models.
func IsModerationEnabledForGroup(setting *ModerationSetting, group string) bool {
	if setting == nil || !setting.Enabled || setting.Mode == ModerationModeOff {
		return false
	}
	if group == "" || group == ModerationAutoGroup {
		return false
	}
	found := false
	for _, g := range setting.EnabledGroups {
		if g == group {
			found = true
			break
		}
	}
	if !found {
		return false
	}
	return EffectiveModerationModeForGroup(setting, group) != ModerationModeOff
}

// EffectiveModerationModeForGroup applies the truth table:
//   - GroupModes[group] absent => off
//   - GroupModes[group] == "" => fallback to global Mode
//   - explicit value          => return as-is
func EffectiveModerationModeForGroup(setting *ModerationSetting, group string) string {
	if setting == nil {
		return ModerationModeOff
	}
	v, ok := setting.GroupModes[group]
	if !ok {
		return ModerationModeOff
	}
	if v == "" {
		return setting.Mode
	}
	return v
}

// MaskModerationKey returns the same shape as the channel multi-key UI so
// admins recognise the format. Used by the GET /config endpoint to avoid
// echoing full secrets back to the browser.
func MaskModerationKey(key string) string {
	key = strings.TrimSpace(key)
	if len(key) <= 4 {
		return strings.Repeat("*", len(key))
	}
	return strings.Repeat("*", len(key)-4) + key[len(key)-4:]
}
