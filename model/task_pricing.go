package model

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	"eflow.local/new-api/pkg/billingexpr"
	"eflow.local/new-api/setting/billing_setting"
	"gorm.io/gorm"
)

func validateChangedTaskExpressions(tx *gorm.DB, next map[string]string) error {
	raw := next[billing_setting.TaskExpressionsOption]
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	if err := billing_setting.ValidateTaskExpressions(raw); err != nil {
		return err
	}
	var rules map[string]map[string]billing_setting.TaskPriceRule
	_ = json.Unmarshal([]byte(raw), &rules)
	if len(rules) == 0 {
		return nil
	}
	current, err := optionValuesForUpdate(tx)
	if err != nil {
		return err
	}
	current, err = expandModelPricing(current)
	if err != nil {
		return err
	}
	var old map[string]map[string]billing_setting.TaskPriceRule
	_ = json.Unmarshal([]byte(current[billing_setting.TaskExpressionsOption]), &old)
	for name, plugins := range rules {
		for plugin, expression := range plugins {
			if reflect.DeepEqual(old[name][plugin], expression) {
				delete(plugins, plugin)
			}
		}
		if len(plugins) == 0 {
			delete(rules, name)
		}
	}
	if len(rules) == 0 {
		return nil
	}
	var infos map[string]BillingUsageInfo
	if current[billing_setting.TaskPriorityOption] == "1" {
		infos, err = getOfflineTaskPricingUsage(tx)
	} else {
		infos, err = GetBillingUsageInfo(tx.Statement.Context, tx)
	}
	if err != nil {
		return err
	}
	for name, plugins := range rules {
		for plugin, expression := range plugins {
			matched := false
			for publicName, info := range infos {
				profile, exists := info.Plugins[plugin]
				if !exists {
					continue
				}
				if profile.UnresolvedTargets || (profile.Status != "ready" && len(profile.Sources) == 0) {
					return fmt.Errorf("task pricing %s/%s cannot resolve binding targets for %s", plugin, name, publicName)
				}
				applies := publicName == name
				for _, source := range profile.Sources {
					applies = applies || source.Model == name
				}
				if !applies {
					continue
				}
				matched = true
				if profile.Status != "ready" {
					return fmt.Errorf("task pricing %s/%s has no unambiguous enabled binding", plugin, name)
				}
				if expression.Kind != "amount" {
					for _, source := range profile.Sources {
						if err := expression.ValidateFor(plugin, source.Model); err != nil {
							return err
						}
					}
				}
				if expression.Kind == "amount" {
					if err := billingexpr.ValidateUsageExpression(expression.Expression, profile.Schema); err != nil {
						return fmt.Errorf("task pricing %s/%s: %w", plugin, name, err)
					}
				}
			}
			if !matched {
				return fmt.Errorf("task pricing %s/%s has no enabled bound model", plugin, name)
			}
		}
	}
	return nil
}

// Display all distinct selected rules when a public model maps to several tails.
// Actual execution resolves against the channel that was selected for the request.
func taskPricingForUsage(name string, info BillingUsageInfo) map[string][]billing_setting.TaskBillingRule {
	result := map[string][]billing_setting.TaskBillingRule{}
	for plugin, profile := range info.Plugins {
		if profile.Status != "ready" {
			continue
		}
		seen := map[string]bool{}
		for _, source := range profile.Sources {
			if seen[source.Model] {
				continue
			}
			seen[source.Model] = true
			result[plugin] = append(result[plugin], billing_setting.ResolveTaskBillingRule(plugin, name, source.Model))
		}
	}
	return result
}
