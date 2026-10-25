package middleware

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"eflow.local/new-api/common"
	"eflow.local/new-api/i18n"
	host "eflow.local/new-api/relay/channel/task/jsplugin"
	management "eflow.local/new-api/service/taskplugin"
	"github.com/gin-gonic/gin"
)

func PinGenericTask() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !strings.HasPrefix(c.GetHeader("Content-Type"), "application/json") {
			abortWithOpenAiMessage(c, 400, "Generic task requires JSON")
			return
		}
		req, err := getModelFromRequest(c)
		if err != nil {
			abortWithOpenAiMessage(c, invalidRequestStatus(err), i18n.T(c, i18n.MsgDistributorInvalidRequest, map[string]any{"Error": err.Error()}))
			return
		}
		if req == nil || req.Model == "" {
			abortWithOpenAiMessage(c, 400, "model is required")
			return
		}
		route, err := management.Default.ResolveGenericRoute(c.Request.Context(), c.Param("key"), req.Model)
		if err != nil {
			abortWithOpenAiMessage(c, 409, "Task binding or model is unavailable")
			return
		}
		c.Set(management.PublicRouteContextKey, route)
		c.Next()
	}
}

func PinNativeTask() gin.HandlerFunc {
	return func(c *gin.Context) {
		n := &management.NativeRequest{Path: c.Request.URL.Path, Method: c.Request.Method, Query: c.Request.URL.Query()}
		if c.Request.Method != http.MethodGet {
			if !strings.HasPrefix(c.GetHeader("Content-Type"), "application/json") {
				abortWithOpenAiMessage(c, 400, "Native task requires JSON")
				return
			}
			storage, err := common.GetBodyStorage(c)
			if err != nil {
				abortWithOpenAiMessage(c, invalidRequestStatus(err), "Invalid task body: "+err.Error())
				return
			}
			raw, err := storage.Bytes()
			var body map[string]any
			if err != nil || len(raw) > host.MaxPayloadBytes || json.Unmarshal(raw, &body) != nil || body == nil {
				abortWithOpenAiMessage(c, 400, "Invalid task body")
				return
			}
			n.Body = map[string]any{"kind": "json", "value": body}
		}
		route, err := management.Default.ResolveNativeRequest(c.Request.Context(), n)
		if err != nil {
			status := 400
			if errors.Is(err, management.ErrNotFound) {
				status = 404
			}
			abortWithOpenAiMessage(c, status, "Native task route is unavailable or invalid")
			return
		}
		if route == nil {
			c.Set(management.NativeQueryContextKey, n)
		} else {
			c.Set(management.PublicRouteContextKey, route)
		}
		c.Next()
	}
}

// TaskQueryOnly is the inverse of TaskSubmitOnly: next runs only for native
// status queries resolved by PinNativeTask.
func TaskQueryOnly(next gin.HandlerFunc) gin.HandlerFunc {
	return func(c *gin.Context) {
		if _, query := c.Get(management.NativeQueryContextKey); !query {
			c.Next()
			return
		}
		next(c)
	}
}

func TaskSubmitOnly(next gin.HandlerFunc) gin.HandlerFunc {
	return func(c *gin.Context) {
		if _, query := c.Get(management.NativeQueryContextKey); query {
			c.Next()
			return
		}
		next(c)
	}
}
