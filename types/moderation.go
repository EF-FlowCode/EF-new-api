package types

// ModerationCondition is a single predicate inside a ModerationRule.
//
//	score := result.Categories[Category]                      // ApplyInputType disabled
//	score := pickScoreOnInputType(result, Category, AppliedInputType)  // enabled
//
// The condition fires when compareRiskMetric(score, Op, Value) is true. We
// reuse the risk-control comparator so all rule editors share the same
// supported operators (>=, >, <=, <, ==, !=).
type ModerationCondition struct {
	Category string  `json:"category"`
	Op       string  `json:"op"`
	Value    float64 `json:"value"`
	// ApplyInputType toggles whether AppliedInputType is consulted. When
	// false (the UI default), the condition matches against the highest
	// score regardless of which input modality OpenAI flagged. When true,
	// the condition only matches when the selected input modality
	// (text / image) appears in the result's category_applied_input_types
	// for this category.
	ApplyInputType   bool   `json:"apply_input_type,omitempty"`
	AppliedInputType string `json:"applied_input_type,omitempty"` // "text" / "image"
}

// ModerationMatchedRule documents one rule that fired for a given evaluation.
// Persisted (as JSON) on ModerationIncident.MatchedRules so admins can audit
// why a request was flagged without re-running the model.
type ModerationMatchedRule struct {
	RuleID            int                   `json:"rule_id"`
	Name              string                `json:"name"`
	Action            string                `json:"action"`
	ScoreWeight       int                   `json:"score_weight"`
	MatchedConditions []ModerationCondition `json:"matched_conditions,omitempty"`
}

// ModerationDecision is the synthesized verdict for one moderation event:
// the union of all matched rules plus a "primary" pick chosen by action
// severity (block > flag > observe). Decision == "allow" when nothing
// matched; in that case nothing is persisted to moderation_incidents
// because the v3 design removed the fallback threshold.
type ModerationDecision struct {
	Decision        string                  `json:"decision"`
	PrimaryRuleID   int                     `json:"primary_rule_id"`
	PrimaryRuleName string                  `json:"primary_rule_name"`
	MatchedRules    []ModerationMatchedRule `json:"matched_rules,omitempty"`
	Reason          string                  `json:"reason,omitempty"`
}
