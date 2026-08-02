package controller

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
)

func GetEnforcementConfig(c *gin.Context) {
	common.ApiSuccess(c, operation_setting.GetEnforcementSetting())
}

func UpdateEnforcementConfig(c *gin.Context) {
	var req operation_setting.EnforcementSetting
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "无效的参数",
		})
		return
	}
	operation_setting.NormalizeEnforcementSetting(&req)
	configMap, err := config.ConfigToMap(&req)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	for key, value := range configMap {
		if err = model.UpdateOption("enforcement."+key, value); err != nil {
			common.ApiError(c, err)
			return
		}
	}
	common.ApiSuccess(c, operation_setting.GetEnforcementSetting())
}

func GetEnforcementOverview(c *gin.Context) {
	overview, err := service.EnforcementOverview()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, overview)
}

func GetEnforcementIncidents(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)
	q := model.EnforcementIncidentQuery{
		Group:   c.Query("group"),
		Source:  c.Query("source"),
		Action:  c.Query("action"),
		Keyword: c.Query("keyword"),
	}
	if v := strings.TrimSpace(c.Query("user_id")); v != "" {
		if uid, err := strconv.Atoi(v); err == nil {
			q.UserID = uid
		}
	}
	rows, total, err := model.ListEnforcementIncidents(q, pageInfo.GetStartIdx(), pageInfo.GetPageSize())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(rows)
	common.ApiSuccess(c, pageInfo)
}

func GetEnforcementCounters(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)
	rows, total, err := model.ListEnforcementCounters(pageInfo.GetStartIdx(), pageInfo.GetPageSize())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(rows)
	common.ApiSuccess(c, pageInfo)
}

// ResetEnforcementCounter clears a target user's counter without flipping
// their status. Useful when an admin wants to forgive a borderline user
// after manual review.
func ResetEnforcementCounter(c *gin.Context) {
	uid, err := strconv.Atoi(c.Param("id"))
	if err != nil || uid <= 0 {
		common.ApiErrorMsg(c, "无效的用户 ID")
		return
	}
	if err := service.ManualResetEnforcementCounter(c.GetInt("id"), uid); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"id": uid})
}

// UnbanEnforcementUser re-enables a user account auto-banned by the engine
// AND zeros their counters per decision point 6. The handler is admin-only
// (registered behind AdminAuth) so a self-unban path is impossible.
func UnbanEnforcementUser(c *gin.Context) {
	uid, err := strconv.Atoi(c.Param("id"))
	if err != nil || uid <= 0 {
		common.ApiErrorMsg(c, "无效的用户 ID")
		return
	}
	if err := service.ManualUnbanEnforcement(c.GetInt("id"), uid); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"id": uid})
}

// SendEnforcementTestEmail dispatches a sample notification to the calling
// admin's mailbox. We deliberately reject "send to anyone" forms so this
// endpoint cannot be repurposed as a general-purpose email relay.
func SendEnforcementTestEmail(c *gin.Context) {
	if err := service.SendEnforcementTestEmail(c.GetInt("id")); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"sent": true})
}
