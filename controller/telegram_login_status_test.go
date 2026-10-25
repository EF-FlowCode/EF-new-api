package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"eflow.local/new-api/common"
	"eflow.local/new-api/model"
	"eflow.local/new-api/setting/system_setting"
	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestTelegramLoginRejectsDisabledUser(t *testing.T) {
	db := useAccountSecurityTestDB(t)
	previous := system_setting.GetLegacyOAuthSettings()
	require.NoError(t, system_setting.UpdateLegacyOAuthSettings(map[string]string{
		"TelegramOAuthEnabled": "true",
		"TelegramBotToken":     telegramBindTestToken,
	}))
	t.Cleanup(func() {
		enabled := "false"
		if previous.TelegramOAuthEnabled {
			enabled = "true"
		}
		_ = system_setting.UpdateLegacyOAuthSettings(map[string]string{
			"TelegramOAuthEnabled": enabled,
			"TelegramBotToken":     previous.TelegramBotToken,
		})
	})
	user := model.User{Username: "tg-banned", Password: "password-hash", AffCode: "tgban1",
		TelegramId: "123456", Status: common.UserStatusDisabled}
	require.NoError(t, db.Create(&user).Error)

	engine := gin.New()
	engine.Use(sessions.Sessions("session", cookie.NewStore([]byte("telegram-login-status-test"))))
	engine.GET("/login", TelegramLogin)
	res := httptest.NewRecorder()
	engine.ServeHTTP(res, httptest.NewRequest(http.MethodGet,
		"/login?"+signedTelegramAuthorization(telegramBindTestToken, time.Now()).Encode(), nil))
	require.Contains(t, res.Body.String(), `"success":false`)
	require.Contains(t, res.Body.String(), "用户已被封禁")
	require.Empty(t, res.Result().Cookies(), "no login session may be issued")
}
