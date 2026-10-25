package controller

import (
	"net/http"
	"strconv"
	"strings"

	"eflow.local/new-api/model"
	perfmetrics "eflow.local/new-api/pkg/perf_metrics"
	"eflow.local/new-api/service"
	"eflow.local/new-api/setting/operation_setting"
	"eflow.local/new-api/setting/ratio_setting"

	"github.com/gin-gonic/gin"
	"github.com/samber/lo"
)

func GetPerfMetricsSummary(c *gin.Context) {
	hours := 24
	if rawHours := c.Query("hours"); rawHours != "" {
		if parsed, err := strconv.Atoi(rawHours); err == nil {
			hours = parsed
		}
	}

	activeGroups := append(lo.Keys(ratio_setting.GetGroupRatioCopy()), "auto")
	result, err := perfmetrics.QuerySummaryAll(hours, activeGroups)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    result,
	})
}

func GetPerfMetrics(c *gin.Context) {
	modelName := c.Query("model")
	if modelName == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "model is required",
		})
		return
	}

	hours := 24
	if rawHours := c.Query("hours"); rawHours != "" {
		if parsed, err := strconv.Atoi(rawHours); err == nil {
			hours = parsed
		}
	}

	result, err := perfmetrics.Query(perfmetrics.QueryParams{
		Model: modelName,
		Group: c.Query("group"),
		Hours: hours,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}

	result.Groups = filterPerfGroupsForUser(c, result.Groups)

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    result,
	})
}

// filterPerfGroupsForUser keeps performance data aligned with the groups the
// requesting user can actually use. Global ratio configuration alone is not an
// authorization check and would expose private groups such as work_china.
func filterPerfGroupsForUser(c *gin.Context, groups []perfmetrics.GroupResult) []perfmetrics.GroupResult {
	userGroup := ""
	var additionalTiers []string
	if rawID, ok := c.Get("id"); ok {
		if user, err := model.GetUserCache(rawID.(int)); err == nil {
			userGroup = user.Group
			additionalTiers = user.AdditionalCustomerTiers.Values()
		}
	}
	usable := service.GetUserEffectiveGroups(userGroup, additionalTiers)
	region := strings.ToUpper(strings.TrimSpace(c.GetHeader("CF-IPCountry")))
	if _, matched := operation_setting.GetWebGroupSetting().GetRegionRule(region); matched {
		for group := range usable {
			if !operation_setting.GetWebGroupSetting().IsGroupAvailable(region, group) {
				delete(usable, group)
			}
		}
	}
	return lo.Filter(groups, func(g perfmetrics.GroupResult, _ int) bool {
		if g.Group == "auto" {
			return true
		}
		_, ok := usable[g.Group]
		return ok
	})
}
