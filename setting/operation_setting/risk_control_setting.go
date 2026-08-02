package operation_setting

import (
	"strings"

	"github.com/QuantumNous/new-api/setting/config"
)

const (
	RiskControlModeOff         = "off"
	RiskControlModeObserveOnly = "observe_only"
	RiskControlModeEnforce     = "enforce"

	// RiskControlAutoGroup is reserved and must never appear in EnabledGroups /
	// GroupModes — auto is resolved to a real group during distribute.
	RiskControlAutoGroup = "auto"
)

type RiskControlSetting struct {
	Enabled                bool   `json:"enabled"`
	Mode                   string `json:"mode"`
	TrustedIPHeaderEnabled bool   `json:"trusted_ip_header_enabled"`
	TrustedIPHeader        string `json:"trusted_ip_header"`
	EventQueueSize         int    `json:"event_queue_size"`
	WorkerCount            int    `json:"worker_count"`
	LocalCacheSeconds      int    `json:"local_cache_seconds"`
	RedisTimeoutMS         int    `json:"redis_timeout_ms"`
	DefaultStatusCode      int    `json:"default_status_code"`
	DefaultResponseMessage string `json:"default_response_message"`
	DefaultRecoverMode     string `json:"default_recover_mode"`
	// EnabledGroups is the whitelist of groups that participate in risk control.
	// auto is filtered out during normalization. An empty slice means risk
	// control is disabled for every group (the safe upgrade default).
	EnabledGroups []string `json:"enabled_groups"`
	// GroupModes maps a group name to its per-group mode override.
	// Semantics (see effectiveRiskModeForGroup):
	//   key absent     => off (default disabled)
	//   value == ""    => fallback to global Mode
	//   "off" / "observe_only" / "enforce" => explicit
	// Entries with invalid mode strings are stripped during normalization.
	GroupModes              map[string]string `json:"group_modes"`
	DefaultRecoverAfterSecs int               `json:"default_recover_after_secs"`
	SnapshotRetentionHours  int               `json:"snapshot_retention_hours"`
}

var riskControlSetting = RiskControlSetting{
	Enabled:                 true,
	Mode:                    RiskControlModeObserveOnly,
	TrustedIPHeaderEnabled:  false,
	TrustedIPHeader:         "X-Real-IP",
	EventQueueSize:          8192,
	WorkerCount:             2,
	LocalCacheSeconds:       2,
	RedisTimeoutMS:          30,
	DefaultStatusCode:       429,
	DefaultResponseMessage:  "当前请求触发风控，请稍后再试",
	DefaultRecoverMode:      "ttl",
	DefaultRecoverAfterSecs: 900,
	SnapshotRetentionHours:  72,
	EnabledGroups:           []string{},
	GroupModes:              map[string]string{},
}

func isValidRiskControlMode(mode string) bool {
	switch mode {
	case RiskControlModeOff, RiskControlModeObserveOnly, RiskControlModeEnforce, "":
		return true
	default:
		return false
	}
}

// normalizeEnabledGroups trims, dedupes, drops empty/auto entries. It does
// NOT filter unknown groups against ratio_setting — that would create an
// import cycle. The controller layer is responsible for whitelist filtering
// against the live group ratio map (see controller.UpdateRiskCenterConfig).
func normalizeEnabledGroups(groups []string) []string {
	if len(groups) == 0 {
		return []string{}
	}
	seen := make(map[string]struct{}, len(groups))
	out := make([]string, 0, len(groups))
	for _, g := range groups {
		g = strings.TrimSpace(g)
		if g == "" || g == RiskControlAutoGroup {
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

// normalizeGroupModes drops auto/empty keys and entries with invalid mode strings.
// Keys not in EnabledGroups are KEPT — admins may pre-configure modes before
// flipping the whitelist switch (see DEV_GUIDE §12.3).
func normalizeGroupModes(modes map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range modes {
		k = strings.TrimSpace(k)
		if k == "" || k == RiskControlAutoGroup {
			continue
		}
		v = strings.TrimSpace(v)
		if !isValidRiskControlMode(v) {
			continue
		}
		out[k] = v
	}
	return out
}

func NormalizeRiskControlSetting(setting *RiskControlSetting) {
	if setting == nil {
		return
	}
	if setting.EventQueueSize <= 0 {
		setting.EventQueueSize = 8192
	}
	if setting.WorkerCount <= 0 {
		setting.WorkerCount = 2
	}
	if setting.LocalCacheSeconds <= 0 {
		setting.LocalCacheSeconds = 2
	}
	if setting.RedisTimeoutMS <= 0 {
		setting.RedisTimeoutMS = 30
	}
	if setting.DefaultStatusCode <= 0 {
		setting.DefaultStatusCode = 429
	}
	if setting.DefaultRecoverAfterSecs <= 0 {
		setting.DefaultRecoverAfterSecs = 900
	}
	if setting.DefaultRecoverMode == "" {
		setting.DefaultRecoverMode = "ttl"
	}
	if setting.DefaultResponseMessage == "" {
		setting.DefaultResponseMessage = "当前请求触发风控，请稍后再试"
	}
	setting.TrustedIPHeader = strings.TrimSpace(setting.TrustedIPHeader)
	if !isValidRiskControlMode(setting.Mode) || setting.Mode == "" {
		setting.Mode = RiskControlModeObserveOnly
	}
	if setting.SnapshotRetentionHours <= 0 {
		setting.SnapshotRetentionHours = 72
	}
	setting.EnabledGroups = normalizeEnabledGroups(setting.EnabledGroups)
	setting.GroupModes = normalizeGroupModes(setting.GroupModes)
}

func init() {
	config.GlobalConfig.Register("risk_control", &riskControlSetting)
}

func GetRiskControlSetting() *RiskControlSetting {
	NormalizeRiskControlSetting(&riskControlSetting)
	return &riskControlSetting
}

// IsRiskControlEnabledForGroup returns true when the global switch is on AND the
// group is whitelisted AND the effective mode for the group is not "off".
// Empty group and auto are always rejected. See DEV_GUIDE §11 for the truth
// table. This is the single source of truth for "should this request be
// observed by risk control".
func IsRiskControlEnabledForGroup(setting *RiskControlSetting, group string) bool {
	if setting == nil || !setting.Enabled || setting.Mode == RiskControlModeOff {
		return false
	}
	if group == "" || group == RiskControlAutoGroup {
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
	return EffectiveRiskModeForGroup(setting, group) != RiskControlModeOff
}

// EffectiveRiskModeForGroup resolves the per-group mode using the rules:
//   - GroupModes[group] absent  => off
//   - GroupModes[group] == ""   => fallback to global Mode
//   - explicit value            => return as-is
//
// Both helpers are pure; the caller is responsible for passing a normalized
// setting. They live on the setting package so that controller/service/UI all
// agree on a single semantic.
func EffectiveRiskModeForGroup(setting *RiskControlSetting, group string) string {
	if setting == nil {
		return RiskControlModeOff
	}
	v, ok := setting.GroupModes[group]
	if !ok {
		return RiskControlModeOff
	}
	if v == "" {
		return setting.Mode
	}
	return v
}
