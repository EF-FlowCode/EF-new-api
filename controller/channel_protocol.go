package controller

import (
	"fmt"
	"strconv"
	"time"

	"eflow.local/new-api/common"
	"eflow.local/new-api/constant"
	"eflow.local/new-api/dto"
	"eflow.local/new-api/model"
	"eflow.local/new-api/service"

	"github.com/gin-gonic/gin"
)

// eflow: protocol routing administration.

type probeChannelProtocolsRequest struct {
	Model string `json:"model"`
}

// ProbeChannelProtocols runs the protocol probe and stores the result for the
// confirmation dialog. It never changes the confirmed protocol set.
func ProbeChannelProtocols(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	var req probeChannelProtocolsRequest
	if c.Request.ContentLength > 0 {
		if err := c.ShouldBindJSON(&req); err != nil {
			common.ApiError(c, err)
			return
		}
	}
	channel, err := model.GetChannelById(id, true)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	result, err := service.ProbeChannelProtocols(c.Request.Context(), channel, req.Model)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if err := model.UpdateChannelOtherSettings(id, func(settings *dto.ChannelOtherSettings) {
		settings.UpstreamProtocolProbe = result
	}); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, result)
}

type confirmChannelProtocolsRequest struct {
	Protocols []string `json:"protocols"`
}

// ConfirmChannelProtocols writes the administrator-confirmed protocol set. An
// empty list clears it and restores the legacy adaptor behavior.
func ConfirmChannelProtocols(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	var req confirmChannelProtocolsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiError(c, err)
		return
	}
	channel, err := model.GetChannelById(id, false)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if !constant.SupportsProtocolRouting(channel.Type) {
		common.ApiError(c, fmt.Errorf("渠道类型 %d 不支持协议路由", channel.Type))
		return
	}
	protocols := dto.NormalizeUpstreamProtocols(req.Protocols)
	operator := c.GetInt("id")
	if err := model.UpdateChannelOtherSettings(id, func(settings *dto.ChannelOtherSettings) {
		settings.UpstreamProtocols = protocols
		if len(protocols) == 0 {
			settings.UpstreamProtocolsConfirmedAt = 0
			settings.UpstreamProtocolsConfirmedBy = 0
			return
		}
		settings.UpstreamProtocolsConfirmedAt = time.Now().Unix()
		settings.UpstreamProtocolsConfirmedBy = operator
	}); err != nil {
		common.ApiError(c, err)
		return
	}
	model.InitChannelCache()
	common.ApiSuccess(c, gin.H{"protocols": protocols})
}
