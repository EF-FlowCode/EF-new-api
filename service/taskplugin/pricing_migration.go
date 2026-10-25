package taskplugin

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"sort"
	"strings"

	"eflow.local/new-api/common"
	"eflow.local/new-api/constant"
	"eflow.local/new-api/model"
	"eflow.local/new-api/pkg/billingexpr"
	host "eflow.local/new-api/relay/channel/task/jsplugin"
	"eflow.local/new-api/setting/billing_setting"
	"eflow.local/new-api/setting/ratio_setting"
)

// Offline planning deliberately ignores enable/active switches. Fixed source
// metadata is required even for paused channels, and no provider hook is run.
func (m *Manager) PlanPricingMigration(ctx context.Context, document model.ModelPricingConfig, channels []model.Channel, state model.TaskPluginSnapshot, sources []model.TaskPluginSourceSnapshot, selfUse bool) (model.TaskPricingMigrationReport, error) {
	report := model.TaskPricingMigrationReport{}
	handles, failures, err := m.fixedPricingHandles(ctx, state, sources)
	if err != nil {
		return report, err
	}
	bindings := map[int]model.TaskPluginProductionBinding{}
	channelIDs := map[int]bool{}
	for _, ch := range channels {
		channelIDs[ch.Id] = true
	}
	for _, b := range state.Production {
		bindings[b.ChannelID] = b
		if !channelIDs[b.ChannelID] {
			report.Blocked = true
			report.Entries = append(report.Entries, model.TaskPricingMigrationEntry{Plugin: b.Key, ChannelID: b.ChannelID, Status: "blocked", Reason: "binding_channel_missing"})
		}
	}
	// Keep the old document untouched until the complete candidate is proven.
	encoded, err := json.Marshal(document)
	if err != nil {
		return report, err
	}
	var candidate model.ModelPricingConfig
	if err = json.Unmarshal(encoded, &candidate); err != nil {
		return report, err
	}
	seen := map[string]bool{}
	selected := map[string]billing_setting.TaskPriceRule{}
	// Factory declarations only identify known missing task bindings. They never
	// authorize execution or supply a replacement for a fixed binding's source.
	unboundDeclarations := map[string][]string{}
	for key, source := range m.factories {
		h, err := host.Compile(source)
		if err != nil {
			return report, err
		}
		unboundDeclarations[key] = h.Meta().Models
	}
	for _, ch := range channels {
		if strings.Trim(ch.Models, ",") == "" {
			if b, ok := bindings[ch.Id]; ok {
				report.Blocked = true
				report.Entries = append(report.Entries, model.TaskPricingMigrationEntry{Plugin: b.Key, ChannelID: ch.Id, Status: "blocked", Reason: "empty_bound_model_list"})
			}
		}
		names := strings.Split(ch.Models, ",")
		// A configured declaration on a fixed plugin is a dormant task price,
		// even if it is not currently exposed in the channel model list.
		if h := handles[ch.Id]; h != nil {
			for name := range document.Models {
				if _, ok := boundModelName(h.Meta().Models, name); ok && !slices.Contains(names, name) {
					names = append(names, name)
				}
			}
		}
		sort.Strings(names)
		for _, name := range names {
			if name == "" {
				continue
			}
			seen[name] = true
			seen[ratio_setting.FormatMatchingModelName(name)] = true
			if strings.HasSuffix(name, ratio_setting.CompactModelSuffix) {
				seen[ratio_setting.CompactWildcardModelKey] = true
			}
			entry := model.TaskPricingMigrationEntry{Model: name, ChannelID: ch.Id}
			b, hasBinding := bindings[ch.Id]
			entry.Plugin = b.Key
			block := func(reason string) { entry.Status, entry.Reason = "blocked", reason; report.Blocked = true }
			if !hasBinding {
				if tail, ok := mappedUsageModel(name, ch.ModelMapping); !ok {
					block("invalid_model_mapping")
				} else if _, declared := boundModelName(unboundDeclarations[model.TaskPluginKeyForChannel(ch.Type)], tail); declared {
					block("declared_task_without_fixed_binding")
				} else {
					entry.Status, entry.Reason = "preserved_unbound", "price_preserved_task_unavailable"
				}
			} else if model.TaskPluginKeyForChannel(ch.Type) != b.Key {
				block("binding_channel_type_mismatch")
			} else if reason := failures[ch.Id]; reason != "" {
				block(reason)
			} else if canonical, found, err := declaredPricingTarget(handles[ch.Id], name, ch.ModelMapping); err != nil {
				block("invalid_model_mapping")
			} else if !found {
				entry.Status, entry.Reason = "outside_fixed_plugin", "model_not_declared"
			} else {
				rule, source, err := legacyTaskPriceRule(document, name, b.Key, canonical, selfUse)
				if err != nil {
					block(err.Error())
				} else {
					schema, _ := handles[ch.Id].Meta().UsageForModel(canonical)
					if rule.Kind == "amount" {
						err = billingexpr.ValidateUsageExpression(rule.Expression, schema)
					} else {
						err = rule.ValidateFor(b.Key, canonical)
					}
					if err != nil {
						block(err.Error())
					} else {
						key := name + "\x00" + b.Key
						// Only stored rules can conflict: a model-price follow is resolved
						// live against each channel's own tail and stores nothing.
						stored := !followsModelTaskPrice(source)
						if old, exists := selected[key]; stored && exists && !reflect.DeepEqual(old, rule) {
							block("conflicting_fixed_model_contracts")
						} else {
							if stored {
								selected[key] = rule
							}
							entry.Status, entry.Reason, entry.Rule = "ready", source, &rule
							var write *billing_setting.TaskPriceRule
							switch {
							case followsModelTaskPrice(source):
								// Tasks follow the model's live price; no copy is frozen.
								entry.Status = "follows_model_price"
							case source == "existing_plugin_original":
								// An override identical to the model-derived rule (typically
								// an earlier migration copy) becomes a live follow again.
								derived, derivedSource, derivedErr := modelTaskPriceRule(document, name, b.Key, canonical, selfUse)
								if derivedErr == nil && followsModelTaskPrice(derivedSource) {
									if sameTaskPriceRule(derived, rule) {
										entry.Status, entry.Reason = "converted_to_follow", derivedSource
									} else {
										entry.Status = "override_differs_from_model"
									}
								}
								if entry.Status != "converted_to_follow" {
									write = &rule
								}
							default:
								write = &rule
							}
							if err = setCandidateTaskRule(&candidate, name, b.Key, write); err != nil {
								return report, err
							}
						}
					}
				}
			}
			report.Entries = append(report.Entries, entry)
		}
	}
	for name, record := range document.Models {
		if !seen[name] {
			report.Entries = append(report.Entries, model.TaskPricingMigrationEntry{Model: name, Status: "preserved_unreferenced", Reason: "price_preserved_follows_model_on_activation"})
		}
		// A general channel reference cannot dispose of a plugin-specific price.
		// Every stored plugin override needs its own fixed-binding evidence.
		var rules map[string]billing_setting.TaskPriceRule
		if raw := record["task_expressions"]; len(raw) > 0 {
			if err := json.Unmarshal(raw, &rules); err != nil {
				return report, err
			}
		}
		for plugin := range rules {
			if _, covered := selected[name+"\x00"+plugin]; !covered {
				report.Blocked = true
				report.Entries = append(report.Entries, model.TaskPricingMigrationEntry{Model: name, Plugin: plugin, Status: "blocked", Reason: "orphan_pricing_requires_disposition"})
			}
		}
	}
	if !report.Blocked {
		candidate.TaskPriorityVersion = 1
		raw, err := json.Marshal(candidate)
		if err != nil {
			return report, err
		}
		report.Candidate = string(raw)
	}
	return report, nil
}

// Sources derived from the model's own general price. Execution reads that
// price live (model_original / legacy_original), so migration never copies it.
func followsModelTaskPrice(source string) bool {
	switch source {
	case "general_original_expression", "original_unit_price", "builtin_default_unit_price", "original_token_ratio", "self_use_token_ratio":
		return true
	}
	return false
}

func sameTaskPriceRule(a, b billing_setting.TaskPriceRule) bool {
	left, err := json.Marshal(a)
	if err != nil {
		return false
	}
	right, err := json.Marshal(b)
	return err == nil && string(left) == string(right)
}

// setCandidateTaskRule stores rule as name's plugin override, or removes that
// override when rule is nil (dropping an emptied task_expressions field).
func setCandidateTaskRule(candidate *model.ModelPricingConfig, name, plugin string, rule *billing_setting.TaskPriceRule) error {
	rules := map[string]billing_setting.TaskPriceRule{}
	if raw := candidate.Models[name]["task_expressions"]; len(raw) > 0 {
		if err := json.Unmarshal(raw, &rules); err != nil {
			return err
		}
	}
	if rule == nil {
		if _, ok := rules[plugin]; !ok {
			return nil
		}
		delete(rules, plugin)
		if len(rules) == 0 {
			delete(candidate.Models[name], "task_expressions")
			return nil
		}
	} else {
		rules[plugin] = *rule
	}
	if candidate.Models[name] == nil {
		candidate.Models[name] = map[string]json.RawMessage{}
	}
	raw, err := json.Marshal(rules)
	if err != nil {
		return err
	}
	candidate.Models[name]["task_expressions"] = raw
	return nil
}

func legacyTaskPriceRule(document model.ModelPricingConfig, name, plugin, tail string, selfUse bool) (billing_setting.TaskPriceRule, string, error) {
	rules := map[string]billing_setting.TaskPriceRule{}
	if raw := document.Models[name]["task_expressions"]; len(raw) > 0 {
		if err := json.Unmarshal(raw, &rules); err != nil {
			return billing_setting.TaskPriceRule{}, "", err
		}
	}
	if rule, ok := rules[plugin]; ok {
		return rule, "existing_plugin_original", nil
	}
	return modelTaskPriceRule(document, name, plugin, tail, selfUse)
}

// modelTaskPriceRule derives the task rule from the model's general price,
// ignoring any plugin override stored on the model itself. Mapped fallbacks
// (existing_plugin_mapped / existing_model_mapped) are still reported for a
// model without its own price.
func modelTaskPriceRule(document model.ModelPricingConfig, name, plugin, tail string, selfUse bool) (billing_setting.TaskPriceRule, string, error) {
	record := document.Models[name]
	var mode, expression string
	_ = json.Unmarshal(record["mode"], &mode)
	_ = json.Unmarshal(record["expression"], &expression)
	if billing_setting.IsExpressionBillingMode(mode) {
		if mode == billing_setting.BillingModeFlatPrice {
			return billing_setting.TaskPriceRule{}, "", fmt.Errorf("flat_token_rule_cannot_price_task")
		}
		amount, err := billingexpr.AmountExpression(expression)
		rule := billing_setting.TaskPriceRule{Version: 1, Kind: "amount", Expression: amount}
		if err == nil {
			err = rule.Validate()
		}
		return rule, "general_original_expression", err
	}
	rule := billing_setting.TaskPriceRule{Version: 1, UpstreamModel: tail, Dimensions: "provider_v1", Settlement: "submit"}
	if common.StringsContains(constant.TaskPricePatches, name) {
		rule.Dimensions = "none"
	}
	lookup := func(field string) (float64, bool, error) {
		matched := ratio_setting.FormatMatchingModelName(name)
		raw, ok := document.Models[matched][field]
		if !ok && strings.HasSuffix(matched, ratio_setting.CompactModelSuffix) {
			raw, ok = document.Models[ratio_setting.CompactWildcardModelKey][field]
		}
		if !ok {
			return 0, false, nil
		}
		var value float64
		err := json.Unmarshal(raw, &value)
		return value, true, err
	}
	price, found, err := lookup("unit_price")
	if err != nil {
		return rule, "", err
	}
	source := "original_unit_price"
	if !found {
		price, found = ratio_setting.GetDefaultModelPriceMap()[name]
		source = "builtin_default_unit_price"
	}
	if found {
		rule.Kind, rule.UnitPrice = "unit_price", &price
		if rule.Dimensions != "none" && billing_setting.TaskUsesWANCompletion(plugin, tail) {
			rule.Settlement = "wan_duration"
		}
	} else {
		ratio, configured, err := lookup("input_ratio")
		if err != nil {
			return rule, "", err
		}
		if !configured && selfUse {
			ratio, configured = 37.5, true
			source = "self_use_token_ratio"
		}
		if !configured {
			// The current resolver already uses a tail expression when the
			// original has no price; preserve that selected contract too.
			var mappedRules map[string]billing_setting.TaskPriceRule
			if raw := document.Models[tail]["task_expressions"]; len(raw) > 0 {
				if err := json.Unmarshal(raw, &mappedRules); err != nil {
					return rule, "", err
				}
			}
			if mapped, ok := mappedRules[plugin]; ok {
				return mapped, "existing_plugin_mapped", mapped.ValidateFor(plugin, tail)
			}
			var mode, expression string
			_ = json.Unmarshal(document.Models[tail]["mode"], &mode)
			_ = json.Unmarshal(document.Models[tail]["expression"], &expression)
			if billing_setting.IsExpressionBillingMode(mode) {
				amount, err := billingexpr.AmountExpression(expression)
				mapped := billing_setting.TaskPriceRule{Version: 1, Kind: "amount", Expression: amount}
				if err == nil {
					err = mapped.Validate()
				}
				return mapped, "existing_model_mapped", err
			}
			return rule, "", fmt.Errorf("missing_price_requires_user_or_site_policy")
		}
		rule.Kind, rule.ModelRatio, rule.RatioConfigured = "token_completion", &ratio, &configured
		if ratio != 0 && rule.Dimensions != "none" {
			rule.Settlement = "tokens"
		}
		if source != "self_use_token_ratio" {
			source = "original_token_ratio"
		}
	}
	return rule, source, rule.ValidateFor(plugin, tail)
}
