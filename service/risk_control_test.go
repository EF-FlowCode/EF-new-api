package service

import (
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/types"
)

func makeRule(t *testing.T, name, scope, action string, enabled bool, groups []string, conditions []types.RiskCondition) *model.RiskRule {
	t.Helper()
	condBytes, err := common.Marshal(conditions)
	if err != nil {
		t.Fatalf("Marshal conditions: %v", err)
	}
	groupsBytes, err := common.Marshal(groups)
	if err != nil {
		t.Fatalf("Marshal groups: %v", err)
	}
	return &model.RiskRule{
		Name:       name,
		Scope:      scope,
		Detector:   "distribution",
		MatchMode:  "all",
		Action:     action,
		Enabled:    enabled,
		Groups:     string(groupsBytes),
		Conditions: string(condBytes),
	}
}

func TestValidateRiskRuleRejectsTokenOnlyMetricOnUserScope(t *testing.T) {
	conditions, err := common.Marshal([]types.RiskCondition{
		{Metric: "tokens_per_ip_10m", Op: ">=", Value: 3},
	})
	if err != nil {
		t.Fatalf("Marshal returned error: %v", err)
	}
	rule := &model.RiskRule{
		Name:       "user_scope_with_token_metric",
		Scope:      RiskSubjectTypeUser,
		Detector:   "distribution",
		Action:     RiskActionObserve,
		Conditions: string(conditions),
	}
	err = validateRiskRule(rule)
	if err == nil {
		t.Fatal("validateRiskRule returned nil, want scope validation error")
	}
	if !strings.Contains(err.Error(), "tokens_per_ip_10m") {
		t.Fatalf("validateRiskRule error = %q, want metric name in error", err.Error())
	}
}

func TestValidateRiskRuleRejectsUnsupportedMetric(t *testing.T) {
	conditions, err := common.Marshal([]types.RiskCondition{
		{Metric: "made_up_metric", Op: ">=", Value: 1},
	})
	if err != nil {
		t.Fatalf("Marshal returned error: %v", err)
	}
	rule := &model.RiskRule{
		Name:       "unsupported_metric_rule",
		Scope:      RiskSubjectTypeToken,
		Detector:   "distribution",
		Action:     RiskActionObserve,
		Conditions: string(conditions),
	}
	err = validateRiskRule(rule)
	if err == nil {
		t.Fatal("validateRiskRule returned nil, want unsupported metric error")
	}
	if !strings.Contains(err.Error(), "unsupported risk metric") {
		t.Fatalf("validateRiskRule error = %q, want unsupported metric error", err.Error())
	}
}

// TestValidateRiskRuleRequiresGroupsWhenEnabled enforces the v4 invariant: a
// rule with Enabled=true must have at least one group bound, otherwise the
// engine silently skips it (DEV_GUIDE §5).
func TestValidateRiskRuleRequiresGroupsWhenEnabled(t *testing.T) {
	conditions, _ := common.Marshal([]types.RiskCondition{
		{Metric: "distinct_ip_10m", Op: ">=", Value: 3},
	})
	rule := &model.RiskRule{
		Name:       "rule_enabled_without_groups",
		Scope:      RiskSubjectTypeToken,
		Detector:   "distribution",
		Action:     RiskActionObserve,
		Enabled:    true,
		Conditions: string(conditions),
		Groups:     "[]",
	}
	err := validateRiskRule(rule)
	if err == nil {
		t.Fatal("expected validation error for enabled rule with empty groups")
	}
	if !strings.Contains(err.Error(), "分组") {
		t.Fatalf("expected error to mention groups, got %q", err.Error())
	}
}

// TestValidateRiskRuleAllowsDisabledWithoutGroups documents that drafts may be
// saved without group bindings as long as Enabled=false. This supports the
// "建好规则、稍后启用" workflow.
func TestValidateRiskRuleAllowsDisabledWithoutGroups(t *testing.T) {
	conditions, _ := common.Marshal([]types.RiskCondition{
		{Metric: "distinct_ip_10m", Op: ">=", Value: 3},
	})
	rule := &model.RiskRule{
		Name:       "rule_disabled_draft",
		Scope:      RiskSubjectTypeToken,
		Detector:   "distribution",
		Action:     RiskActionObserve,
		Enabled:    false,
		Conditions: string(conditions),
	}
	if err := validateRiskRule(rule); err != nil {
		t.Fatalf("disabled rule with empty groups should be allowed, got error: %v", err)
	}
}

func TestRuleAppliesToGroup(t *testing.T) {
	rule := &compiledRiskRule{
		Groups: map[string]struct{}{"vip": {}, "free": {}},
	}
	if !ruleAppliesToGroup(rule, "vip") {
		t.Fatal("expected vip to match")
	}
	if ruleAppliesToGroup(rule, "default") {
		t.Fatal("expected default to be unmatched")
	}
	empty := &compiledRiskRule{Groups: map[string]struct{}{}}
	if ruleAppliesToGroup(empty, "vip") {
		t.Fatal("expected empty group set to never match")
	}
}

// TestEvaluateRiskRulesGroupFiltering exercises the §6 scenario matrix:
// rule with groups=[vip] only fires for vip events.
func TestEvaluateRiskRulesGroupFiltering(t *testing.T) {
	cond := []types.RiskCondition{{Metric: "distinct_ip_10m", Op: ">=", Value: 3}}
	rule := makeRule(t, "vip_only", RiskSubjectTypeToken, RiskActionBlock, true, []string{"vip"}, cond)
	parsed := []types.RiskCondition{}
	_ = common.UnmarshalJsonStr(rule.Conditions, &parsed)
	compiled := &compiledRiskRule{
		Raw:        rule,
		Conditions: parsed,
		Groups:     map[string]struct{}{"vip": {}},
	}
	metrics := types.RiskMetrics{DistinctIP10M: 5}
	if got := evaluateRiskRules([]*compiledRiskRule{compiled}, RiskSubjectTypeToken, "vip", metrics); len(got) != 1 {
		t.Fatalf("expected vip event to match rule, got %d", len(got))
	}
	if got := evaluateRiskRules([]*compiledRiskRule{compiled}, RiskSubjectTypeToken, "free", metrics); len(got) != 0 {
		t.Fatalf("expected free event to be filtered, got %d", len(got))
	}
	if got := evaluateRiskRules([]*compiledRiskRule{compiled}, RiskSubjectTypeToken, "", metrics); len(got) != 0 {
		t.Fatalf("expected empty group to short-circuit, got %d", len(got))
	}
}

// TestEffectiveRiskModeForGroup encodes the §11 truth table:
//
//	key absent  => off
//	value == "" => fallback to global Mode
//	explicit    => return as-is
func TestEffectiveRiskModeForGroup(t *testing.T) {
	cfg := &operation_setting.RiskControlSetting{
		Mode: operation_setting.RiskControlModeObserveOnly,
		GroupModes: map[string]string{
			"explicit_enforce": operation_setting.RiskControlModeEnforce,
			"empty_fallback":   "",
			"explicit_off":     operation_setting.RiskControlModeOff,
		},
	}
	cases := []struct {
		group string
		want  string
	}{
		{"missing", operation_setting.RiskControlModeOff},
		{"explicit_enforce", operation_setting.RiskControlModeEnforce},
		{"empty_fallback", operation_setting.RiskControlModeObserveOnly},
		{"explicit_off", operation_setting.RiskControlModeOff},
	}
	for _, tc := range cases {
		if got := operation_setting.EffectiveRiskModeForGroup(cfg, tc.group); got != tc.want {
			t.Errorf("group=%q got=%q want=%q", tc.group, got, tc.want)
		}
	}
}

// TestIsRiskControlEnabledForGroup verifies the double gate (whitelist AND
// effective mode != off) per §11.
func TestIsRiskControlEnabledForGroup(t *testing.T) {
	cfg := &operation_setting.RiskControlSetting{
		Enabled:       true,
		Mode:          operation_setting.RiskControlModeObserveOnly,
		EnabledGroups: []string{"vip", "free"},
		GroupModes: map[string]string{
			"vip":  operation_setting.RiskControlModeEnforce,
			"free": "", // fallback to global observe_only
		},
	}
	cases := []struct {
		group string
		want  bool
	}{
		{"vip", true},
		{"free", true},
		{"default", false},                                    // not in whitelist
		{"", false},                                           // empty group
		{operation_setting.RiskControlAutoGroup, false},       // auto explicitly rejected
		{"observed_listed_no_mode", false},                    // not in GroupModes => off
	}
	cfg.EnabledGroups = append(cfg.EnabledGroups, "observed_listed_no_mode")
	for _, tc := range cases {
		if got := operation_setting.IsRiskControlEnabledForGroup(cfg, tc.group); got != tc.want {
			t.Errorf("group=%q got=%v want=%v", tc.group, got, tc.want)
		}
	}
	// global switch off
	cfg.Enabled = false
	if operation_setting.IsRiskControlEnabledForGroup(cfg, "vip") {
		t.Error("expected disabled when global switch is off")
	}
	cfg.Enabled = true
	cfg.Mode = operation_setting.RiskControlModeOff
	if operation_setting.IsRiskControlEnabledForGroup(cfg, "vip") {
		t.Error("expected disabled when global mode is off")
	}
}

// TestNormalizeFiltersAutoAndInvalidModes exercises the v4 normalize rules:
// auto must never appear in EnabledGroups; GroupModes drops invalid mode
// strings.
func TestNormalizeFiltersAutoAndInvalidModes(t *testing.T) {
	cfg := &operation_setting.RiskControlSetting{
		Enabled:       true,
		Mode:          operation_setting.RiskControlModeObserveOnly,
		EnabledGroups: []string{"vip", "auto", "vip", " ", "free"},
		GroupModes: map[string]string{
			"vip":  operation_setting.RiskControlModeEnforce,
			"auto": operation_setting.RiskControlModeEnforce,
			"free": "garbage",
			"":     operation_setting.RiskControlModeObserveOnly,
			"keep": "",
		},
	}
	operation_setting.NormalizeRiskControlSetting(cfg)
	if got := cfg.EnabledGroups; len(got) != 2 || !contains(got, "vip") || !contains(got, "free") {
		t.Errorf("unexpected EnabledGroups: %v", cfg.EnabledGroups)
	}
	if _, ok := cfg.GroupModes["auto"]; ok {
		t.Error("auto must be filtered from GroupModes")
	}
	if _, ok := cfg.GroupModes["free"]; ok {
		t.Error("invalid mode strings must be dropped")
	}
	if _, ok := cfg.GroupModes[""]; ok {
		t.Error("empty key must be filtered from GroupModes")
	}
	if cfg.GroupModes["keep"] != "" {
		t.Error("empty mode (fallback marker) must survive normalization")
	}
	if cfg.GroupModes["vip"] != operation_setting.RiskControlModeEnforce {
		t.Error("explicit mode must survive")
	}
}

func contains(s []string, x string) bool {
	for _, v := range s {
		if v == x {
			return true
		}
	}
	return false
}
