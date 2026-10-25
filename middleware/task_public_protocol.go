package middleware

import (
	management "eflow.local/new-api/service/taskplugin"
	"github.com/gin-gonic/gin"
	"net/http"
)

// Ownership inspection never rewrites BodyStorage or the original model.
func PinTaskPublicProtocol() gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.Method != http.MethodPost || management.ProtocolForPath(c.Request.URL.Path) == "" {
			c.Next()
			return
		}
		req, err := getModelFromRequest(c)
		if err != nil || req == nil || req.Model == "" {
			c.Next()
			return
		}
		route, err := management.Default.ResolvePublicRoute(c.Request.Context(), req.Model, c.Request.URL.Path)
		if err != nil {
			abortWithOpenAiMessage(c, http.StatusConflict, "Task protocol routing is unavailable or ambiguous")
			return
		}
		if route != nil {
			c.Set(management.PublicRouteContextKey, route)
		}
		c.Next()
	}
}
