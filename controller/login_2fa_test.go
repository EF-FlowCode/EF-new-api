package controller

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"eflow.local/new-api/common"
	"eflow.local/new-api/i18n"
	"eflow.local/new-api/internal/testdb"
	"eflow.local/new-api/model"
	"eflow.local/new-api/setting/system_setting"
	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/pquerna/otp/totp"
	"github.com/stretchr/testify/require"
)

func useLogin2FATestDB(t *testing.T) {
	t.Helper()
	require.NoError(t, i18n.Init())
	db := testdb.OpenMySQL8(t, &model.User{}, &model.TwoFA{}, &model.TwoFABackupCode{}, &model.AuditLog{}, &model.PasskeyCredential{})
	previous, previousLog, previousRedis := model.DB, model.LOG_DB, common.RedisEnabled
	model.DB, model.LOG_DB, common.RedisEnabled = db, db, false
	t.Cleanup(func() { model.DB, model.LOG_DB, common.RedisEnabled = previous, previousLog, previousRedis })

	user := model.User{Username: "tg-2fa", Password: "password-hash", AffCode: "tg2fa1",
		TelegramId: "123456", Status: common.UserStatusEnabled}
	require.NoError(t, db.Create(&user).Error)
	require.NoError(t, db.Create(&model.TwoFA{UserId: user.Id, Secret: "JBSWY3DPEHPK3PXP", IsEnabled: true}).Error)
}

func enableTelegramLoginForTest(t *testing.T) {
	t.Helper()
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
}

type login2FAHarness struct {
	engine  *gin.Engine
	cookies []*http.Cookie
}

func newLogin2FAHarness() *login2FAHarness {
	h := &login2FAHarness{engine: gin.New()}
	h.engine.Use(sessions.Sessions("session", cookie.NewStore([]byte("login-2fa-test"))))
	h.engine.GET("/telegram", TelegramLogin)
	h.engine.POST("/2fa", Verify2FALogin)
	h.engine.POST("/login/verify", VerifyLogin)
	h.engine.POST("/login/passkey/begin", LoginPasskeyBegin)
	h.engine.GET("/whoami", func(c *gin.Context) {
		id, _ := sessions.Default(c).Get("id").(int)
		c.JSON(http.StatusOK, gin.H{"id": id})
	})
	h.engine.GET("/age-pending", func(c *gin.Context) {
		session := sessions.Default(c)
		session.Set(pending2FAStartedAtSessionKey, time.Now().Add(-pending2FAMaxAge-time.Second).Unix())
		_ = session.Save()
	})
	return h
}

func (h *login2FAHarness) do(method, path, body string) string {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	for _, c := range h.cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	h.engine.ServeHTTP(rec, req)
	if cookies := rec.Result().Cookies(); len(cookies) > 0 {
		h.cookies = cookies
	}
	return rec.Body.String()
}

func TestTelegramLoginRequires2FAWhenEnabled(t *testing.T) {
	useLogin2FATestDB(t)
	enableTelegramLoginForTest(t)
	h := newLogin2FAHarness()

	body := h.do(http.MethodGet, "/telegram?"+signedTelegramAuthorization(telegramBindTestToken, time.Now()).Encode(), "")
	require.Contains(t, body, `"require_2fa":true`)
	require.Equal(t, `{"id":0}`, h.do(http.MethodGet, "/whoami", ""), "third-party login must not sign in before 2FA")

	body = h.do(http.MethodPost, "/2fa", `{"code":"000000"}`)
	require.Contains(t, body, `"success":false`)
	require.Equal(t, `{"id":0}`, h.do(http.MethodGet, "/whoami", ""))
}

func TestVerify2FALoginRejectsExpiredPendingState(t *testing.T) {
	useLogin2FATestDB(t)
	enableTelegramLoginForTest(t)
	h := newLogin2FAHarness()

	require.Contains(t, h.do(http.MethodGet, "/telegram?"+signedTelegramAuthorization(telegramBindTestToken, time.Now()).Encode(), ""), `"require_2fa":true`)
	h.do(http.MethodGet, "/age-pending", "")

	body := h.do(http.MethodPost, "/2fa", `{"code":"000000"}`)
	require.Contains(t, body, "会话已过期")
	// 过期后待验证状态被清掉，再提交也不会进入验证码校验。
	require.Contains(t, h.do(http.MethodPost, "/2fa", `{"code":"000000"}`), "会话已过期")
}

func TestTwoFactorLoginRecordsLoginAudit(t *testing.T) {
	useLogin2FATestDB(t)
	enableTelegramLoginForTest(t)
	h := newLogin2FAHarness()

	require.Contains(t, h.do(http.MethodGet, "/telegram?"+signedTelegramAuthorization(telegramBindTestToken, time.Now()).Encode(), ""), `"require_2fa":true`)
	var pendingRows int64
	require.NoError(t, model.LOG_DB.Model(&model.AuditLog{}).Count(&pendingRows).Error)
	require.Zero(t, pendingRows, "a login waiting for 2FA is not a completed sign-in")

	code, err := totp.GenerateCode("JBSWY3DPEHPK3PXP", time.Now())
	require.NoError(t, err)
	require.Contains(t, h.do(http.MethodPost, "/2fa", `{"code":"`+code+`"}`), `"success":true`)

	var row model.AuditLog
	require.NoError(t, model.LOG_DB.Take(&row).Error)
	require.Equal(t, model.AuditCategoryLogin, row.Category)
	require.Equal(t, "user.login", row.Action)
	require.Equal(t, "tg-2fa", row.Username)
	require.Equal(t, common.RoleCommonUser, row.ActorRole)
	require.Equal(t, "telegram+2fa", row.Other.LoginMethod)
}

func TestLoginVerificationIsBoundToTheFlowToken(t *testing.T) {
	useLogin2FATestDB(t)
	enableTelegramLoginForTest(t)
	h := newLogin2FAHarness()

	body := h.do(http.MethodGet, "/telegram?"+signedTelegramAuthorization(telegramBindTestToken, time.Now()).Encode(), "")
	require.Contains(t, body, `"require_verification":true`)
	require.Contains(t, body, `"method":"2fa"`)
	flow := regexp.MustCompile(`"flow_token":"([^"]+)"`).FindStringSubmatch(body)[1]

	code, err := totp.GenerateCode("JBSWY3DPEHPK3PXP", time.Now())
	require.NoError(t, err)
	require.Contains(t, h.do(http.MethodPost, "/login/verify", `{"flow_token":"wrong","code":"`+code+`"}`), "AUTH_FLOW_INVALID")
	require.Equal(t, `{"id":0}`, h.do(http.MethodGet, "/whoami", ""))
	require.Contains(t, h.do(http.MethodPost, "/login/verify", `{"flow_token":"`+flow+`","method":"passkey","code":"x"}`), "SECURITY_PROOF_METHOD_MISMATCH")
	require.Contains(t, h.do(http.MethodPost, "/login/verify", `{"flow_token":"`+flow+`","code":"`+code+`"}`), `"success":true`)
	require.NotEqual(t, `{"id":0}`, h.do(http.MethodGet, "/whoami", ""))
	require.Contains(t, h.do(http.MethodPost, "/login/verify", `{"flow_token":"`+flow+`","code":"`+code+`"}`), "AUTH_FLOW_INVALID", "a completed login cannot be replayed")
}

func TestPasskeyOnlyAccountsMustVerifyAfterPassword(t *testing.T) {
	useLogin2FATestDB(t)
	enableTelegramLoginForTest(t)
	require.NoError(t, model.DB.Where("1 = 1").Delete(&model.TwoFA{}).Error)
	var user model.User
	require.NoError(t, model.DB.Where("username = ?", "tg-2fa").First(&user).Error)
	require.NoError(t, model.DB.Create(&model.PasskeyCredential{UserID: user.Id, CredentialID: "cred-only", PublicKey: "pk"}).Error)
	h := newLogin2FAHarness()

	body := h.do(http.MethodGet, "/telegram?"+signedTelegramAuthorization(telegramBindTestToken, time.Now()).Encode(), "")
	require.Contains(t, body, `"require_verification":true`)
	require.Contains(t, body, `"require_2fa":false`, "old clients do not see a TOTP prompt they cannot satisfy")
	require.Contains(t, body, `"method":"passkey"`)
	require.Equal(t, `{"id":0}`, h.do(http.MethodGet, "/whoami", ""), "a Passkey account is not signed in before verifying it")
}
