package controller

import (
	"net/http"

	perfmetrics "eflow.local/new-api/pkg/perf_metrics"
	"eflow.local/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
)

// Both routes expose only the configured monitoring groups, never channel or
// request/token counts. Model performance failure does not hide group status.
func GetMonitoringModels(c *gin.Context) {
	setting := operation_setting.GetGroupMonitoringSetting()
	result, err := perfmetrics.QueryMonitoringModels(c.Request.Context(), setting.MonitoringGroups)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "message": "Failed to load model monitoring data"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": result})
}
