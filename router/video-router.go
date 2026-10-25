package router

import (
	"eflow.local/new-api/controller"
	"eflow.local/new-api/middleware"

	"github.com/gin-gonic/gin"
)

func SetVideoRouter(router *gin.Engine) {
	// Video proxy: accepts either session auth (dashboard) or token auth (API clients)
	videoProxyRouter := router.Group("/v1")
	videoProxyRouter.Use(middleware.RouteTag("relay"))
	videoProxyRouter.Use(middleware.TokenOrUserAuth())
	{
		videoProxyRouter.GET("/videos/:task_id/content", controller.VideoProxy)
	}

	videoV1Router := router.Group("/v1")
	videoV1Router.Use(middleware.RouteTag("relay"))
	videoV1Router.Use(middleware.TokenAuth(), middleware.PinTaskPublicProtocol(), middleware.Distribute())
	// Submissions start paid upstream work, so they get the same overload and
	// model rate-limit guards as /v1 relay; status polling does not.
	videoSubmit := []gin.HandlerFunc{middleware.SystemPerformanceCheck(), middleware.ModelRequestRateLimit(), controller.RelayTask}
	{
		videoV1Router.POST("/video/generations", videoSubmit...)
		videoV1Router.GET("/video/generations/:task_id", controller.RelayTaskFetch)
		videoV1Router.POST("/videos/:video_id/remix", videoSubmit...)
	}
	// openai compatible API video routes
	// docs: https://platform.openai.com/docs/api-reference/videos/create
	{
		videoV1Router.POST("/videos", videoSubmit...)
		videoV1Router.GET("/videos/:task_id", controller.RelayTaskFetch)
	}
}
