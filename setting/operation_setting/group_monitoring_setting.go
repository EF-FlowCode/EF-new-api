package operation_setting

import (
	"slices"
	"strings"

	"eflow.local/new-api/setting/config"
)

type GroupMonitoringSetting struct {
	MonitoringGroups               []string `json:"monitoring_groups"`
	AvailabilityPeriodMinutes      int      `json:"availability_period_minutes"`
	CacheHitPeriodMinutes          int      `json:"cache_hit_period_minutes"`
	AvailabilityExcludeModels      []string `json:"availability_exclude_models"`
	CacheHitExcludeModels          []string `json:"cache_hit_exclude_models"`
	AvailabilityExcludeKeywords    []string `json:"availability_exclude_keywords"`
	AvailabilityExcludeStatusCodes []int    `json:"availability_exclude_status_codes"`
	GroupDisplayOrder              []string `json:"group_display_order"`
	AggregationIntervalMinutes     int      `json:"aggregation_interval_minutes"`
	CacheTokensSeparateGroups      []string `json:"cache_tokens_separate_groups"`
	// eflow: 分组分类管理。GroupCategories 是 groupName → categoryName 的映射（未
	// 出现的 group 视为"未分类"）；CategoryOrder 决定分类块在前端的渲染顺序，
	// 不在列表里的分类按字母顺序回退、"未分类"始终最后。
	GroupCategories map[string]string `json:"group_categories"`
	CategoryOrder   []string          `json:"category_order"`
}

var groupMonitoringSetting = GroupMonitoringSetting{
	MonitoringGroups:               []string{},
	AvailabilityPeriodMinutes:      60,
	CacheHitPeriodMinutes:          60,
	AvailabilityExcludeModels:      []string{},
	CacheHitExcludeModels:          []string{},
	AvailabilityExcludeKeywords:    []string{},
	AvailabilityExcludeStatusCodes: []int{},
	GroupDisplayOrder:              []string{},
	AggregationIntervalMinutes:     5,
	CacheTokensSeparateGroups:      []string{},
	GroupCategories:                map[string]string{},
	CategoryOrder:                  []string{},
}

func init() {
	config.GlobalConfig.Register("group_monitoring_setting", &groupMonitoringSetting)
}

func GetGroupMonitoringSetting() GroupMonitoringSetting {
	return config.MustSnapshot[GroupMonitoringSetting]("group_monitoring_setting")
}

// ResolveMonitoringGroups 把 MonitoringGroups 里的规则展开成实际监控的分组列表（已排序去重）。
// eflow: 规则三种写法——`name` 精确匹配（即使 knownGroups 里没有也保留，兼容旧配置）；
// `prefix*` 前缀匹配 knownGroups；`!name` / `!prefix*` 从结果里排除。auto 不参与通配。
func (s GroupMonitoringSetting) ResolveMonitoringGroups(knownGroups []string) []string {
	var includes, excludes []string
	for _, rule := range s.MonitoringGroups {
		rule = strings.TrimSpace(rule)
		if negated, ok := strings.CutPrefix(rule, "!"); ok {
			if negated = strings.TrimSpace(negated); negated != "" {
				excludes = append(excludes, negated)
			}
		} else if rule != "" {
			includes = append(includes, rule)
		}
	}

	// explicit=true 表示被精确点名；精确点名只会被精确的 !name 排除，不受 !prefix* 影响，
	// 这样 "1_*, !1_vip_*, 1_vip_codex" 能把单个分组加回来。
	explicit := make(map[string]bool)
	for _, rule := range includes {
		if !strings.HasSuffix(rule, "*") {
			explicit[rule] = true
			continue
		}
		for _, group := range knownGroups {
			if group != "auto" && matchMonitoringRule(rule, group) && !explicit[group] {
				explicit[group] = false
			}
		}
	}

	result := make([]string, 0, len(explicit))
	for group, named := range explicit {
		excluded := slices.ContainsFunc(excludes, func(rule string) bool {
			if named {
				return rule == group
			}
			return matchMonitoringRule(rule, group)
		})
		if !excluded {
			result = append(result, group)
		}
	}
	slices.Sort(result)
	return result
}

func matchMonitoringRule(rule, group string) bool {
	if prefix, ok := strings.CutSuffix(rule, "*"); ok {
		return strings.HasPrefix(group, prefix)
	}
	return rule == group
}

// IsAvailabilityExcluded reports whether a request must be omitted from
// availability statistics. Model, status-code, and keyword rules use OR
// semantics; keyword matching is case-insensitive.
func (s GroupMonitoringSetting) IsAvailabilityExcluded(statusCode int, modelName, content string) bool {
	if modelName != "" {
		if slices.Contains(s.AvailabilityExcludeModels, modelName) {
			return true
		}
	}
	if statusCode > 0 {
		if slices.Contains(s.AvailabilityExcludeStatusCodes, statusCode) {
			return true
		}
	}
	if content != "" && len(s.AvailabilityExcludeKeywords) > 0 {
		lowerContent := strings.ToLower(content)
		for _, keyword := range s.AvailabilityExcludeKeywords {
			if strings.Contains(lowerContent, strings.ToLower(keyword)) {
				return true
			}
		}
	}
	return false
}
