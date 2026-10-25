package middleware

import (
	"eflow.local/new-api/common"
	"eflow.local/new-api/service"
	"github.com/gin-gonic/gin"
)

// BodyStorageCleanup 请求体存储清理中间件
// 在请求处理完成后自动清理磁盘/内存缓存
func BodyStorageCleanup() gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			common.CleanupBodyStorage(c)
			service.CleanupFileSources(c)
		}()
		c.Next()
	}
}
