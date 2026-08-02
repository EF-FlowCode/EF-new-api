package operation_setting

import (
	"github.com/QuantumNous/new-api/setting/config"
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
	return groupMonitoringSetting
}
