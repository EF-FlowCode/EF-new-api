package service

import (
	"errors"
	"fmt"
	"strings"
	"sync/atomic"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/types"
)

const (
	ModerationActionObserve = "observe"
	ModerationActionFlag    = "flag"
	// ModerationActionBlock is recorded in incidents and wins the primary
	// pick over flag/observe, but the v3 engine does NOT short-circuit the
	// relay path on it. Reserved for future PreflightModerationHook work.
	ModerationActionBlock = "block"

	ModerationDecisionAllow   = "allow"
	ModerationDecisionObserve = "observe"
	ModerationDecisionFlag    = "flag"
	ModerationDecisionBlock   = "block"

	ModerationInputTypeText  = "text"
	ModerationInputTypeImage = "image"
)

// moderationCategoryDef enumerates the 13 categories OpenAI omni-moderation
// returns as of 2024-09-26 (omni-moderation-latest). The list is hard-coded
// so the rule editor can validate and present a curated dropdown without a
// runtime fetch. AppliedTypeImage marks categories that the upstream may
// score against image inputs (the rest only score text).
type moderationCategoryDef struct {
	Name        string
	Label       string
	ImageScored bool
}

var moderationCategoryDefs = []moderationCategoryDef{
	{Name: "harassment", Label: "harassment"},
	{Name: "harassment/threatening", Label: "harassment/threatening"},
	{Name: "hate", Label: "hate"},
	{Name: "hate/threatening", Label: "hate/threatening"},
	{Name: "illicit", Label: "illicit"},
	{Name: "illicit/violent", Label: "illicit/violent"},
	{Name: "self-harm", Label: "self-harm", ImageScored: true},
	{Name: "self-harm/intent", Label: "self-harm/intent", ImageScored: true},
	{Name: "self-harm/instructions", Label: "self-harm/instructions", ImageScored: true},
	{Name: "sexual", Label: "sexual", ImageScored: true},
	{Name: "sexual/minors", Label: "sexual/minors"},
	{Name: "violence", Label: "violence", ImageScored: true},
	{Name: "violence/graphic", Label: "violence/graphic", ImageScored: true},
}

var moderationCategoryIndex = func() map[string]moderationCategoryDef {
	m := make(map[string]moderationCategoryDef, len(moderationCategoryDefs))
	for _, d := range moderationCategoryDefs {
		m[d.Name] = d
	}
	return m
}()

// IsKnownModerationCategory exposes the category set so the controller layer
// can validate user-authored rules without importing the engine.
func IsKnownModerationCategory(name string) bool {
	_, ok := moderationCategoryIndex[strings.TrimSpace(name)]
	return ok
}

// ModerationCategoryCanScoreImage reports whether the given category may
// receive a score from image inputs in OpenAI responses. Rules that toggle
// applied_input_type=image on a non-image-scored category are rejected at
// validate time so the admin doesn't author a rule that can never fire.
func ModerationCategoryCanScoreImage(name string) bool {
	def, ok := moderationCategoryIndex[strings.TrimSpace(name)]
	return ok && def.ImageScored
}

type compiledModerationRule struct {
	Raw        *model.ModerationRule
	Conditions []types.ModerationCondition
	Groups     map[string]struct{}
}

var moderationRulesAtomic atomic.Value // []*compiledModerationRule

// ReloadModerationRules pulls every enabled rule, parses conditions+groups,
// and atomically swaps the in-memory snapshot. Rules with empty groups,
// invalid JSON, or zero conditions are dropped (and SysLog'd) so the engine
// never silently fires on garbage data.
func ReloadModerationRules() error {
	rules, err := model.ListEnabledModerationRules()
	if err != nil {
		return err
	}
	compiled := make([]*compiledModerationRule, 0, len(rules))
	for _, rule := range rules {
		conds := rule.ParsedConditions()
		if len(conds) == 0 {
			common.SysLog(fmt.Sprintf("moderation rule %q skipped: no conditions", rule.Name))
			continue
		}
		groups := rule.ParsedGroups()
		if len(groups) == 0 {
			common.SysLog(fmt.Sprintf("moderation rule %q skipped: no groups configured", rule.Name))
			continue
		}
		set := make(map[string]struct{}, len(groups))
		for _, g := range groups {
			set[g] = struct{}{}
		}
		compiled = append(compiled, &compiledModerationRule{
			Raw:        rule,
			Conditions: conds,
			Groups:     set,
		})
	}
	moderationRulesAtomic.Store(compiled)
	return nil
}

func currentModerationRules() []*compiledModerationRule {
	if raw := moderationRulesAtomic.Load(); raw != nil {
		if rules, ok := raw.([]*compiledModerationRule); ok {
			return rules
		}
	}
	return nil
}

func moderationRuleAppliesToGroup(rule *compiledModerationRule, group string) bool {
	if rule == nil || len(rule.Groups) == 0 {
		return false
	}
	_, ok := rule.Groups[group]
	return ok
}

// scoreForModerationCondition picks the score to compare against. When
// ApplyInputType is false (the common case) we use the global category score
// — OpenAI already collapses scores across input types into a single number.
// When ApplyInputType is true we additionally require AppliedInputType to
// appear in category_applied_input_types for that category, otherwise the
// condition is treated as not-fired.
func scoreForModerationCondition(result *ModerationResult, cond types.ModerationCondition) (float64, bool) {
	if result == nil {
		return 0, false
	}
	score, ok := result.Categories[cond.Category]
	if !ok {
		return 0, false
	}
	if cond.ApplyInputType {
		want := strings.TrimSpace(cond.AppliedInputType)
		if want == "" {
			return 0, false
		}
		applied := result.AppliedTypes[cond.Category]
		matched := false
		for _, t := range applied {
			if t == want {
				matched = true
				break
			}
		}
		if !matched {
			return 0, false
		}
	}
	return score, true
}

// matchesModerationRule returns whether the rule fires for the given result.
// match_mode "any" => OR; "all" (default) => AND. Conditions whose category
// is missing from the response (or whose AppliedInputType filter excludes
// the response) count as "not satisfied" — short-circuiting AND, ignored by OR.
func matchesModerationRule(rule *compiledModerationRule, result *ModerationResult) (bool, []types.ModerationCondition) {
	if rule == nil || len(rule.Conditions) == 0 {
		return false, nil
	}
	mode := strings.ToLower(strings.TrimSpace(rule.Raw.MatchMode))
	if mode == "" {
		mode = "all"
	}
	matched := make([]types.ModerationCondition, 0, len(rule.Conditions))
	for _, cond := range rule.Conditions {
		score, ok := scoreForModerationCondition(result, cond)
		if !ok {
			if mode == "all" {
				return false, nil
			}
			continue
		}
		if compareRiskMetric(score, cond.Op, cond.Value) {
			matched = append(matched, cond)
			if mode == "any" {
				return true, matched
			}
		} else if mode == "all" {
			return false, nil
		}
	}
	if mode == "any" {
		return len(matched) > 0, matched
	}
	return len(matched) == len(rule.Conditions), matched
}

// EvaluateModerationRules runs every rule applicable to the given group
// against the OpenAI response and returns the matched subset in priority
// order (already sorted because ReloadModerationRules pulls them ordered).
func EvaluateModerationRules(group string, result *ModerationResult) []types.ModerationMatchedRule {
	if group == "" || result == nil {
		return nil
	}
	rules := currentModerationRules()
	out := make([]types.ModerationMatchedRule, 0)
	for _, rule := range rules {
		if !rule.Raw.Enabled {
			continue
		}
		if !moderationRuleAppliesToGroup(rule, group) {
			continue
		}
		hit, conds := matchesModerationRule(rule, result)
		if !hit {
			continue
		}
		out = append(out, types.ModerationMatchedRule{
			RuleID:            rule.Raw.Id,
			Name:              rule.Raw.Name,
			Action:            rule.Raw.Action,
			ScoreWeight:       rule.Raw.ScoreWeight,
			MatchedConditions: conds,
		})
	}
	return out
}

// moderationActionSeverity orders actions for primary-rule selection.
// block > flag > observe > allow.
func moderationActionSeverity(action string) int {
	switch action {
	case ModerationActionBlock:
		return 3
	case ModerationActionFlag:
		return 2
	case ModerationActionObserve:
		return 1
	default:
		return 0
	}
}

// BuildModerationDecision picks the primary rule (highest severity, ties
// broken by the order they came back from the engine, which already
// reflects priority desc) and returns a ModerationDecision. When matched
// is empty the decision is allow with no primary.
func BuildModerationDecision(matched []types.ModerationMatchedRule) types.ModerationDecision {
	if len(matched) == 0 {
		return types.ModerationDecision{Decision: ModerationDecisionAllow}
	}
	primary := matched[0]
	primarySev := moderationActionSeverity(primary.Action)
	for _, m := range matched[1:] {
		if sev := moderationActionSeverity(m.Action); sev > primarySev {
			primary = m
			primarySev = sev
		}
	}
	decision := primary.Action
	if decision == "" {
		decision = ModerationDecisionObserve
	}
	names := make([]string, 0, len(matched))
	for _, m := range matched {
		names = append(names, m.Name)
	}
	return types.ModerationDecision{
		Decision:        decision,
		PrimaryRuleID:   primary.RuleID,
		PrimaryRuleName: primary.Name,
		MatchedRules:    matched,
		Reason:          fmt.Sprintf("matched_rules=%s", strings.Join(names, ",")),
	}
}

// ValidateModerationRule mirrors validateRiskRule's invariant set:
//   - name required
//   - match_mode normalized to all/any (default all)
//   - action normalized to observe/flag/block (default observe)
//   - conditions parsable, non-empty, every category known, value in [0,1],
//     op supported, applied_input_type only on image-scored categories
//   - enabled=true requires non-empty groups (we never silently drop an
//     enabled rule because the admin forgot to bind it)
//
// Like risk validation, this does NOT enforce that groups are inside
// ModerationSetting.EnabledGroups — admins should be able to draft rules
// before flipping the whitelist.
func ValidateModerationRule(rule *model.ModerationRule) error {
	if rule == nil {
		return errors.New("rule is nil")
	}
	rule.Name = strings.TrimSpace(rule.Name)
	if rule.Name == "" {
		return errors.New("rule name is required")
	}
	mode := strings.ToLower(strings.TrimSpace(rule.MatchMode))
	switch mode {
	case "", "all":
		rule.MatchMode = "all"
	case "any":
		rule.MatchMode = "any"
	default:
		return fmt.Errorf("unsupported match_mode: %s", rule.MatchMode)
	}
	action := strings.ToLower(strings.TrimSpace(rule.Action))
	switch action {
	case "", ModerationActionObserve:
		rule.Action = ModerationActionObserve
	case ModerationActionFlag:
		rule.Action = ModerationActionFlag
	case ModerationActionBlock:
		rule.Action = ModerationActionBlock
	default:
		return fmt.Errorf("unsupported action: %s", rule.Action)
	}
	var conds []types.ModerationCondition
	if err := common.UnmarshalJsonStr(rule.Conditions, &conds); err != nil {
		return errors.New("rule conditions must be valid JSON")
	}
	if len(conds) == 0 {
		return errors.New("at least one condition is required")
	}
	for i, cond := range conds {
		cond.Category = strings.TrimSpace(cond.Category)
		if !IsKnownModerationCategory(cond.Category) {
			return fmt.Errorf("condition %d unsupported category: %s", i+1, cond.Category)
		}
		if cond.Value < 0 || cond.Value > 1 {
			return fmt.Errorf("condition %d value must be in [0, 1]", i+1)
		}
		switch cond.Op {
		case ">", ">=", "<", "<=", "==", "!=", "gt", "gte", "lt", "lte":
		default:
			return fmt.Errorf("condition %d unsupported op: %s", i+1, cond.Op)
		}
		if cond.ApplyInputType {
			at := strings.TrimSpace(cond.AppliedInputType)
			switch at {
			case ModerationInputTypeText:
				// every category receives text scores; always allowed
			case ModerationInputTypeImage:
				if !ModerationCategoryCanScoreImage(cond.Category) {
					return fmt.Errorf("condition %d category %s cannot score against image inputs", i+1, cond.Category)
				}
			default:
				return fmt.Errorf("condition %d invalid applied_input_type: %q", i+1, at)
			}
			cond.AppliedInputType = at
		} else {
			cond.AppliedInputType = ""
		}
		conds[i] = cond
	}
	body, err := common.Marshal(conds)
	if err != nil {
		return err
	}
	rule.Conditions = string(body)

	parsedGroups := rule.ParsedGroups()
	if rule.Enabled && len(parsedGroups) == 0 {
		return errors.New("启用规则前必须至少选择一个分组")
	}
	groupsBytes, err := common.Marshal(parsedGroups)
	if err != nil {
		return err
	}
	rule.Groups = string(groupsBytes)
	return nil
}

// CreateModerationRule validates → persists → reloads.
func CreateModerationRule(rule *model.ModerationRule) error {
	if err := ValidateModerationRule(rule); err != nil {
		return err
	}
	if err := model.CreateModerationRule(rule); err != nil {
		return err
	}
	return ReloadModerationRules()
}

func UpdateModerationRule(rule *model.ModerationRule) error {
	if err := ValidateModerationRule(rule); err != nil {
		return err
	}
	if err := model.UpdateModerationRule(rule); err != nil {
		return err
	}
	return ReloadModerationRules()
}

func DeleteModerationRule(id int) error {
	if err := model.DeleteModerationRule(id); err != nil {
		return err
	}
	return ReloadModerationRules()
}

// SeedDefaultModerationRules installs the canonical starter set on a fresh
// install. All seeds ship with Enabled=false and Groups="" so they have zero
// effect until an operator binds groups (matches the v4 default-disabled
// philosophy from the risk-control rollout).
func SeedDefaultModerationRules() error {
	count, err := model.CountModerationRules()
	if err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	defaults := []*model.ModerationRule{
		makeDefaultModerationRule(
			"moderation_minors_strict_block",
			"任何疑似涉及未成年人的内容立即标记为最高风险（启用前请绑定分组）",
			"all",
			ModerationActionBlock,
			100,
			[]types.ModerationCondition{
				{Category: "sexual/minors", Op: ">=", Value: 0.05},
			},
		),
		makeDefaultModerationRule(
			"moderation_violent_illicit_flag",
			"暴力非法行为指引（启用前请绑定分组）",
			"all",
			ModerationActionFlag,
			60,
			[]types.ModerationCondition{
				{Category: "illicit/violent", Op: ">=", Value: 0.5},
			},
		),
		makeDefaultModerationRule(
			"moderation_explicit_text_flag",
			"文本上的色情内容（启用前请绑定分组）",
			"all",
			ModerationActionFlag,
			50,
			[]types.ModerationCondition{
				{Category: "sexual", Op: ">=", Value: 0.6, ApplyInputType: true, AppliedInputType: "text"},
			},
		),
		makeDefaultModerationRule(
			"moderation_image_violence_flag",
			"图像上的暴力内容（启用前请绑定分组）",
			"all",
			ModerationActionFlag,
			50,
			[]types.ModerationCondition{
				{Category: "violence", Op: ">=", Value: 0.7, ApplyInputType: true, AppliedInputType: "image"},
			},
		),
		makeDefaultModerationRule(
			"moderation_high_risk_combo_observe",
			"仇恨或威胁性骚扰任一命中即进入观察（启用前请绑定分组）",
			"any",
			ModerationActionObserve,
			30,
			[]types.ModerationCondition{
				{Category: "hate", Op: ">=", Value: 0.5},
				{Category: "harassment/threatening", Op: ">=", Value: 0.5},
			},
		),
	}
	for _, r := range defaults {
		if err := model.CreateModerationRule(r); err != nil {
			return err
		}
	}
	return nil
}

func makeDefaultModerationRule(name, description, matchMode, action string, scoreWeight int, conditions []types.ModerationCondition) *model.ModerationRule {
	condBytes, _ := common.Marshal(conditions)
	return &model.ModerationRule{
		Name:        name,
		Description: description,
		Enabled:     false,
		MatchMode:   matchMode,
		Action:      action,
		Priority:    scoreWeight,
		ScoreWeight: scoreWeight,
		Conditions:  string(condBytes),
		Groups:      "",
	}
}

// ListModerationCategories powers the rule editor dropdown. Returned in the
// hard-coded order for stable UI; image_scored is exposed so the editor can
// disable the image option on text-only categories.
type ModerationCategoryInfo struct {
	Name        string `json:"name"`
	Label       string `json:"label"`
	ImageScored bool   `json:"image_scored"`
}

func ListModerationCategories() []ModerationCategoryInfo {
	out := make([]ModerationCategoryInfo, 0, len(moderationCategoryDefs))
	for _, d := range moderationCategoryDefs {
		out = append(out, ModerationCategoryInfo{
			Name:        d.Name,
			Label:       d.Label,
			ImageScored: d.ImageScored,
		})
	}
	return out
}
