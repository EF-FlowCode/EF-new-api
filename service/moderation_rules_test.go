package service

import (
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/types"
)

func makeModerationRuleFixture(t *testing.T, name, mode, action string, enabled bool, groups []string, conds []types.ModerationCondition) *compiledModerationRule {
	t.Helper()
	condBytes, _ := common.Marshal(conds)
	groupsBytes, _ := common.Marshal(groups)
	rule := &model.ModerationRule{
		Id:         1,
		Name:       name,
		Enabled:    enabled,
		MatchMode:  mode,
		Action:     action,
		Conditions: string(condBytes),
		Groups:     string(groupsBytes),
	}
	parsed := []types.ModerationCondition{}
	_ = common.UnmarshalJsonStr(rule.Conditions, &parsed)
	set := map[string]struct{}{}
	for _, g := range groups {
		set[g] = struct{}{}
	}
	return &compiledModerationRule{Raw: rule, Conditions: parsed, Groups: set}
}

func TestModerationRuleAllModeRequiresEveryCondition(t *testing.T) {
	rule := makeModerationRuleFixture(t, "all_rule", "all", ModerationActionFlag, true, []string{"vip"},
		[]types.ModerationCondition{
			{Category: "sexual", Op: ">=", Value: 0.5},
			{Category: "violence", Op: ">=", Value: 0.5},
		})
	hit, _ := matchesModerationRule(rule, &ModerationResult{
		Categories: map[string]float64{"sexual": 0.6, "violence": 0.6},
	})
	if !hit {
		t.Fatal("AND rule with both conditions satisfied should fire")
	}
	miss, _ := matchesModerationRule(rule, &ModerationResult{
		Categories: map[string]float64{"sexual": 0.6, "violence": 0.1},
	})
	if miss {
		t.Fatal("AND rule with only one condition should NOT fire")
	}
}

func TestModerationRuleAnyModeNeedsOneCondition(t *testing.T) {
	rule := makeModerationRuleFixture(t, "any_rule", "any", ModerationActionFlag, true, []string{"vip"},
		[]types.ModerationCondition{
			{Category: "sexual", Op: ">=", Value: 0.9},
			{Category: "violence", Op: ">=", Value: 0.9},
		})
	hit, _ := matchesModerationRule(rule, &ModerationResult{
		Categories: map[string]float64{"sexual": 0.95, "violence": 0.1},
	})
	if !hit {
		t.Fatal("OR rule with one condition satisfied should fire")
	}
	miss, _ := matchesModerationRule(rule, &ModerationResult{
		Categories: map[string]float64{"sexual": 0.1, "violence": 0.1},
	})
	if miss {
		t.Fatal("OR rule with no condition satisfied should NOT fire")
	}
}

func TestModerationRuleAppliedInputTypeToggle(t *testing.T) {
	// ApplyInputType=true with image — must check applied_input_types
	rule := makeModerationRuleFixture(t, "image_only", "all", ModerationActionFlag, true, []string{"vip"},
		[]types.ModerationCondition{
			{Category: "violence", Op: ">=", Value: 0.5, ApplyInputType: true, AppliedInputType: "image"},
		})
	resultWithImage := &ModerationResult{
		Categories:   map[string]float64{"violence": 0.7},
		AppliedTypes: map[string][]string{"violence": {"image"}},
	}
	if hit, _ := matchesModerationRule(rule, resultWithImage); !hit {
		t.Fatal("image-only rule should fire when applied_input_types includes image")
	}
	resultTextOnly := &ModerationResult{
		Categories:   map[string]float64{"violence": 0.9},
		AppliedTypes: map[string][]string{"violence": {"text"}},
	}
	if hit, _ := matchesModerationRule(rule, resultTextOnly); hit {
		t.Fatal("image-only rule must NOT fire when only text input was scored")
	}

	// Toggle disabled => applied_input_type ignored, raw score used
	rule2 := makeModerationRuleFixture(t, "any_input", "all", ModerationActionFlag, true, []string{"vip"},
		[]types.ModerationCondition{
			{Category: "violence", Op: ">=", Value: 0.5},
		})
	if hit, _ := matchesModerationRule(rule2, resultTextOnly); !hit {
		t.Fatal("rule without applied_input_type filter should fire on text-only result")
	}
}

func TestEvaluateModerationRulesGroupFiltering(t *testing.T) {
	rule := makeModerationRuleFixture(t, "vip_only", "all", ModerationActionFlag, true, []string{"vip"},
		[]types.ModerationCondition{{Category: "sexual", Op: ">=", Value: 0.5}})
	moderationRulesAtomic.Store([]*compiledModerationRule{rule})
	defer moderationRulesAtomic.Store([]*compiledModerationRule{})
	r := &ModerationResult{Categories: map[string]float64{"sexual": 0.9}}
	if got := EvaluateModerationRules("vip", r); len(got) != 1 {
		t.Fatalf("vip should match, got %d", len(got))
	}
	if got := EvaluateModerationRules("free", r); len(got) != 0 {
		t.Fatalf("free should be filtered, got %d", len(got))
	}
	if got := EvaluateModerationRules("", r); len(got) != 0 {
		t.Fatalf("empty group should be filtered, got %d", len(got))
	}
}

func TestBuildModerationDecisionPicksMostSevere(t *testing.T) {
	matched := []types.ModerationMatchedRule{
		{RuleID: 1, Name: "low", Action: ModerationActionObserve},
		{RuleID: 2, Name: "block", Action: ModerationActionBlock},
		{RuleID: 3, Name: "flag", Action: ModerationActionFlag},
	}
	d := BuildModerationDecision(matched)
	if d.Decision != ModerationDecisionBlock {
		t.Fatalf("Decision=%q want %q", d.Decision, ModerationDecisionBlock)
	}
	if d.PrimaryRuleID != 2 {
		t.Fatalf("primary id=%d want 2", d.PrimaryRuleID)
	}
}

func TestBuildModerationDecisionEmptyMatchesAllow(t *testing.T) {
	d := BuildModerationDecision(nil)
	if d.Decision != ModerationDecisionAllow {
		t.Fatalf("want allow, got %q", d.Decision)
	}
	d2 := BuildModerationDecision([]types.ModerationMatchedRule{})
	if d2.Decision != ModerationDecisionAllow {
		t.Fatalf("want allow on empty, got %q", d2.Decision)
	}
}

func TestValidateModerationRuleRejectsUnknownCategory(t *testing.T) {
	condBytes, _ := common.Marshal([]types.ModerationCondition{
		{Category: "made_up", Op: ">=", Value: 0.5},
	})
	rule := &model.ModerationRule{
		Name:       "bad_category",
		Conditions: string(condBytes),
	}
	if err := ValidateModerationRule(rule); err == nil || !strings.Contains(err.Error(), "unsupported category") {
		t.Fatalf("expected unsupported category error, got %v", err)
	}
}

func TestValidateModerationRuleRejectsScoreOutOfRange(t *testing.T) {
	condBytes, _ := common.Marshal([]types.ModerationCondition{
		{Category: "sexual", Op: ">=", Value: 1.2},
	})
	rule := &model.ModerationRule{
		Name:       "bad_value",
		Conditions: string(condBytes),
	}
	if err := ValidateModerationRule(rule); err == nil || !strings.Contains(err.Error(), "value must be in") {
		t.Fatalf("expected value range error, got %v", err)
	}
}

func TestValidateModerationRuleRejectsImageOnTextOnlyCategory(t *testing.T) {
	// sexual/minors is text-only per OpenAI; image filter must be rejected.
	condBytes, _ := common.Marshal([]types.ModerationCondition{
		{Category: "sexual/minors", Op: ">=", Value: 0.1, ApplyInputType: true, AppliedInputType: "image"},
	})
	rule := &model.ModerationRule{
		Name:       "minors_image",
		Conditions: string(condBytes),
	}
	if err := ValidateModerationRule(rule); err == nil || !strings.Contains(err.Error(), "cannot score against image inputs") {
		t.Fatalf("expected image-not-supported error, got %v", err)
	}
}

func TestValidateModerationRuleRequiresGroupsWhenEnabled(t *testing.T) {
	condBytes, _ := common.Marshal([]types.ModerationCondition{
		{Category: "sexual", Op: ">=", Value: 0.5},
	})
	rule := &model.ModerationRule{
		Name:       "draft",
		Enabled:    true,
		Conditions: string(condBytes),
		Groups:     "[]",
	}
	if err := ValidateModerationRule(rule); err == nil || !strings.Contains(err.Error(), "分组") {
		t.Fatalf("expected groups-required error, got %v", err)
	}
}

func TestValidateModerationRuleAllowsDraftWithoutGroups(t *testing.T) {
	condBytes, _ := common.Marshal([]types.ModerationCondition{
		{Category: "sexual", Op: ">=", Value: 0.5},
	})
	rule := &model.ModerationRule{
		Name:       "draft",
		Enabled:    false,
		Conditions: string(condBytes),
	}
	if err := ValidateModerationRule(rule); err != nil {
		t.Fatalf("disabled draft should be allowed: %v", err)
	}
}

func TestPreviewModerationDecisionCoversAllEnabledRules(t *testing.T) {
	rule1 := makeModerationRuleFixture(t, "vip_rule", "all", ModerationActionFlag, true, []string{"vip"},
		[]types.ModerationCondition{{Category: "sexual", Op: ">=", Value: 0.5}})
	rule2 := makeModerationRuleFixture(t, "free_rule", "all", ModerationActionBlock, true, []string{"free"},
		[]types.ModerationCondition{{Category: "violence", Op: ">=", Value: 0.5}})
	moderationRulesAtomic.Store([]*compiledModerationRule{rule1, rule2})
	defer moderationRulesAtomic.Store([]*compiledModerationRule{})
	r := &ModerationResult{Categories: map[string]float64{"sexual": 0.9, "violence": 0.9}}
	d := previewModerationDecision(r, nil)
	// Preview ignores group filtering and picks most-severe action.
	if d.Decision != ModerationDecisionBlock {
		t.Fatalf("preview should pick block, got %q", d.Decision)
	}
}

func TestModerationCategoryListExposesImageScoredFlag(t *testing.T) {
	cats := ListModerationCategories()
	if len(cats) != len(moderationCategoryDefs) {
		t.Fatalf("category list length mismatch: got %d want %d", len(cats), len(moderationCategoryDefs))
	}
	// pick a known image-scored category
	for _, c := range cats {
		if c.Name == "violence" && !c.ImageScored {
			t.Fatal("violence should be image_scored")
		}
		if c.Name == "sexual/minors" && c.ImageScored {
			t.Fatal("sexual/minors should NOT be image_scored")
		}
	}
}
