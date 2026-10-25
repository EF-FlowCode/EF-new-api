package controller

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"eflow.local/new-api/common"
	"eflow.local/new-api/model"
	"eflow.local/new-api/setting/system_setting"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
)

const (
	// The legacy Telegram widget has no nonce. Keep its signed assertion short-lived
	// so captured callbacks cannot be reused indefinitely.
	telegramAuthorizationMaxAge     = 5 * time.Minute
	telegramAuthorizationFutureSkew = 2 * time.Minute

	// 绑定回调是浏览器 GET 跳转，只能 cookie 鉴权；一次性 flow 令牌把回调绑定到
	// 本人发起的绑定流程，防止他人诱导已登录用户打开携带其 Telegram 签名的链接。
	telegramBindFlowParam       = "flow_token"
	telegramBindFlowSessionKey  = "telegram_bind_flow"
	telegramBindFlowExpireKey   = "telegram_bind_flow_expire"
	telegramBindFlowMaxLifetime = 10 * time.Minute
)

// TelegramBindStart 为当前登录用户签发一次性绑定 flow 令牌，前端将其放入 Telegram 组件回调地址。
func TelegramBindStart(c *gin.Context) {
	if telegramLegacyRetired(c) {
		return
	}
	settings := system_setting.GetLegacyOAuthSettings()
	if !settings.TelegramOAuthEnabled {
		c.JSON(200, gin.H{
			"message": "管理员未开启通过 Telegram 登录以及注册",
			"success": false,
		})
		return
	}
	token := common.GetRandomString(32)
	session := sessions.Default(c)
	session.Set(telegramBindFlowSessionKey, token)
	session.Set(telegramBindFlowExpireKey, time.Now().Add(telegramBindFlowMaxLifetime).Unix())
	if err := session.Save(); err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    token,
	})
}

// consumeTelegramBindFlow 校验并立即作废 session 中的 flow 令牌，无论成功与否都不可复用。
func consumeTelegramBindFlow(c *gin.Context, provided string) bool {
	session := sessions.Default(c)
	expected, _ := session.Get(telegramBindFlowSessionKey).(string)
	expireAt, _ := session.Get(telegramBindFlowExpireKey).(int64)
	session.Delete(telegramBindFlowSessionKey)
	session.Delete(telegramBindFlowExpireKey)
	if err := session.Save(); err != nil {
		common.SysLog("TelegramBind clear flow token failed: " + err.Error())
		return false
	}
	if expected == "" || provided == "" || time.Now().Unix() > expireAt {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(expected), []byte(provided)) == 1
}

func TelegramBind(c *gin.Context) {
	if telegramLegacyRetired(c) {
		return
	}
	settings := system_setting.GetLegacyOAuthSettings()
	if !settings.TelegramOAuthEnabled {
		c.JSON(200, gin.H{
			"message": "管理员未开启通过 Telegram 登录以及注册",
			"success": false,
		})
		return
	}
	params := c.Request.URL.Query()
	flowToken := params.Get(telegramBindFlowParam)
	params.Del(telegramBindFlowParam)
	if !consumeTelegramBindFlow(c, flowToken) {
		c.JSON(200, gin.H{
			"message": "绑定流程已失效，请重新发起 Telegram 绑定",
			"success": false,
		})
		return
	}
	telegramId, err := verifyTelegramAuthorization(params, settings.TelegramBotToken, time.Now())
	if err != nil {
		common.SysLog("TelegramBind authorization failed: " + err.Error())
		c.JSON(200, gin.H{
			"message": "无效的请求",
			"success": false,
		})
		return
	}
	if model.IsTelegramIdAlreadyTaken(telegramId) {
		c.JSON(200, gin.H{
			"message": "该 Telegram 账户已被绑定",
			"success": false,
		})
		return
	}

	// 路由已挂 SessionAuth；这里只更新绑定列，避免完整用户快照覆盖并发的状态变更。
	userId := c.GetInt("id")
	if err := model.UpdateUserBindColumn(userId, "telegram_id", telegramId); err != nil {
		c.JSON(200, gin.H{
			"message": err.Error(),
			"success": false,
		})
		return
	}

	c.Redirect(302, common.LegacyConsolePath("/console/personal"))
}

func TelegramLogin(c *gin.Context) {
	if telegramLegacyRetired(c) {
		return
	}
	settings := system_setting.GetLegacyOAuthSettings()
	if !settings.TelegramOAuthEnabled {
		c.JSON(200, gin.H{
			"message": "管理员未开启通过 Telegram 登录以及注册",
			"success": false,
		})
		return
	}
	params := c.Request.URL.Query()
	telegramId, err := verifyTelegramAuthorization(params, settings.TelegramBotToken, time.Now())
	if err != nil {
		common.SysLog("TelegramLogin authorization failed: " + err.Error())
		c.JSON(200, gin.H{
			"message": "无效的请求",
			"success": false,
		})
		return
	}

	user := model.User{TelegramId: telegramId}
	if err := user.FillUserByTelegramId(); err != nil {
		c.JSON(200, gin.H{
			"message": err.Error(),
			"success": false,
		})
		return
	}
	if user.Status != common.UserStatusEnabled {
		c.JSON(http.StatusOK, gin.H{
			"message": "用户已被封禁",
			"success": false,
		})
		return
	}
	completeLogin(&user, c, loginMethodTelegram)
}

func verifyTelegramAuthorization(params url.Values, token string, now time.Time) (string, error) {
	if token == "" {
		return "", errors.New("telegram bot token is empty")
	}
	for _, values := range params {
		if len(values) != 1 {
			return "", errors.New("telegram authorization contains duplicate parameters")
		}
	}

	telegramID := params.Get("id")
	hash := params.Get("hash")
	authDateText := params.Get("auth_date")
	if telegramID == "" || hash == "" || authDateText == "" {
		return "", errors.New("telegram authorization is incomplete")
	}
	authDate, err := strconv.ParseInt(authDateText, 10, 64)
	if err != nil {
		return "", errors.New("telegram authorization date is invalid")
	}
	if authDate < now.Add(-telegramAuthorizationMaxAge).Unix() ||
		authDate > now.Add(telegramAuthorizationFutureSkew).Unix() {
		return "", errors.New("telegram authorization has expired")
	}

	strs := make([]string, 0, len(params)-1)
	for k, v := range params {
		if k == "hash" {
			continue
		}
		strs = append(strs, k+"="+v[0])
	}
	sort.Strings(strs)
	secret := sha256.Sum256([]byte(token))
	mac := hmac.New(sha256.New, secret[:])
	_, _ = mac.Write([]byte(strings.Join(strs, "\n")))
	providedHash, err := hex.DecodeString(hash)
	if err != nil || !hmac.Equal(providedHash, mac.Sum(nil)) {
		return "", errors.New("telegram authorization signature is invalid")
	}

	return telegramID, nil
}

// telegramLegacyRetired rejects the legacy Login Widget once Telegram OpenID
// Connect is configured; until then the widget keeps working.
func telegramLegacyRetired(c *gin.Context) bool {
	if !system_setting.GetLegacyOAuthSettings().TelegramOIDCEnabled() {
		return false
	}
	writeSecurityStatusError(c, http.StatusOK, "TELEGRAM_LEGACY_RETIRED", "Telegram sign-in has moved to the new authorization page. Refresh the page and try again.")
	return true
}
