package router

import (
	"eflow.local/new-api/controller"
	"eflow.local/new-api/middleware"
	"github.com/gin-gonic/gin"
)

func SetTaskRouter(router *gin.Engine) {
	tasks := router.Group("/v1/tasks", middleware.RouteTag("relay"), middleware.TokenAuth())
	tasks.POST("/:key", middleware.PinGenericTask(), middleware.Distribute(), middleware.SystemPerformanceCheck(), middleware.ModelRequestRateLimit(), controller.RelayTask)
	tasks.GET("/:key", middleware.TaskQueryRateLimit(), controller.RetrieveGenericTask)
	// Provider namespaces are stable host mounts. The enabled fixed source
	// manifest decides the exact path/method on every request, including rebinds.
	for _, namespace := range []string{"ali", "doubao", "kling", "jimeng"} {
		group := router.Group("/"+namespace, middleware.RouteTag("relay"), middleware.TokenAuth(), middleware.PinNativeTask(), middleware.TaskQueryOnly(middleware.TaskQueryRateLimit()), middleware.TaskSubmitOnly(middleware.Distribute()), middleware.TaskSubmitOnly(middleware.SystemPerformanceCheck()), middleware.TaskSubmitOnly(middleware.ModelRequestRateLimit()))
		group.Any("/*task_path", controller.RelayNativeTask)
	}
}
