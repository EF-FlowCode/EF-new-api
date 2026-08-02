package middleware

import (
	"strings"

	"github.com/gin-gonic/gin"
)

func Cache() func(c *gin.Context) {
	return func(c *gin.Context) {
		uri := c.Request.RequestURI
		// eflow: 只有内容哈希命名的静态资源（/static/、/assets/）才长缓存
		// 一周——文件名随构建变，缓存再久也不会拿到旧代码。其余（"/"、HTML、
		// /api/* 含 /api/pricing）一律 no-cache。否则调价/改模型后，老用户浏览器
		// 会继续用一周前缓存的 /api/pricing JSON，前端永远看不到新数据
		// （实测：ASR 改 audio_seconds 后模型广场卡片纹丝不动，根因就是这个
		// 一周缓存，与后端/前端代码无关）。
		if strings.HasPrefix(uri, "/static/") || strings.HasPrefix(uri, "/assets/") {
			c.Header("Cache-Control", "max-age=604800") // hashed assets, safe to cache long
		} else {
			c.Header("Cache-Control", "no-cache")
		}
		c.Header("Cache-Version", "b688f2fb5be447c25e5aa3bd063087a83db32a288bf6a4f35f2d8db310e40b14")
		c.Next()
	}
}
