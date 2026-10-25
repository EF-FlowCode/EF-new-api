package service

import (
	"eflow.local/new-api/model"
	management "eflow.local/new-api/service/taskplugin"
	"github.com/gin-gonic/gin"
)

func TaskPublicRoute(c *gin.Context) *management.PublicRoute {
	if c == nil {
		return nil
	}
	value, _ := c.Get(management.PublicRouteContextKey)
	route, _ := value.(*management.PublicRoute)
	return route
}

func AllowsTaskPublicChannel(c *gin.Context, channel *model.Channel) bool {
	return management.Default.Allows(TaskPublicRoute(c), channel)
}
