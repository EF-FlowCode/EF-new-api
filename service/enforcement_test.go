package service

import (
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/setting/operation_setting"
)

func TestEnforcementSettingNormalizeFiltersUnknownSources(t *testing.T) {
	cfg := &operation_setting.EnforcementSetting{
		EnabledSources: []string{
			operation_setting.EnforcementSourceModeration,
			"made_up",
			operation_setting.EnforcementSourceModeration,
		},
		BanThresholdPerSource: map[string]int{
			operation_setting.EnforcementSourceRiskDistribution: 5,
			"another_unknown": 7,
		},
		HitEmailMaxPerWindow:  -1,
		HitEmailWindowMinutes: 0,
		BanEmailMaxPerWindow:  0,
		BanEmailWindowMinutes: 0,
		CountWindowHours:      -2,
	}
	operation_setting.NormalizeEnforcementSetting(cfg)
	if len(cfg.EnabledSources) != 1 || cfg.EnabledSources[0] != operation_setting.EnforcementSourceModeration {
		t.Fatalf("EnabledSources should drop unknown + dedupe; got %v", cfg.EnabledSources)
	}
	if _, ok := cfg.BanThresholdPerSource["another_unknown"]; ok {
		t.Fatal("unknown source key must be filtered from BanThresholdPerSource")
	}
	if cfg.HitEmailMaxPerWindow != 3 {
		t.Errorf("HitEmailMaxPerWindow should default to 3, got %d", cfg.HitEmailMaxPerWindow)
	}
	if cfg.HitEmailWindowMinutes != 10 {
		t.Errorf("HitEmailWindowMinutes should default to 10, got %d", cfg.HitEmailWindowMinutes)
	}
	if cfg.BanEmailMaxPerWindow != 3 {
		t.Errorf("BanEmailMaxPerWindow should default to 3, got %d", cfg.BanEmailMaxPerWindow)
	}
	if cfg.BanEmailWindowMinutes != 60 {
		t.Errorf("BanEmailWindowMinutes should default to 60, got %d", cfg.BanEmailWindowMinutes)
	}
	if cfg.CountWindowHours != 24 {
		t.Errorf("CountWindowHours should default to 24 when negative, got %d", cfg.CountWindowHours)
	}
}

func TestEnforcementSourceGate(t *testing.T) {
	cfg := &operation_setting.EnforcementSetting{
		Enabled: true,
		EnabledSources: []string{
			operation_setting.EnforcementSourceRiskDistribution,
		},
	}
	if !operation_setting.IsEnforcementSourceEnabled(cfg, operation_setting.EnforcementSourceRiskDistribution) {
		t.Fatal("risk distribution should be enabled")
	}
	if operation_setting.IsEnforcementSourceEnabled(cfg, operation_setting.EnforcementSourceModeration) {
		t.Fatal("moderation should be disabled (not in enabled_sources)")
	}
	cfg.Enabled = false
	if operation_setting.IsEnforcementSourceEnabled(cfg, operation_setting.EnforcementSourceRiskDistribution) {
		t.Fatal("global disabled should override")
	}
}

func TestEffectiveBanThresholdFallsBackToGlobal(t *testing.T) {
	cfg := &operation_setting.EnforcementSetting{
		BanThreshold: 10,
		BanThresholdPerSource: map[string]int{
			operation_setting.EnforcementSourceModeration: 3,
		},
	}
	if got := operation_setting.EffectiveEnforcementBanThreshold(cfg, operation_setting.EnforcementSourceModeration); got != 3 {
		t.Errorf("moderation override should be 3, got %d", got)
	}
	if got := operation_setting.EffectiveEnforcementBanThreshold(cfg, operation_setting.EnforcementSourceRiskDistribution); got != 10 {
		t.Errorf("risk should fall back to global 10, got %d", got)
	}
}

func TestRenderEnforcementEmailNeverIncludesRuleName(t *testing.T) {
	cfg := operation_setting.GetEnforcementSetting()
	cfg.EmailHitTemplate = operation_setting.DefaultEnforcementHitTemplate
	cfg.EmailBanTemplate = operation_setting.DefaultEnforcementBanTemplate
	cfg.EmailHitSubject = "您的账户触发了平台风控策略"
	cfg.EmailBanSubject = "您的账户已被自动封禁"
	subj, body := renderEnforcementEmail(cfg, "alice", "vip", operation_setting.EnforcementSourceModeration, 2, 5, false, 1700000000)
	if strings.Contains(body, "rule_name") || strings.Contains(body, "moderation_minors_strict_block") {
		t.Fatalf("hit email must never reference rule name: %s", body)
	}
	if !strings.Contains(body, "alice") || !strings.Contains(body, "vip") || !strings.Contains(body, "内容审核") {
		t.Fatalf("hit email did not interpolate variables correctly: %s", body)
	}
	if !strings.Contains(subj, "风控策略") {
		t.Fatalf("subject got mangled: %s", subj)
	}
	bSubj, bBody := renderEnforcementEmail(cfg, "alice", "vip", operation_setting.EnforcementSourceRiskDistribution, 5, 5, true, 1700000000)
	if !strings.Contains(bSubj, "封禁") {
		t.Fatalf("ban subject should mention 封禁: %s", bSubj)
	}
	if !strings.Contains(bBody, "分发检测") {
		t.Fatalf("ban body should localise source: %s", bBody)
	}
}

// TestEnforcementHitNoOpWhenDisabled confirms the public entry point makes
// zero database round-trips when the layer is off — the engines must be
// safe to call EnforcementHit unconditionally.
func TestEnforcementHitNoOpWhenDisabled(t *testing.T) {
	cfg := operation_setting.GetEnforcementSetting()
	prev := cfg.Enabled
	cfg.Enabled = false
	defer func() { cfg.Enabled = prev }()
	// If the function attempted a DB write it would panic in this test
	// process (no DB initialised). A successful no-op is the expected
	// behaviour, so reaching the next line at all proves correctness.
	EnforcementHit(1, "vip", operation_setting.EnforcementSourceModeration, "moderation_flag")
}
