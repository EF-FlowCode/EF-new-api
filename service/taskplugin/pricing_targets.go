package taskplugin

import (
	"context"
	"fmt"
	"strings"

	"eflow.local/new-api/common"
	"eflow.local/new-api/model"
	"eflow.local/new-api/pkg/jsplugin"
	host "eflow.local/new-api/relay/channel/task/jsplugin"
)

// Schema readiness is independent of operational enable switches. This is used
// only to edit explicit task rules, so a paused binding can be priced safely
// before activation; it never authorizes a request or a general u() expression.
func (m *Manager) OfflineTaskPricingUsage(ctx context.Context, channels []model.Channel, state model.TaskPluginSnapshot, sources []model.TaskPluginSourceSnapshot) (map[string]model.BillingUsageInfo, error) {
	handles, failures, err := m.fixedPricingHandles(ctx, state, sources)
	if err != nil {
		return nil, err
	}
	result := map[string]model.BillingUsageInfo{}
	for _, ch := range channels {
		b, found := productionBindingFor(state, ch.Id)
		if !found {
			continue
		}
		for name := range strings.SplitSeq(ch.Models, ",") {
			if name == "" {
				continue
			}
			info := result[name]
			if info.Plugins == nil {
				info.Plugins = map[string]model.BillingUsageInfo{}
			}
			profile := model.BillingUsageInfo{Status: "unavailable", UnresolvedTargets: true}
			if failures[ch.Id] == "" && model.TaskPluginKeyForChannel(ch.Type) == b.Key {
				tail, declared, err := declaredPricingTarget(handles[ch.Id], name, ch.ModelMapping)
				if err == nil && !declared {
					continue
				}
				if err == nil {
					schema, examples := handles[ch.Id].Meta().UsageForModel(tail)
					profile = model.BillingUsageInfo{Status: "ready", Schema: jsplugin.CloneUsageSchema(schema), Examples: jsplugin.CloneUsageExamples(examples), Sources: []model.BillingUsageSource{{Plugin: b.Key, Version: b.Version, SourceHash: b.SourceHash, Model: tail}}}
				}
			}
			old, exists := info.Plugins[b.Key]
			info.Plugins[b.Key] = mergeBillingUsage(old, profile, exists)
			result[name] = info
		}
	}
	return result, nil
}

// Fixed source identity, not the active registry or channel type, proves a task
// target. This resolver is also usable while a channel or plugin is paused.
func (m *Manager) fixedPricingHandles(ctx context.Context, state model.TaskPluginSnapshot, sources []model.TaskPluginSourceSnapshot) (map[int]*host.Handle, map[int]string, error) {
	handles, failures := map[int]*host.Handle{}, map[int]string{}
	for _, b := range state.Production {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		source := m.factories[b.Key]
		if !b.Factory {
			source = ""
			for _, p := range state.Plugins {
				if p.Key == b.Key && p.Version == b.Version {
					source = p.Source
					break
				}
			}
		}
		if source == "" || sourceHash(source) != b.SourceHash {
			for _, saved := range sources {
				if saved.Key == b.Key && saved.Version == b.Version && saved.SourceHash == b.SourceHash {
					source = saved.Source
					break
				}
			}
		}
		if source == "" || sourceHash(source) != b.SourceHash {
			failures[b.ChannelID] = "fixed_source_missing_or_changed"
			continue
		}
		h, err := compileHandle(source)
		if err != nil || h.Identity() != (host.Identity{Key: b.Key, Version: b.Version, SourceHash: b.SourceHash}) {
			failures[b.ChannelID] = "fixed_source_invalid"
			continue
		}
		handles[b.ChannelID] = h
	}
	return handles, failures, nil
}

func declaredPricingTarget(h *host.Handle, name string, mapping *string) (string, bool, error) {
	return declaredTarget(h.Meta().Models, name, mapping)
}

func declaredTarget(models []string, name string, mapping *string) (string, bool, error) {
	tail, ok := mappedUsageModel(name, mapping)
	if !ok {
		return "", false, fmt.Errorf("invalid_model_mapping")
	}
	canonical, found := boundModelName(models, tail)
	return canonical, found, nil
}

// Config validation uses exactly the same explicit-rule selection as requests.
// Paused bindings can be staged, priced, then enabled; enabling is the gate.
func (m *Manager) ValidatePricingTargets(ctx context.Context, document model.ModelPricingConfig, channels []model.Channel, state model.TaskPluginSnapshot, sources []model.TaskPluginSourceSnapshot, selfUse bool) error {
	if document.TaskPriorityVersion != 1 {
		return nil
	}
	// Compile only the bindings of the enabled channels being checked; a single
	// channel write must not recompile every production plugin.
	checked := map[int]bool{}
	for _, ch := range channels {
		checked[ch.Id] = ch.Status == common.ChannelStatusEnabled
	}
	scoped := state
	scoped.Production = nil
	for _, b := range state.Production {
		if checked[b.ChannelID] {
			scoped.Production = append(scoped.Production, b)
		}
	}
	handles, failures, err := m.fixedPricingHandles(ctx, scoped, sources)
	if err != nil {
		return err
	}
	for _, ch := range channels {
		b, bound := productionBindingFor(state, ch.Id)
		if !bound || ch.Status != common.ChannelStatusEnabled || productionEnabled(state, b) != nil {
			continue
		}
		if model.TaskPluginKeyForChannel(ch.Type) != b.Key {
			return fmt.Errorf("binding_channel_type_mismatch: %d", ch.Id)
		}
		if reason := failures[ch.Id]; reason != "" {
			return fmt.Errorf("%s: %d", reason, ch.Id)
		}
		for name := range strings.SplitSeq(ch.Models, ",") {
			if name == "" {
				continue
			}
			tail, found, err := declaredPricingTarget(handles[ch.Id], name, ch.ModelMapping)
			if err != nil {
				return err
			}
			if !found {
				continue
			} // Mixed channels retain ordinary models.
			rule, err := model.SelectConfiguredTaskRule(document, b.Key, name, tail, selfUse)
			if err != nil {
				return err
			}
			schema, _ := handles[ch.Id].Meta().UsageForModel(tail)
			if err := rule.Validate(schema); err != nil {
				return fmt.Errorf("channel %d: %w", ch.Id, err)
			}
		}
	}
	return nil
}
