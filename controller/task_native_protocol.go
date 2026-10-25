package controller

import (
	"encoding/json"
	"net/http"

	"eflow.local/new-api/common"
	"eflow.local/new-api/constant"
	"eflow.local/new-api/model"
	"eflow.local/new-api/pkg/jsplugin"
	host "eflow.local/new-api/relay/channel/task/jsplugin"
	management "eflow.local/new-api/service/taskplugin"
	"github.com/gin-gonic/gin"
)

func RetrieveGenericTask(c *gin.Context) {
	task, _, err := management.Default.PublicTask(c.Request.Context(), c.Param("key"), common.GetContextKeyInt(c, constant.ContextKeyUserId), "")
	if err != nil {
		abortNativeTask(c)
		return
	}
	c.JSON(http.StatusOK, management.GenericTaskView(task))
}

func abortNativeTask(c *gin.Context) {
	c.AbortWithStatusJSON(404, gin.H{"error": gin.H{"code": "task_unavailable", "message": "Task is unavailable"}})
}

func RelayNativeTask(c *gin.Context) {
	value, query := c.Get(management.NativeQueryContextKey)
	if !query {
		RelayTask(c)
		return
	}
	n, ok := value.(*management.NativeRequest)
	if !ok || len(n.TaskIDs) == 0 {
		abortNativeTask(c)
		return
	}
	var handle *host.Handle
	var first *model.Task
	views := []any{}
	channelDisabled := false
	for _, id := range n.TaskIDs {
		task, h, err := management.Default.PublicTask(c.Request.Context(), id, common.GetContextKeyInt(c, constant.ContextKeyUserId), "")
		if err != nil || h.Identity().Key != n.Key {
			abortNativeTask(c)
			return
		}
		// Each batch has one source/renderer and one provider account. Never let
		// a newer decoder reinterpret another version's task identity.
		if handle != nil && (handle.Identity() != h.Identity() || first.ChannelId != task.ChannelId || first.PrivateData.Key != task.PrivateData.Key) {
			abortNativeTask(c)
			return
		}
		r, _, matched := management.MatchNativeRoute(h.Meta(), n.Method, n.Path)
		if !matched || r.Type == jsplugin.RouteTypeSubmit {
			abortNativeTask(c)
			return
		}
		if r.Type == jsplugin.RouteTypeDynamic {
			_, params, _ := management.MatchNativeRoute(h.Meta(), n.Method, n.Path)
			intent, e := h.Native(c.Request.Context(), r.Decode, map[string]any{"body": n.Body, "path": n.Path, "method": n.Method, "query": n.Query, "params": params})
			decoded, valid := intent.(map[string]any)
			rawIDs, _ := json.Marshal(decoded["taskIds"])
			expected, _ := json.Marshal(n.TaskIDs)
			if e != nil || !valid || decoded["kind"] != "query" || string(rawIDs) != string(expected) {
				abortNativeTask(c)
				return
			}
		}
		if len(r.Models) > 0 {
			match := false
			for _, name := range r.Models {
				if name == task.Properties.UpstreamModelName {
					match = true
				}
			}
			if !match {
				abortNativeTask(c)
				return
			}
		}
		// PublicTaskView hides provider terminal data while settlement is pending.
		_, view := management.PublicTaskView(task)
		views = append(views, view)
		channelDisabled = channelDisabled || task.ChannelDisabled
		handle, first = h, task
	}
	r, params, _ := management.MatchNativeRoute(handle.Meta(), n.Method, n.Path)
	pc, _ := management.PublicTaskView(first)
	pc["path"], pc["method"], pc["params"], pc["query"] = n.Path, n.Method, params, n.Query
	var payload any = views
	if r.Type == jsplugin.RouteTypeQuery {
		if len(views) != 1 {
			abortNativeTask(c)
			return
		}
		payload = views[0]
	}
	out, err := handle.Native(c.Request.Context(), r.Render, pc, payload)
	if err != nil {
		c.JSON(500, gin.H{"error": gin.H{"code": "task_render_failed"}})
		return
	}
	// Native bodies are provider-shaped; signal historical results out of band.
	if channelDisabled {
		c.Header("X-Task-Channel-Status", management.ChannelStatusDisabled)
	}
	c.JSON(http.StatusOK, out)
}
