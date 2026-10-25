package middleware

import (
	"encoding/json"
	"net/http"
	"net/url"
	"time"

	"eflow.local/new-api/common"
	"eflow.local/new-api/pkg/requestip"
	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
)

// eflow: Turnstile 通过标记 10 分钟有效，过期后重新校验，不再在整个会话期内免检。
const (
	turnstileSessionKey   = "turnstile"
	turnstilePassedMaxAge = 10 * time.Minute
)

type turnstileCheckResponse struct {
	Success bool `json:"success"`
}

// turnstileVerifyClient 限制 Cloudflare 校验耗时，避免上游无响应时注册请求一直挂起。
var turnstileVerifyClient = &http.Client{Timeout: 10 * time.Second}

func TurnstileCheck() gin.HandlerFunc {
	return func(c *gin.Context) {
		authSettings := common.GetAuthSettings()
		common.SetRequestAuthSettings(c, authSettings)
		if authSettings.TurnstileCheckEnabled {
			session := sessions.Default(c)
			if passedAt, ok := session.Get(turnstileSessionKey).(int64); ok &&
				time.Since(time.Unix(passedAt, 0)) < turnstilePassedMaxAge {
				c.Next()
				return
			}
			response := c.Query("turnstile")
			if response == "" {
				c.JSON(http.StatusOK, gin.H{
					"success": false,
					"message": "Turnstile token 为空",
				})
				c.Abort()
				return
			}
			rawRes, err := turnstileVerifyClient.PostForm("https://challenges.cloudflare.com/turnstile/v0/siteverify", url.Values{
				"secret":   {authSettings.TurnstileSecretKey},
				"response": {response},
				"remoteip": {requestip.GetClientIP(c)},
			})
			if err != nil {
				common.SysLog(err.Error())
				c.JSON(http.StatusOK, gin.H{
					"success": false,
					"message": err.Error(),
				})
				c.Abort()
				return
			}
			defer rawRes.Body.Close()
			var res turnstileCheckResponse
			err = json.NewDecoder(rawRes.Body).Decode(&res)
			if err != nil {
				common.SysLog(err.Error())
				c.JSON(http.StatusOK, gin.H{
					"success": false,
					"message": err.Error(),
				})
				c.Abort()
				return
			}
			if !res.Success {
				c.JSON(http.StatusOK, gin.H{
					"success": false,
					"message": "Turnstile 校验失败，请刷新重试！",
				})
				c.Abort()
				return
			}
			session.Set(turnstileSessionKey, time.Now().Unix())
			err = session.Save()
			if err != nil {
				c.JSON(http.StatusOK, gin.H{
					"message": "无法保存会话信息，请重试",
					"success": false,
				})
				c.Abort()
				return
			}
		}
		c.Next()
	}
}
