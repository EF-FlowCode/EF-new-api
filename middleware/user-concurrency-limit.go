package middleware

// eflow: 按已登录用户 ID 限制同时 in-flight 的请求数，给重查询路径（如日志读取）兜底。
// 同行被一个用户开 ~1000 并发慢查询打爆 MySQL，rate-limit 是 N/秒维度，挡不住慢查询堆积；
// 真正的护栏是"同一时刻每个用户最多 K 个 in-flight"。
//
// 必须挂在 UserAuth() 之后（依赖 c.GetInt("id")）。
// 实现上故意走 in-memory：访问极频繁、状态短生命周期；走 redis 反而拉高每请求开销。

import (
	"net/http"
	"sync"

	"github.com/gin-gonic/gin"
)

// UserConcurrencyLimit 返回一个 gin 中间件，限制每个用户同时 in-flight 的请求数。
// 超过 maxInFlight 直接 429。每个 mark（标签）维护独立的计数器集合，
// 这样多个挂点（如 /log/self 和 /log/self/export）互不串配额。
func UserConcurrencyLimit(mark string, maxInFlight int32) func(c *gin.Context) {
	if maxInFlight <= 0 {
		// 0 视为禁用，跟其它限流中间件一致
		return defNext
	}
	// 计数归零即删除，内存只与当前 in-flight 用户数相关，不随历史用户数增长。
	var (
		mu       sync.Mutex
		inFlight = map[int]int32{}
	)
	release := func(userId int) {
		mu.Lock()
		if inFlight[userId] <= 1 {
			delete(inFlight, userId)
		} else {
			inFlight[userId]--
		}
		mu.Unlock()
	}
	return func(c *gin.Context) {
		userId := c.GetInt("id")
		if userId == 0 {
			c.Status(http.StatusUnauthorized)
			c.Abort()
			return
		}
		mu.Lock()
		if inFlight[userId] >= maxInFlight {
			mu.Unlock()
			c.Header("X-Eflow-Concurrency-Limit", mark)
			c.Status(http.StatusTooManyRequests)
			c.Abort()
			return
		}
		inFlight[userId]++
		mu.Unlock()
		defer release(userId)
		c.Next()
	}
}
