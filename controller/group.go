package controller

import (
	"net/http"
	"strings"

	"eflow.local/new-api/common"
	"eflow.local/new-api/model"
	"eflow.local/new-api/service"
	"eflow.local/new-api/setting"
	"eflow.local/new-api/setting/operation_setting"
	"eflow.local/new-api/setting/ratio_setting"

	"github.com/gin-gonic/gin"
)

func GetGroups(c *gin.Context) {
	groupNames := make([]string, 0)
	for groupName := range ratio_setting.GetGroupRatioCopy() {
		groupNames = append(groupNames, groupName)
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    groupNames,
	})
}

func GetUserGroups(c *gin.Context) {
	usableGroups := make(map[string]map[string]any)
	userId := c.GetInt("id")
	userGroup := ""
	var additionalTiers []string
	if userId > 0 {
		user, err := model.GetUserCache(userId)
		if err != nil {
			common.ApiError(c, err)
			return
		}
		userGroup = user.Group
		additionalTiers = user.AdditionalCustomerTiers.Values()
	}
	userUsableGroups := service.GetUserEffectiveGroups(userGroup, additionalTiers)
	region := strings.ToUpper(strings.TrimSpace(c.GetHeader("CF-IPCountry")))
	webGroupSetting := operation_setting.GetWebGroupSetting()
	_, regionRestricted := webGroupSetting.GetRegionRule(region)
	for groupName, _ := range ratio_setting.GetGroupRatioCopy() {
		// UserUsableGroups contains the groups that the user can use
		if desc, ok := userUsableGroups[groupName]; ok {
			ratio, _ := service.GetUserGroupRatio(userGroup, additionalTiers, groupName)
			groupData := map[string]any{
				"ratio": ratio,
				"desc":  desc,
			}
			if regionRestricted {
				groupData["available"] = webGroupSetting.IsGroupAvailable(region, groupName)
			}
			usableGroups[groupName] = groupData
		}
	}
	if _, ok := userUsableGroups["auto"]; ok {
		groupData := map[string]any{
			"ratio": "自动",
			"desc":  setting.GetUsableGroupDescription("auto"),
		}
		if regionRestricted {
			groupData["available"] = webGroupSetting.IsGroupAvailable(region, "auto")
		}
		usableGroups["auto"] = groupData
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    usableGroups,
	})
}

// GetWebGroupRegionAccess exposes presentation-only region information for
// the public web shell. It does not enforce anything on relay/API requests.
func GetWebGroupRegionAccess(c *gin.Context) {
	region := strings.ToUpper(strings.TrimSpace(c.GetHeader("CF-IPCountry")))
	rule, matched := operation_setting.GetWebGroupSetting().GetRegionRule(region)
	data := gin.H{
		"region":     region,
		"restricted": false,
	}
	if !matched {
		c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": data})
		return
	}

	allGroups := ratio_setting.GetGroupRatioCopy()
	allowed := make(map[string]struct{}, len(rule.AllowedGroups))
	for _, group := range rule.AllowedGroups {
		allowed[strings.TrimSpace(group)] = struct{}{}
	}
	allowedCount := 0
	for group := range allGroups {
		if _, ok := allowed[group]; ok {
			allowedCount++
		}
	}
	restricted := allowedCount < len(allGroups)
	data["restricted"] = restricted
	if restricted && rule.PopupEnabled {
		data["popup"] = gin.H{
			"display_name": rule.DisplayName,
			"title":        rule.PopupTitle,
			"content":      rule.PopupContent,
			"notice":       rule.PopupNotice,
			"button":       rule.PopupButton,
			"version":      rule.Version,
		}
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": data})
}
