package controller

import (
	"maps"
	"strings"

	"eflow.local/new-api/common"
	"eflow.local/new-api/model"
	"eflow.local/new-api/service"
	"eflow.local/new-api/setting/operation_setting"
	"eflow.local/new-api/setting/ratio_setting"

	"github.com/gin-gonic/gin"
)

func filterPricingByUsableGroups(pricing []model.Pricing, usableGroup map[string]string) []model.Pricing {
	if len(pricing) == 0 {
		return pricing
	}
	if len(usableGroup) == 0 {
		return []model.Pricing{}
	}

	filtered := make([]model.Pricing, 0, len(pricing))
	for _, item := range pricing {
		if common.StringsContains(item.EnableGroup, "all") {
			filtered = append(filtered, item)
			continue
		}
		// eflow: 不仅决定模型是否展示，还要把 EnableGroup 裁剪成用户实际可用
		// 分组的交集，避免隐藏/无权限分组名经由 enable_groups 泄露到模型广场
		// 卡片（前端 model-card 直接读该字段；分组下拉/详情用 usable_group 取
		// 交集本身安全，此处对齐三者）。item 是 range 拷贝，重新赋值 slice 头不
		// 会污染 GetPricing 的共享缓存。
		visibleGroups := make([]string, 0, len(item.EnableGroup))
		for _, group := range item.EnableGroup {
			if _, ok := usableGroup[group]; ok {
				visibleGroups = append(visibleGroups, group)
			}
		}
		if len(visibleGroups) == 0 {
			continue
		}
		item.EnableGroup = visibleGroups
		filtered = append(filtered, item)
	}
	return filtered
}

func filterPricingGroupEndpointOverrides(overrides map[string]model.PricingGroupEndpointOverride, usableGroup map[string]string) map[string]model.PricingGroupEndpointOverride {
	filtered := make(map[string]model.PricingGroupEndpointOverride)
	for group, override := range overrides {
		if group == "default" {
			filtered[group] = override
		} else if _, ok := usableGroup[group]; ok {
			filtered[group] = override
		}
	}
	return filtered
}

func GetPricing(c *gin.Context) {
	pricing := model.GetPricing()
	userId, exists := c.Get("id")
	usableGroup := map[string]string{}
	groupRatio := map[string]float64{}
	maps.Copy(groupRatio, ratio_setting.GetGroupRatioCopy())
	var group string
	var additionalTiers []string
	isRoot := false
	if exists {
		user, err := model.GetUserCache(userId.(int))
		if err == nil {
			group = user.Group
			additionalTiers = user.AdditionalCustomerTiers.Values()
			isRoot = user.Role >= common.RoleRootUser
			for g := range groupRatio {
				groupRatio[g], _ = service.GetUserGroupRatio(group, additionalTiers, g)
			}
		}
	}

	usableGroup = service.GetUserEffectiveGroups(group, additionalTiers)
	region := strings.ToUpper(strings.TrimSpace(c.GetHeader("CF-IPCountry")))
	if _, matched := operation_setting.GetWebGroupSetting().GetRegionRule(region); matched {
		for groupName := range usableGroup {
			if !operation_setting.GetWebGroupSetting().IsGroupAvailable(region, groupName) {
				delete(usableGroup, groupName)
			}
		}
	}
	pricing = filterPricingByUsableGroups(pricing, usableGroup)
	// Plugin source identity and task rules are Root editor data; the shared
	// cache holds the full view, so strip per request for everyone else.
	if !isRoot {
		for i := range pricing {
			pricing[i] = model.PublicPricing(pricing[i])
		}
	}
	configuredEndpointOverrides := model.GetPricingGroupEndpointOverrides()
	groupEndpointOverrides := filterPricingGroupEndpointOverrides(
		configuredEndpointOverrides,
		usableGroup,
	)
	// check groupRatio contains usableGroup
	for group := range ratio_setting.GetGroupRatioCopy() {
		if _, ok := usableGroup[group]; !ok {
			delete(groupRatio, group)
		}
	}

	c.JSON(200, gin.H{
		"success":                  true,
		"data":                     pricing,
		"vendors":                  model.GetVendors(),
		"group_ratio":              groupRatio,
		"usable_group":             usableGroup,
		"supported_endpoint":       model.GetSupportedEndpointMap(),
		"group_endpoint_overrides": groupEndpointOverrides,
		"auto_groups":              service.GetUserAutoGroup(group, additionalTiers),
		"pricing_version":          "a42d372ccf0b5dd13ecf71203521f9d2",
	})
}

func ResetModelRatio(c *gin.Context) {
	defaultStr := ratio_setting.DefaultModelRatio2JSONString()
	err := model.UpdateOption("ModelRatio", defaultStr)
	if err != nil {
		c.JSON(200, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}
	err = ratio_setting.UpdateModelRatioByJSONString(defaultStr)
	if err != nil {
		c.JSON(200, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}
	c.JSON(200, gin.H{
		"success": true,
		"message": "重置模型倍率成功",
	})
}
