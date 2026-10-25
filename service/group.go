package service

import (
	"strings"

	"eflow.local/new-api/setting"
	"eflow.local/new-api/setting/ratio_setting"
)

func GetUserUsableGroups(userGroup string) map[string]string {
	groupsCopy := setting.GetUserUsableGroupsCopy()
	if userGroup != "" {
		specialSettings, b := ratio_setting.GetGroupRatioSetting().GroupSpecialUsableGroup.Get(userGroup)
		if b {
			// 处理特殊可用分组
			for specialGroup, desc := range specialSettings {
				if after, ok := strings.CutPrefix(specialGroup, "-:"); ok {
					// 移除分组
					groupToRemove := after
					delete(groupsCopy, groupToRemove)
				} else if after, ok := strings.CutPrefix(specialGroup, "+:"); ok {
					// 添加分组
					groupToAdd := after
					groupsCopy[groupToAdd] = desc
				} else {
					// 直接添加分组
					groupsCopy[specialGroup] = desc
				}
			}
		}
	}
	return groupsCopy
}

// GetUserEffectiveGroups merges the token-group permissions inherited from a
// user's primary and administrator-assigned additional customer tiers.
func GetUserEffectiveGroups(primaryTier string, additionalTiers []string) map[string]string {
	groups := make(map[string]string)
	for _, tier := range append([]string{primaryTier}, additionalTiers...) {
		for group, desc := range GetUserUsableGroups(strings.TrimSpace(tier)) {
			if group == "auto" || ratio_setting.ContainsGroupRatio(group) {
				groups[group] = desc
			}
		}
	}
	return groups
}

func GroupInUserEffectiveGroups(userGroup string, additionalTiers []string, groupName string) bool {
	_, ok := GetUserEffectiveGroups(userGroup, additionalTiers)[groupName]
	return ok
}

// GetUserAutoGroup 根据用户分组获取自动分组设置
func GetUserAutoGroup(userGroup string, additionalTiers []string) []string {
	return GetUserAutoGroupWithPolicy(userGroup, additionalTiers, setting.GetAutoGroupPolicy())
}

func GetUserAutoGroupWithPolicy(userGroup string, additionalTiers []string, policy setting.AutoGroupPolicy) []string {
	return getUserAutoGroupFromConfiguredGroups(userGroup, additionalTiers, policy.Groups)
}

func getUserAutoGroupFromConfiguredGroups(userGroup string, additionalTiers, configuredGroups []string) []string {
	groups := GetUserEffectiveGroups(userGroup, additionalTiers)
	autoGroups := make([]string, 0)
	for _, group := range configuredGroups {
		if _, ok := groups[group]; ok {
			autoGroups = append(autoGroups, group)
		}
	}
	return autoGroups
}

// GetUserGroupRatio returns the lowest configured override across the user's
// primary and additional customer tiers, or the token group's base ratio.
func GetUserGroupRatio(primaryTier string, additionalTiers []string, group string) (float64, bool) {
	var best float64
	found := false
	for _, tier := range append([]string{primaryTier}, additionalTiers...) {
		if ratio, ok := ratio_setting.GetGroupGroupRatio(strings.TrimSpace(tier), group); ok && (!found || ratio < best) {
			best = ratio
			found = true
		}
	}
	if found {
		return best, true
	}
	return ratio_setting.GetGroupRatio(group), false
}
