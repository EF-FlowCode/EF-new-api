package controller

import (
	"net/http"
	"sort"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"

	"github.com/gin-gonic/gin"
)

func GetAdminMonitoringGroups(c *gin.Context) {
	setting := operation_setting.GetGroupMonitoringSetting()
	monitoringGroups := setting.MonitoringGroups

	// eflow: 自动列出系统里所有分组（来自 group_ratio 配置）+ union 当前
	// MonitoringGroups。让管理员能在 UI 里直接勾选/取消勾选哪些分组进监控列表，
	// 而不是要去 system-settings JSON 编辑器手撸。
	allGroupsSet := make(map[string]struct{})
	for name := range ratio_setting.GetGroupRatioCopy() {
		allGroupsSet[name] = struct{}{}
	}
	for _, name := range monitoringGroups {
		allGroupsSet[name] = struct{}{}
	}
	allGroups := make([]string, 0, len(allGroupsSet))
	for name := range allGroupsSet {
		allGroups = append(allGroups, name)
	}
	sort.Strings(allGroups)

	var stats []model.GroupMonitoringStat
	if len(monitoringGroups) > 0 {
		var err error
		stats, err = model.GetGroupMonitoringStatsByNames(monitoringGroups)
		if err != nil {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": "获取监控数据失败: " + err.Error(),
			})
			return
		}
	}

	orderedStats := orderGroupStats(stats, setting.GroupDisplayOrder)

	categories := setting.GroupCategories
	if categories == nil {
		categories = map[string]string{}
	}
	categoryOrder := setting.CategoryOrder
	if categoryOrder == nil {
		categoryOrder = []string{}
	}

	c.JSON(http.StatusOK, gin.H{
		"success":          true,
		"message":          "",
		"data":             orderedStats,
		"all_groups":       allGroups,
		"monitored_groups": monitoringGroups,
		"categories":       categories,
		"category_order":   categoryOrder,
	})
}

func GetAdminMonitoringGroupDetail(c *gin.Context) {
	groupName := c.Param("group")
	if groupName == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "分组名称不能为空",
		})
		return
	}

	groupStat, err := model.GetGroupMonitoringStatByName(groupName)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "分组不存在或无监控数据",
		})
		return
	}

	channelStats, err := model.GetChannelMonitoringStatsByGroup(groupName)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "获取渠道监控数据失败: " + err.Error(),
		})
		return
	}

	activeChannels, err := model.GetAllChannelsByGroup(groupName)
	if err == nil {
		activeSet := make(map[int]bool, len(activeChannels))
		channelNameMap := make(map[int]string, len(activeChannels))
		channelStatusMap := make(map[int]int, len(activeChannels))
		for _, ch := range activeChannels {
			activeSet[ch.Id] = true
			channelNameMap[ch.Id] = ch.Name
			channelStatusMap[ch.Id] = ch.Status
		}
		filtered := make([]model.ChannelMonitoringStat, 0, len(channelStats))
		seenChannels := make(map[int]bool, len(channelStats))
		for _, cs := range channelStats {
			if activeSet[cs.ChannelId] {
				cs.ChannelName = channelNameMap[cs.ChannelId]
				cs.ChannelStatus = channelStatusMap[cs.ChannelId]
				filtered = append(filtered, cs)
				seenChannels[cs.ChannelId] = true
			}
		}
		for _, ch := range activeChannels {
			if !seenChannels[ch.Id] {
				filtered = append(filtered, model.ChannelMonitoringStat{
					GroupName:        groupName,
					ChannelId:        ch.Id,
					ChannelName:      ch.Name,
					ChannelStatus:    ch.Status,
					AvailabilityRate: -1,
					CacheHitRate:     -1,
				})
			}
		}
		channelStats = filtered
	}

	c.JSON(http.StatusOK, gin.H{
		"success":       true,
		"message":       "",
		"data":          groupStat,
		"channel_stats": channelStats,
	})
}

func GetAdminMonitoringGroupHistory(c *gin.Context) {
	groupName := c.Param("group")
	if groupName == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "分组名称不能为空",
		})
		return
	}

	setting := operation_setting.GetGroupMonitoringSetting()
	endTime := time.Now().Unix()
	startTime := endTime - int64(setting.AvailabilityPeriodMinutes*60)

	history, err := model.GetMonitoringHistory(groupName, startTime, endTime)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "获取历史数据失败: " + err.Error(),
		})
		return
	}

	history = prependSeedRecord(groupName, startTime, history)

	c.JSON(http.StatusOK, gin.H{
		"success":                      true,
		"message":                      "",
		"data":                         history,
		"period_minutes":               setting.AvailabilityPeriodMinutes,
		"aggregation_interval_minutes": setting.AggregationIntervalMinutes,
	})
}

func RefreshMonitoringData(c *gin.Context) {
	ok := service.TriggerAggregationRefresh()
	if !ok {
		c.JSON(http.StatusTooManyRequests, gin.H{
			"success": false,
			"message": "聚合正在运行中，请稍后再试",
		})
		return
	}

	c.JSON(http.StatusAccepted, gin.H{
		"success": true,
		"message": "刷新已触发，数据将在几秒后更新",
	})
}

func DeleteMonitoringGroupRecords(c *gin.Context) {
	groupName := c.Param("group")
	if groupName == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "分组名称不能为空",
		})
		return
	}

	totalDeleted, err := model.DeleteAllMonitoringDataForGroup(groupName)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "清空记录失败: " + err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "清空成功",
		"data": gin.H{
			"deleted_rows": totalDeleted,
		},
	})
}

func GetPublicMonitoringGroups(c *gin.Context) {
	setting := operation_setting.GetGroupMonitoringSetting()

	monitoringGroups := setting.MonitoringGroups
	if len(monitoringGroups) == 0 {
		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"message": "",
			"data":    []interface{}{},
		})
		return
	}

	stats, err := model.GetGroupMonitoringStatsForPublic(monitoringGroups)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "获取监控数据失败",
		})
		return
	}

	desensitized := make([]gin.H, 0, len(stats))
	for _, s := range stats {
		desensitized = append(desensitized, desensitizeGroupStat(&s))
	}

	orderedData := orderDesensitizedStats(desensitized, setting.GroupDisplayOrder)

	// eflow: 公共接口也回传 categories + category_order，让 /status 公共状态
	// 页能跟管理员视图一样按分类折叠。匿名访客看不到管理按钮，但能看到分类
	// 块结构和顺序。
	categories := setting.GroupCategories
	if categories == nil {
		categories = map[string]string{}
	}
	categoryOrder := setting.CategoryOrder
	if categoryOrder == nil {
		categoryOrder = []string{}
	}

	c.JSON(http.StatusOK, gin.H{
		"success":        true,
		"message":        "",
		"data":           orderedData,
		"categories":     categories,
		"category_order": categoryOrder,
	})
}

func GetPublicMonitoringGroupHistory(c *gin.Context) {
	setting := operation_setting.GetGroupMonitoringSetting()

	groupName := c.Param("group")
	if groupName == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "分组名称不能为空",
		})
		return
	}

	monitoringGroups := setting.MonitoringGroups
	found := false
	for _, g := range monitoringGroups {
		if g == groupName {
			found = true
			break
		}
	}
	if !found {
		c.JSON(http.StatusForbidden, gin.H{
			"success": false,
			"message": "该分组不在监控列表中",
		})
		return
	}

	endTime := time.Now().Unix()
	startTime := endTime - int64(setting.AvailabilityPeriodMinutes*60)

	history, err := model.GetMonitoringHistory(groupName, startTime, endTime)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "获取历史数据失败",
		})
		return
	}

	history = prependSeedRecord(groupName, startTime, history)

	c.JSON(http.StatusOK, gin.H{
		"success":                      true,
		"message":                      "",
		"data":                         history,
		"period_minutes":               setting.AvailabilityPeriodMinutes,
		"aggregation_interval_minutes": setting.AggregationIntervalMinutes,
	})
}

func desensitizeGroupStat(stat *model.GroupMonitoringStat) gin.H {
	return gin.H{
		"group_name":        stat.GroupName,
		"availability_rate": stat.AvailabilityRate,
		"cache_hit_rate":    stat.CacheHitRate,
		"avg_response_time": stat.AvgResponseTime,
		"avg_frt":           stat.AvgFRT,
		"is_online":         stat.OnlineChannels > 0,
		"group_ratio":       stat.GroupRatio,
		"last_test_model":   stat.LastTestModel,
		"updated_at":        stat.UpdatedAt,
	}
}

func orderGroupStats(stats []model.GroupMonitoringStat, order []string) []model.GroupMonitoringStat {
	if len(order) == 0 {
		return stats
	}

	statMap := make(map[string]model.GroupMonitoringStat)
	for _, s := range stats {
		statMap[s.GroupName] = s
	}

	ordered := make([]model.GroupMonitoringStat, 0, len(stats))
	for _, name := range order {
		if s, ok := statMap[name]; ok {
			ordered = append(ordered, s)
			delete(statMap, name)
		}
	}

	for _, s := range statMap {
		ordered = append(ordered, s)
	}

	return ordered
}

func orderDesensitizedStats(stats []gin.H, order []string) []gin.H {
	if len(order) == 0 {
		return stats
	}

	statMap := make(map[string]gin.H)
	for _, s := range stats {
		if name, ok := s["group_name"].(string); ok {
			statMap[name] = s
		}
	}

	ordered := make([]gin.H, 0, len(stats))
	for _, name := range order {
		if s, ok := statMap[name]; ok {
			ordered = append(ordered, s)
			delete(statMap, name)
		}
	}

	for _, s := range statMap {
		ordered = append(ordered, s)
	}

	return ordered
}

func prependSeedRecord(groupName string, startTime int64, history []model.MonitoringHistory) []model.MonitoringHistory {
	seed, err := model.GetLastMonitoringHistoryBefore(groupName, startTime)
	if err != nil || seed == nil {
		return history
	}
	seed.RecordedAt = startTime
	return append([]model.MonitoringHistory{*seed}, history...)
}
