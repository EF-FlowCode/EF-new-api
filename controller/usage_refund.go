package controller

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"eflow.local/new-api/common"
	"eflow.local/new-api/model"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func usageRefundResponse(c *gin.Context, data any, err error, failureMessage string) {
	if err == nil {
		common.ApiSuccess(c, data)
		return
	}
	status := http.StatusInternalServerError
	switch {
	case errors.Is(err, model.ErrUsageRefundInvalid), errors.Is(err, model.ErrUsageRefundLimit):
		status = http.StatusBadRequest
	case errors.Is(err, model.ErrUsageRefundForbidden):
		status = http.StatusForbidden
	case errors.Is(err, model.ErrUsageRefundStale):
		status = http.StatusConflict
	case errors.Is(err, gorm.ErrRecordNotFound):
		status = http.StatusNotFound
	}
	message := err.Error()
	if status == http.StatusInternalServerError {
		common.SysError("usage refund: " + message)
		message = failureMessage
		if failureMessage == "Refund preview failed; no balance was changed" && errors.Is(err, context.DeadlineExceeded) {
			status = http.StatusGatewayTimeout
			message = "Refund scan timed out; narrow the time range or import request IDs. No balance was changed"
		}
	}
	c.JSON(status, gin.H{"success": false, "message": message})
}

func decodeUsageRefund(c *gin.Context, target any) bool {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 2<<20)
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		usageRefundResponse(c, nil, model.ErrUsageRefundInvalid, "")
		return false
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		usageRefundResponse(c, nil, model.ErrUsageRefundInvalid, "")
		return false
	}
	return true
}

func PreviewUsageRefund(c *gin.Context) {
	var request model.UsageRefundSelection
	if !decodeUsageRefund(c, &request) {
		return
	}
	data, err := model.PreviewUsageRefund(c.Request.Context(), c.GetInt("id"), c.GetInt("role"), request)
	usageRefundResponse(c, data, err, "Refund preview failed; no balance was changed")
}

func ListUsageRefundCandidates(c *gin.Context) {
	var request model.UsageRefundCandidateQuery
	if !decodeUsageRefund(c, &request) {
		return
	}
	data, err := model.ListUsageRefundCandidates(c.Request.Context(), c.GetInt("id"), c.GetInt("role"), request)
	usageRefundResponse(c, data, err, "Failed to load refund candidates")
}

// ListBillingAnomalies 列出异常计费消费日志（仅超级管理员），供审核与转入批量返还。
func ListBillingAnomalies(c *gin.Context) {
	var request model.BillingAnomalyQuery
	if !decodeUsageRefund(c, &request) {
		return
	}
	data, err := model.ListBillingAnomalies(c.Request.Context(), c.GetInt("id"), c.GetInt("role"), request)
	usageRefundResponse(c, data, err, "Failed to load billing anomalies")
}

func ExecuteUsageRefund(c *gin.Context) {
	var request struct {
		Quota int `json:"expected_quota"`
		Count int `json:"expected_count"`
	}
	if !decodeUsageRefund(c, &request) {
		return
	}
	data, err := model.ExecuteUsageRefund(c.Request.Context(), c.Param("id"), c.GetInt("id"), c.GetInt("role"), request.Quota, request.Count)
	usageRefundResponse(c, data, err, "refund operation failed; query the batch before retrying")
}

func GetUsageRefund(c *gin.Context) {
	data, err := model.GetUsageRefund(c.Request.Context(), c.Param("id"), c.GetInt("id"))
	usageRefundResponse(c, data, err, "Failed to load batch")
}

func ListUsageRefunds(c *gin.Context) {
	data, err := model.ListUsageRefunds(c.Request.Context(), c.GetInt("id"))
	usageRefundResponse(c, data, err, "Failed to load batches")
}
