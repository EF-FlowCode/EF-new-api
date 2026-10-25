package setting

import (
	"fmt"
	"slices"
	"strconv"
	"sync"
	"sync/atomic"

	"eflow.local/new-api/common"
)

// AutoGroupPolicy is one immutable runtime version of automatic group routing.
type AutoGroupPolicy struct {
	Groups              []string
	DefaultUseAutoGroup bool
}

var (
	autoGroupPolicy   atomic.Pointer[AutoGroupPolicy]
	autoGroupPolicyMu sync.Mutex
)

func init() {
	publishAutoGroupPolicy(AutoGroupPolicy{Groups: []string{"default"}})
}

// GetAutoGroupPolicy returns an isolated copy of one published version.
func GetAutoGroupPolicy() AutoGroupPolicy {
	current := autoGroupPolicy.Load()
	if current == nil {
		return AutoGroupPolicy{}
	}
	return cloneAutoGroupPolicy(*current)
}

func IsAutoGroupOptionKey(key string) bool {
	return key == "AutoGroups" || key == "DefaultUseAutoGroup"
}

// UpdateAutoGroupPolicy validates all supplied fields before publishing them
// together. Unknown keys are ignored.
func UpdateAutoGroupPolicy(values map[string]string) error {
	autoGroupPolicyMu.Lock()
	defer autoGroupPolicyMu.Unlock()

	next := GetAutoGroupPolicy()
	touched := false
	if value, ok := values["AutoGroups"]; ok {
		var groups []string
		if err := common.Unmarshal([]byte(value), &groups); err != nil {
			return err
		}
		next.Groups = groups
		touched = true
	}
	if value, ok := values["DefaultUseAutoGroup"]; ok {
		parsed, err := strconv.ParseBool(value)
		if err != nil {
			return fmt.Errorf("DefaultUseAutoGroup must be a boolean: %w", err)
		}
		next.DefaultUseAutoGroup = parsed
		touched = true
	}
	if touched {
		publishAutoGroupPolicy(next)
	}
	return nil
}

func ContainsAutoGroup(group string) bool {
	policy := GetAutoGroupPolicy()
	return slices.Contains(policy.Groups, group)
}

func UpdateAutoGroupsByJsonString(jsonString string) error {
	return UpdateAutoGroupPolicy(map[string]string{"AutoGroups": jsonString})
}

func AutoGroups2JsonString() string {
	jsonBytes, err := common.Marshal(GetAutoGroupPolicy().Groups)
	if err != nil {
		return "[]"
	}
	return string(jsonBytes)
}

func GetAutoGroups() []string {
	return GetAutoGroupPolicy().Groups
}

func cloneAutoGroupPolicy(policy AutoGroupPolicy) AutoGroupPolicy {
	policy.Groups = append([]string(nil), policy.Groups...)
	return policy
}

func publishAutoGroupPolicy(next AutoGroupPolicy) {
	owned := cloneAutoGroupPolicy(next)
	autoGroupPolicy.Store(&owned)
}
