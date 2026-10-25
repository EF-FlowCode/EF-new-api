package billing_setting

import (
	"encoding/json"
	"fmt"
	"strings"

	"eflow.local/new-api/pkg/billingexpr"
	"eflow.local/new-api/pkg/jsplugin"
	"eflow.local/new-api/setting/ratio_setting"
)

// A projection of ModelPricing.models[name].task_expressions, never a separate
// persistent option. Keys are exact public/canonical model names, then plugin.
const TaskPriorityOption = "billing_setting.task_priority_version"

const TaskExpressionsOption = "billing_setting.task_expressions"

type TaskBillingRule struct {
	Source      string         `json:"source"`
	Plugin      string         `json:"plugin"`
	Model       string         `json:"model"`
	MappedModel string         `json:"mapped_model"`
	Expression  string         `json:"expression,omitempty"`
	Rule        *TaskPriceRule `json:"rule,omitempty"`
	// BillingMode is the stored model billing_mode behind a model_original /
	// model_mapped selection. flat_price must stay distinguishable from
	// tiered_expr there: a token-priced expression evaluates to zero per call.
	BillingMode string `json:"billing_mode,omitempty"`
}

func ValidateTaskExpressions(raw string) error {
	var models map[string]map[string]TaskPriceRule
	if err := json.Unmarshal([]byte(raw), &models); err != nil {
		return err
	}
	if models == nil {
		return fmt.Errorf("task expressions must be an object")
	}
	for name, plugins := range models {
		if strings.TrimSpace(name) == "" || len(plugins) == 0 {
			return fmt.Errorf("empty task pricing record %q", name)
		}
		for plugin, expression := range plugins {
			if !jsplugin.ValidPluginKey(plugin) {
				return fmt.Errorf("invalid task pricing plugin %q", plugin)
			}
			if err := expression.Validate(); err != nil {
				return fmt.Errorf("task rule %s/%s: %w", name, plugin, err)
			}
			if expression.Kind != "amount" {
				if err := expression.ValidateFor(plugin, expression.UpstreamModel); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// Existing non-expression contracts retain two-stage truncation and completion
// settlement. They must not silently become a mapped model's fixed total.
func HasLegacyTaskPrice(name string) bool {
	if _, ok := ratio_setting.GetModelPrice(name, false); ok {
		return true
	}
	if _, ok := ratio_setting.GetDefaultModelPriceMap()[name]; ok {
		return true
	}
	_, ok, _ := ratio_setting.GetModelRatio(name)
	return ok
}

func ResolveTaskBillingRule(plugin, name, mapped string) TaskBillingRule {
	setting := billingSettingSnapshot()
	return SelectTaskBillingRule(setting, plugin, name, mapped, HasLegacyTaskPrice(name))
}

// SelectTaskBillingRule is shared by execution and offline configuration checks.
// Without a plugin-specific override a task always follows the model's own live
// price, before and after migration, so a model price edit applies to the next
// task request. Migration only adds one guard: a model without its own price
// must not silently inherit a mapped model's general price; only an explicit
// mapped plugin rule may apply, otherwise an explicit task rule is required.
func SelectTaskBillingRule(setting BillingSetting, plugin, name, mapped string, hasLegacy bool) TaskBillingRule {
	rule := TaskBillingRule{Plugin: plugin, Model: name, MappedModel: mapped}
	if expression, ok := setting.TaskExpressions[name][plugin]; ok {
		rule.Source, rule.Expression, rule.Rule = "plugin_original", expression.Expression, &expression
		return rule
	}
	// The original price is protected ahead of automatic alias fallback.
	if IsExpressionBillingMode(setting.BillingMode[name]) {
		rule.Source, rule.Expression, rule.BillingMode = "model_original", setting.BillingExpr[name], setting.BillingMode[name]
		return rule
	}
	if hasLegacy {
		rule.Source = "legacy_original"
		return rule
	}
	migrated := setting.TaskPriorityVersion == 1
	if mapped != "" && mapped != name {
		if expression, ok := setting.TaskExpressions[mapped][plugin]; ok {
			rule.Source, rule.Model, rule.Expression, rule.Rule = "plugin_mapped", mapped, expression.Expression, &expression
			return rule
		}
		if !migrated && IsExpressionBillingMode(setting.BillingMode[mapped]) {
			rule.Source, rule.Model, rule.Expression, rule.BillingMode = "model_mapped", mapped, setting.BillingExpr[mapped], setting.BillingMode[mapped]
			return rule
		}
	}
	if migrated {
		rule.Source = "explicit_task_rule_required"
		return rule
	}
	rule.Source = "missing"
	return rule
}

// ExpressionBillingMode is the mode an expression selection is priced with;
// plugin rules carry no model billing_mode and are per-call expressions.
func (r TaskBillingRule) ExpressionBillingMode() string {
	if r.BillingMode != "" {
		return r.BillingMode
	}
	return BillingModeTieredExpr
}

func (r TaskBillingRule) IsExpression() bool {
	return (r.Rule == nil || r.Rule.Kind == "amount") && (r.Source == "plugin_original" || r.Source == "plugin_mapped" || r.Source == "model_original" || r.Source == "model_mapped")
}

// Typed schema checking remains required even for expressions whose current
// request does not execute a u() branch.
func (r TaskBillingRule) Validate(schema map[string]jsplugin.UsageFieldSchema) error {
	if r.Source == "explicit_task_rule_required" {
		return fmt.Errorf("task pricing %s/%s requires an explicit reviewed rule (%s)", r.Plugin, r.Model, r.Source)
	}
	if !r.IsExpression() {
		if r.Rule != nil {
			return r.Rule.ValidateFor(r.Plugin, r.MappedModel)
		}
		return nil
	}
	if r.BillingMode == BillingModeFlatPrice {
		return fmt.Errorf("task pricing %s/%s uses flat token pricing and cannot be billed per call; configure a per-call price or expression", r.Plugin, r.Model)
	}
	if strings.TrimSpace(r.Expression) == "" {
		return fmt.Errorf("selected task pricing rule %s/%s is empty", r.Plugin, r.Model)
	}
	return billingexpr.ValidateUsageExpression(r.Expression, schema)
}
