package controller

import (
	"eflow.local/new-api/common"
	"eflow.local/new-api/model"
	"github.com/gin-gonic/gin"
)

func GetSelfUsageSummary(c *gin.Context) {
	data, err := model.GetUserUsageSummary(c.Request.Context(), c.GetInt("id"))
	if err != nil {
		common.SysError("user usage summary: " + err.Error())
		common.ApiErrorMsg(c, "Failed to load account summary")
		return
	}
	common.ApiSuccess(c, data)
}

func GetSelfRefundRecords(c *gin.Context) {
	page := common.GetPageQuery(c)
	data, err := model.GetUserRefundRecords(c.Request.Context(), c.GetInt("id"), c.DefaultQuery("kind", "usage"), page.Page, page.PageSize)
	if err != nil {
		common.SysError("user refund records: " + err.Error())
		common.ApiErrorMsg(c, "Failed to load refund records")
		return
	}
	common.ApiSuccess(c, data)
}
