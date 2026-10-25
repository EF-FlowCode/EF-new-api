package middleware

import (
	"fmt"
	"net/url"
	"strings"

	"eflow.local/new-api/common"
	"github.com/gin-gonic/gin"
)

// eflow: 请求日志里的这些查询参数是凭证或个人信息（OAuth code/state、Gemini ?key=、
// Telegram 登录签名、邮箱等），输出前打码。只影响此后新写的日志行，不改动已有日志。
var sensitiveLogQueryKeys = map[string]struct{}{
	"code": {}, "state": {}, "key": {}, "hash": {}, "flow_token": {}, "token": {},
	"email": {}, "access_token": {}, "refresh_token": {}, "id_token": {},
	"turnstile": {}, "password": {}, "verification_code": {},
}

// redactLogPath 对 path 中敏感查询参数的值打码，其余参数与顺序保持不变。
func redactLogPath(path string) string {
	base, query, found := strings.Cut(path, "?")
	if !found || query == "" {
		return path
	}
	parts := strings.Split(query, "&")
	for i, part := range parts {
		name, _, hasValue := strings.Cut(part, "=")
		decoded, err := url.QueryUnescape(name)
		if err != nil {
			decoded = name
		}
		if _, sensitive := sensitiveLogQueryKeys[strings.ToLower(decoded)]; sensitive && hasValue {
			parts[i] = name + "=***"
		}
	}
	return base + "?" + strings.Join(parts, "&")
}

const RouteTagKey = "route_tag"

func RouteTag(tag string) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Set(RouteTagKey, tag)
		c.Next()
	}
}

func SetUpLogger(server *gin.Engine) {
	server.Use(gin.LoggerWithFormatter(func(param gin.LogFormatterParams) string {
		var requestID string
		if param.Keys != nil {
			requestID, _ = param.Keys[common.RequestIdKey].(string)
		}
		tag, _ := param.Keys[RouteTagKey].(string)
		if tag == "" {
			tag = "web"
		}
		return fmt.Sprintf("[GIN] %s | %s | %s | %3d | %13v | %15s | %7s %s\n",
			param.TimeStamp.Format("2006/01/02 - 15:04:05"),
			tag,
			requestID,
			param.StatusCode,
			param.Latency,
			param.ClientIP,
			param.Method,
			redactLogPath(param.Path),
		)
	}))
}
