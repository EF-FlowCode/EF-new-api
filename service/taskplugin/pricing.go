package taskplugin

import (
	"context"
	"encoding/json"
	"reflect"
	"slices"
	"sort"
	"strings"

	"eflow.local/new-api/model"
	"eflow.local/new-api/pkg/jsplugin"
)

// BillingUsageInfos resolves all enabled channels from one control snapshot.
// Any eligible channel without a compatible fixed binding makes u() unsafe for
// that public model: ordinary token routing must not receive a task-only rule.
func (m *Manager) BillingUsageInfos(ctx context.Context, channels []model.Channel) (map[string]model.BillingUsageInfo, error) {
	state, err := m.read(ctx)
	if err != nil {
		return nil, err
	}
	view := m.view.Load()
	result := map[string]model.BillingUsageInfo{}
	taskModels := map[string]bool{}
	byPlugin := map[string]map[string]model.BillingUsageInfo{}
	for _, channel := range channels {
		for name := range strings.SplitSeq(channel.Models, ",") {
			if name == "" {
				continue
			}
			next := model.BillingUsageInfo{Status: "unavailable", UnresolvedTargets: true}
			binding, found := productionBindingFor(state, channel.Id)
			// OpenAI/Ali/Gemini channel types also serve ordinary text models.
			// A channel type alone is not evidence of a production task binding.
			if found {
				taskModels[name] = true
			}
			if found && view != nil && model.TaskPluginKeyForChannel(channel.Type) == binding.Key {
				cached, ok := view.production[channel.Id]
				if ok && cached.binding == binding {
					meta := cached.handle.Meta()
					if canonical, declared, err := declaredPricingTarget(cached.handle, name, channel.ModelMapping); err == nil {
						// A resolved tail outside this fixed plugin cannot use its
						// task rules (the channel may also expose text models).
						next.UnresolvedTargets = false
						if declared {
							schema, examples := meta.UsageForModel(canonical)
							next.Sources = []model.BillingUsageSource{{Plugin: binding.Key, Version: binding.Version, SourceHash: binding.SourceHash, Model: canonical}}
							if productionEnabled(state, binding) == nil {
								next.Status, next.Schema, next.Examples = "ready", jsplugin.CloneUsageSchema(schema), jsplugin.CloneUsageExamples(examples)
							}
						}
					}
				}
			}
			if found && (next.UnresolvedTargets || len(next.Sources) > 0) {
				if byPlugin[name] == nil {
					byPlugin[name] = map[string]model.BillingUsageInfo{}
				}
				old, exists := byPlugin[name][binding.Key]
				byPlugin[name][binding.Key] = mergeBillingUsage(old, next, exists)
			}
			old, exists := result[name]
			result[name] = mergeBillingUsage(old, next, exists)
		}
	}
	for name, info := range result {
		if !taskModels[name] {
			delete(result, name)
			continue
		}
		sort.Slice(info.Sources, func(i, j int) bool {
			a, b := info.Sources[i], info.Sources[j]
			return a.Plugin+"/"+a.Version+"/"+a.SourceHash+"/"+a.Model < b.Plugin+"/"+b.Version+"/"+b.SourceHash+"/"+b.Model
		})
		info.Plugins = byPlugin[name]
		for plugin, profile := range info.Plugins {
			sort.Slice(profile.Sources, func(i, j int) bool {
				return profile.Sources[i].Model+profile.Sources[i].SourceHash < profile.Sources[j].Model+profile.Sources[j].SourceHash
			})
			info.Plugins[plugin] = profile
		}
		result[name] = info
	}
	return result, nil
}

// Match ModelMappedHelper: exact map keys, chained aliases, self-map/empty tail
// as a terminal, and refusal of actual cycles. Folding happens only afterwards.
func mappedUsageModel(name string, raw *string) (string, bool) {
	if raw == nil || *raw == "" {
		return name, true
	}
	var mapping map[string]string
	if json.Unmarshal([]byte(*raw), &mapping) != nil {
		return "", false
	}
	seen := map[string]bool{name: true}
	for {
		next := mapping[name]
		if next == "" || next == name {
			return name, true
		}
		if seen[next] {
			return "", false
		}
		seen[next] = true
		name = next
	}
}

func mergeBillingUsage(old, next model.BillingUsageInfo, exists bool) model.BillingUsageInfo {
	if !exists {
		return next
	}
	if old.Status != "ready" || next.Status != "ready" || !reflect.DeepEqual(old.Schema, next.Schema) || old.Sources[0].Plugin != next.Sources[0].Plugin {
		// Keep all known targets for save-time mapped-rule validation. Conflict
		// removes the usable schema, not evidence that a rule affects a channel.
		for _, source := range next.Sources {
			if !slices.Contains(old.Sources, source) {
				old.Sources = append(old.Sources, source)
			}
		}
		return model.BillingUsageInfo{Status: "conflict", Sources: old.Sources, UnresolvedTargets: old.UnresolvedTargets || next.UnresolvedTargets}
	}
	if !slices.Contains(old.Sources, next.Sources[0]) {
		old.Sources = append(old.Sources, next.Sources[0])
	}
	if !reflect.DeepEqual(old.Examples, next.Examples) {
		old.Examples = nil
	}
	return old
}
