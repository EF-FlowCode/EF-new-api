package controller

import (
	"context"
	"io"
	"net/http"
	"strings"
	"time"

	"eflow.local/new-api/constant"
	"eflow.local/new-api/model"
	"eflow.local/new-api/service"
	management "eflow.local/new-api/service/taskplugin"
	"github.com/gin-gonic/gin"
)

// Called only after VideoProxy verifies task ownership and successful status.
func serveTaskPluginContent(c *gin.Context, task *model.Task, channel *model.Channel, artifactKey ...string) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 60*time.Second)
	defer cancel()
	executor, err := service.NewTaskPluginHTTPExecutorForProxy(channel.GetSetting().Proxy)
	if err != nil {
		videoProxyError(c, 502, "plugin_content_failed", "Invalid content transport")
		return
	}
	if channel.GetSetting().Proxy != "" {
		defer executor.Client.CloseIdleConnections()
	}
	if err := management.Default.CheckProductionEnabled(ctx, channel.Id); err != nil {
		videoProxyError(c, 503, "plugin_disabled", "Plugin is unavailable")
		return
	}
	base := channel.GetBaseURL()
	if base == "" {
		base = constant.GetChannelBaseURL(channel.Type)
	}
	if base == "" && task.PrivateData.Plugin.Key == "vertex-ai" {
		base = "https://aiplatform.googleapis.com"
	}
	env := management.TaskQueryEnvironment{BaseURL: base, Auth: map[string]any{}}
	if task.PrivateData.Plugin.Key != "vertex-ai" {
		env, err = service.PrepareTaskPluginAuth(ctx, channel.Type, base, task.PrivateData.Key, channel.Other, executor)
	}
	if err != nil {
		videoProxyError(c, 502, "plugin_auth_failed", "Content authentication failed")
		return
	}
	plan, err := management.Default.PrepareTaskQueries(ctx, []int64{task.ID}, map[int]management.TaskQueryEnvironment{channel.Id: env})
	if err != nil || len(plan.Queries) != 1 {
		videoProxyError(c, 502, "plugin_content_failed", "Content identity is unavailable")
		return
	}
	if err := plan.Queries[0].ValidateContent(ctx); err != nil {
		videoProxyError(c, 503, "plugin_disabled", "Content is unavailable")
		return
	}
	if task.PrivateData.Plugin.Key == "vertex-ai" {
		value := extractVertexVideoURLFromTaskData(task)
		if strings.HasPrefix(value, "data:") {
			c.Header("Cache-Control", "private, no-store")
			if err := writeVideoDataURL(c, value); err != nil && !c.Writer.Written() {
				videoProxyError(c, 502, "plugin_content_failed", "Stored video content is unavailable")
			}
			return
		}
	}
	response, err := plan.Queries[0].OpenContent(ctx, executor, artifactKey...)
	if err != nil {
		videoProxyError(c, 502, "plugin_content_failed", "Content request failed")
		return
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		videoProxyError(c, 502, "plugin_content_failed", "Content provider rejected the request")
		return
	}
	if value := response.Header.Get("Content-Type"); value != "" {
		c.Header("Content-Type", value)
	}
	c.Header("Cache-Control", "private, no-store")
	c.Status(http.StatusOK)
	_, _ = io.Copy(c.Writer, response.Body)
}
