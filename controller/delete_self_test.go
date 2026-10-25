package controller

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"eflow.local/new-api/common"
	"eflow.local/new-api/i18n"
	"eflow.local/new-api/internal/testdb"
	"eflow.local/new-api/middleware"
	"eflow.local/new-api/model"
	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/pquerna/otp/totp"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// stepUpHarness signs a user into a cookie session and exposes the step-up
// endpoints together with a few proof-protected actions.
type stepUpHarness struct {
	t       *testing.T
	engine  *gin.Engine
	cookies []*http.Cookie
	userID  int
}

func useStepUpTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	gin.SetMode(gin.TestMode)
	require.NoError(t, i18n.Init())
	db := testdb.OpenMySQL8(t, &model.User{}, &model.TwoFA{}, &model.TwoFABackupCode{}, &model.PasskeyCredential{}, &model.AuditLog{}, &model.Log{}, &model.UserAccessToken{}, &model.Option{})
	previous, previousLog, previousRedis := model.DB, model.LOG_DB, common.RedisEnabled
	model.DB, model.LOG_DB, common.RedisEnabled = db, db, false
	t.Cleanup(func() { model.DB, model.LOG_DB, common.RedisEnabled = previous, previousLog, previousRedis })
	return db
}

func newStepUpHarness(t *testing.T, user *model.User) *stepUpHarness {
	h := &stepUpHarness{t: t, engine: gin.New(), userID: user.Id}
	h.engine.Use(sessions.Sessions("session", cookie.NewStore([]byte("step-up-test"))))
	h.engine.GET("/login", func(c *gin.Context) {
		session := sessions.Default(c)
		session.Set("id", user.Id)
		session.Set(LoginAtSessionKey, time.Now().Unix())
		middleware.ResetStepUpSession(session)
		require.NoError(t, session.Save())
	})
	h.engine.GET("/age-login", func(c *gin.Context) {
		session := sessions.Default(c)
		session.Set(LoginAtSessionKey, time.Now().Add(-time.Hour).Unix())
		require.NoError(t, session.Save())
	})
	auth := func(c *gin.Context) { asStepUpUser(t, c) }
	h.engine.GET("/verify/methods", auth, GetVerificationMethods)
	h.engine.POST("/verify", auth, UniversalVerify)
	h.engine.DELETE("/self", auth, DeleteSelf)
	h.engine.POST("/channel/:id/key", auth, middleware.SecureVerificationRequired(), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"success": true, "channel": c.Param("id")})
	})
	h.do(http.MethodGet, "/login", "", "")
	return h
}

// ginContext keeps test route literals short.
type ginContext = gin.Context

// asStepUpUser sets the auth context the way authHelper does for a signed-in user.
func asStepUpUser(t *testing.T, c *gin.Context) {
	t.Helper()
	id, _ := sessions.Default(c).Get("id").(int)
	loaded, err := model.GetUserById(id, false)
	require.NoError(t, err)
	c.Set("id", loaded.Id)
	c.Set("role", loaded.Role)
	c.Set("username", loaded.Username)
}

func (h *stepUpHarness) do(method, path, body, proof string) (int, map[string]any) {
	h.t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if proof != "" {
		req.Header.Set(middleware.SecurityProofHeader, proof)
	}
	for _, c := range h.cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	h.engine.ServeHTTP(rec, req)
	if cookies := rec.Result().Cookies(); len(cookies) > 0 {
		h.cookies = cookies
	}
	var result map[string]any
	if rec.Body.Len() > 0 {
		require.NoError(h.t, json.Unmarshal(rec.Body.Bytes(), &result), rec.Body.String())
	}
	return rec.Code, result
}

func (h *stepUpHarness) proof(body string) string {
	h.t.Helper()
	_, result := h.do(http.MethodPost, "/verify", body, "")
	require.Equal(h.t, true, result["success"], result)
	data := result["data"].(map[string]any)
	return data["proof_token"].(string)
}

func TestDeleteSelfRequiresSingleUseProof(t *testing.T) {
	db := useStepUpTestDB(t)
	hash, err := common.Password2Hash("correct-password")
	require.NoError(t, err)
	user := model.User{Username: "delete-self", Password: hash, AffCode: "dels01", Role: common.RoleCommonUser, Status: common.UserStatusEnabled}
	require.NoError(t, db.Create(&user).Error)
	h := newStepUpHarness(t, &user)

	status, result := h.do(http.MethodDelete, "/self", "", "")
	require.Equal(t, http.StatusForbidden, status)
	require.Equal(t, "SECURITY_PROOF_REQUIRED", result["code"])

	_, result = h.do(http.MethodPost, "/verify", `{"method":"password","scope":"account.delete","password":"wrong"}`, "")
	require.Equal(t, "SECURITY_VERIFICATION_FAILED", result["code"])

	// A proof for another action cannot authorize the deletion.
	keyProof := h.proof(`{"method":"password","scope":"account.password.change","password":"correct-password"}`)
	status, result = h.do(http.MethodDelete, "/self", "", keyProof)
	require.Equal(t, http.StatusForbidden, status)
	require.Equal(t, "SECURITY_PROOF_SCOPE_MISMATCH", result["code"])

	var count int64
	require.NoError(t, db.Model(&model.User{}).Where("id = ?", user.Id).Count(&count).Error)
	require.EqualValues(t, 1, count, "account must survive failed confirmations")

	proof := h.proof(`{"method":"password","scope":"account.delete","password":"correct-password"}`)
	_, result = h.do(http.MethodDelete, "/self", "", proof)
	require.Equal(t, true, result["success"], result)
	require.NoError(t, db.Model(&model.User{}).Where("id = ?", user.Id).Count(&count).Error)
	require.Zero(t, count)

	var audit model.AuditLog
	require.NoError(t, db.Where("action = ?", "user.account_delete").Take(&audit).Error)
	require.Equal(t, model.AuditCategorySecurity, audit.Category)
}

func TestProofIsSingleUseAndBoundToContextAndSession(t *testing.T) {
	db := useStepUpTestDB(t)
	secret := "JBSWY3DPEHPK3PXP"
	root := model.User{Username: "proof-root", Password: "unused-hash", AffCode: "prf001", Role: common.RoleRootUser, Status: common.UserStatusEnabled}
	require.NoError(t, db.Create(&root).Error)
	require.NoError(t, db.Create(&model.TwoFA{UserId: root.Id, Secret: secret, IsEnabled: true}).Error)
	h := newStepUpHarness(t, &root)

	_, result := h.do(http.MethodGet, "/verify/methods?scope=channel.key.read", "", "")
	methods := result["data"].(map[string]any)["methods"].([]any)
	require.Len(t, methods, 1, "channel keys accept only enrolled strong factors")
	require.Equal(t, "2fa", methods[0].(map[string]any)["method"])

	_, result = h.do(http.MethodPost, "/verify", `{"method":"password","scope":"channel.key.read","context":{"channel_id":7},"password":"x"}`, "")
	require.Equal(t, "SECURITY_PROOF_METHOD_MISMATCH", result["code"], "password never unlocks channel keys")

	verify := func(at time.Time) string {
		code, err := totp.GenerateCode(secret, at)
		require.NoError(t, err)
		return h.proof(`{"method":"2fa","scope":"channel.key.read","context":{"channel_id":7},"code":"` + code + `"}`)
	}
	proof := verify(time.Now())

	status, result := h.do(http.MethodPost, "/channel/8/key", "", proof)
	require.Equal(t, http.StatusForbidden, status)
	require.Equal(t, "SECURITY_PROOF_CONTEXT_MISMATCH", result["code"], "a proof for channel 7 cannot read channel 8")

	_, result = h.do(http.MethodPost, "/channel/7/key", "", proof)
	require.Equal(t, true, result["success"], result)
	status, result = h.do(http.MethodPost, "/channel/7/key", "", proof)
	require.Equal(t, http.StatusForbidden, status)
	require.Equal(t, "SECURITY_PROOF_CONSUMED", result["code"])

	// The same TOTP window cannot be reused, so take the next window's code.
	second := verify(time.Now().Add(30 * time.Second))
	other := newStepUpHarness(t, &root)
	status, result = other.do(http.MethodPost, "/channel/7/key", "", second)
	require.Equal(t, http.StatusForbidden, status)
	require.Equal(t, "SECURITY_PROOF_INVALID", result["code"], "a proof is bound to the browser session that obtained it")
}

func TestRecentSignInConfirmsAccountsWithoutFactors(t *testing.T) {
	db := useStepUpTestDB(t)
	oauthOnly := model.User{Username: "proof-oauth", AffCode: "prf002", Role: common.RoleCommonUser, Status: common.UserStatusEnabled, GitHubId: "42"}
	require.NoError(t, db.Create(&oauthOnly).Error)
	require.NoError(t, db.Model(&oauthOnly).Update("password", "").Error)
	h := newStepUpHarness(t, &oauthOnly)

	_, result := h.do(http.MethodGet, "/verify/methods?scope=account.delete", "", "")
	methods := result["data"].(map[string]any)["methods"].([]any)
	require.Len(t, methods, 1)
	require.Equal(t, "session", methods[0].(map[string]any)["method"])
	require.Equal(t, true, methods[0].(map[string]any)["available"])

	_, result = h.do(http.MethodGet, "/verify/methods?scope=channel.key.read", "", "")
	require.Equal(t, "SECURITY_ACTION_FORBIDDEN", result["code"], "only root may read channel keys")

	h.do(http.MethodGet, "/age-login", "", "")
	_, result = h.do(http.MethodPost, "/verify", `{"method":"session","scope":"account.delete"}`, "")
	require.Equal(t, "REAUTH_REQUIRED", result["code"], "an old sign-in cannot confirm sensitive actions")

	h.do(http.MethodGet, "/login", "", "")
	proof := h.proof(`{"method":"session","scope":"account.delete"}`)
	_, result = h.do(http.MethodDelete, "/self", "", proof)
	require.Equal(t, true, result["success"], result)
}

func TestRootCannotDeleteItself(t *testing.T) {
	db := useStepUpTestDB(t)
	root := model.User{Username: "proof-root-self", Password: "hash", AffCode: "prf003", Role: common.RoleRootUser, Status: common.UserStatusEnabled}
	require.NoError(t, db.Create(&root).Error)
	h := newStepUpHarness(t, &root)
	_, result := h.do(http.MethodDelete, "/self", "", "")
	require.Equal(t, false, result["success"])
	require.NotEqual(t, "SECURITY_PROOF_REQUIRED", result["code"])
	_, result = h.do(http.MethodGet, "/verify/methods?scope=account.delete", "", "")
	require.Equal(t, "SECURITY_ACTION_FORBIDDEN", result["code"])
	require.Equal(t, strconv.Itoa(root.Id), strconv.Itoa(h.userID))
}
