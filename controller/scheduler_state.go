package controller

import (
	"net/http"
	"strconv"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service/multi_key_scheduler"
	"github.com/gin-gonic/gin"
)

// GetSchedulerState 给 admin-api 状态面板提供单 channel 的 multi-key 调度器
// 实时快照：(keyFp, model) 的 RPM/TPM/in_flight/cool_until/last_429。
//
// 路由约定 GET /api/channel/:id/scheduler/state，权限随 channelRoute 走 AdminAuth。
//
// 响应形如：
//
//	{
//	  "channel_id": 99,
//	  "enabled": true,
//	  "entries": [
//	    {"key_fp":"abcd...", "model":"qwen-plus", "rpm_count":3, "in_flight":1, ...}
//	  ]
//	}
//
// 注：返回的是 keyFp（sha256 前 8 字节十六进制），明文 key 永远不出包；admin-api
// 自己有 sk → fp 的映射（同步 sk 时算并存），渲染时再做关联展示。
func GetSchedulerState(c *gin.Context) {
	idStr := c.Param("id")
	channelID, err := strconv.Atoi(idStr)
	if err != nil || channelID <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "invalid channel id",
		})
		return
	}

	ch, err := model.GetChannelById(channelID, false)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"success": false,
			"message": "channel not found",
		})
		return
	}

	enabled := ch.ChannelInfo.IsMultiKey && multi_key_scheduler.IsEnabledFor(ch.ChannelInfo.SchedulerConfig)
	var entries []multi_key_scheduler.StateEntry
	if enabled {
		entries = multi_key_scheduler.SnapshotChannel(c.Request.Context(), channelID)
	}
	if entries == nil {
		// 让前端拿到的总是 [] 而不是 null，省去前端判空
		entries = []multi_key_scheduler.StateEntry{}
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"channel_id":   channelID,
			"channel_name": ch.Name,
			"is_multi_key": ch.ChannelInfo.IsMultiKey,
			"enabled":      enabled,
			"entries":      entries,
		},
	})
}