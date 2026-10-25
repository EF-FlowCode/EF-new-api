package controller

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"eflow.local/new-api/common"
	"eflow.local/new-api/i18n"
	"eflow.local/new-api/model"
	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestOAuthStateIsSingleUse(t *testing.T) {
	gin.SetMode(gin.TestMode)
	require.NoError(t, i18n.Init())
	engine := gin.New()
	engine.Use(sessions.Sessions("session", cookie.NewStore([]byte("oauth-state-single-use"))))
	engine.GET("/state", GenerateOAuthCode)
	engine.GET("/callback", func(c *gin.Context) {
		if consumeOAuthState(c, sessions.Default(c)) {
			c.JSON(http.StatusOK, gin.H{"success": true})
		}
	})
	var cookies []*http.Cookie
	do := func(target string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, target, nil)
		for _, cookie := range cookies {
			req.AddCookie(cookie)
		}
		res := httptest.NewRecorder()
		engine.ServeHTTP(res, req)
		if next := res.Result().Cookies(); len(next) > 0 {
			cookies = next
		}
		return res
	}

	var issued struct{ Data string }
	require.NoError(t, json.Unmarshal(do("/state").Body.Bytes(), &issued))
	require.NotEmpty(t, issued.Data)
	require.Equal(t, http.StatusOK, do("/callback?state="+issued.Data).Code)
	require.Equal(t, http.StatusForbidden, do("/callback?state="+issued.Data).Code, "replayed state must be rejected")
}

func TestOAuthStateIsBurnedByMismatchedCallback(t *testing.T) {
	gin.SetMode(gin.TestMode)
	require.NoError(t, i18n.Init())
	engine := gin.New()
	engine.Use(sessions.Sessions("session", cookie.NewStore([]byte("oauth-state-burned"))))
	engine.GET("/state", GenerateOAuthCode)
	engine.GET("/callback", func(c *gin.Context) {
		if consumeOAuthState(c, sessions.Default(c)) {
			c.JSON(http.StatusOK, gin.H{"success": true})
		}
	})
	var cookies []*http.Cookie
	do := func(target string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, target, nil)
		for _, cookie := range cookies {
			req.AddCookie(cookie)
		}
		res := httptest.NewRecorder()
		engine.ServeHTTP(res, req)
		if next := res.Result().Cookies(); len(next) > 0 {
			cookies = next
		}
		return res
	}

	var issued struct{ Data string }
	require.NoError(t, json.Unmarshal(do("/state").Body.Bytes(), &issued))
	require.Equal(t, http.StatusForbidden, do("/callback?state=guess").Code)
	require.Equal(t, http.StatusForbidden, do("/callback?state="+issued.Data).Code, "a probed state must not stay usable")
}

func TestEmailBindConsumesVerificationCode(t *testing.T) {
	db := useAccountSecurityTestDB(t)
	user := model.User{Username: "bind-email", Password: "password-hash", AffCode: "bnde01"}
	require.NoError(t, db.Create(&user).Error)
	email, code := "bind@example.com", "123456"
	require.NoError(t, common.RegisterVerificationCodeWithKey(email, code, common.EmailBindVerificationPurpose(user.Id)))

	body := `{"email":"` + email + `","code":"` + code + `"}`
	asUser := func(c *gin.Context) { c.Set("id", user.Id) }
	_, result := callAccountHandler(t, EmailBind, http.MethodPost, body, asUser)
	require.Equal(t, true, result["success"], result)
	_, result = callAccountHandler(t, EmailBind, http.MethodPost, body, asUser)
	require.Equal(t, false, result["success"], "a consumed code cannot bind again")
}

func TestEmailBindCodeIsScopedToRequestingUser(t *testing.T) {
	db := useAccountSecurityTestDB(t)
	owner := model.User{Username: "bind-owner", Password: "password-hash", AffCode: "bndo01"}
	other := model.User{Username: "bind-other", Password: "password-hash", AffCode: "bndo02"}
	require.NoError(t, db.Create(&owner).Error)
	require.NoError(t, db.Create(&other).Error)
	email, code := "scoped@example.com", "654321"
	require.NoError(t, common.RegisterVerificationCodeWithKey(email, code, common.EmailBindVerificationPurpose(owner.Id)))

	body := `{"email":"` + email + `","code":"` + code + `"}`
	_, result := callAccountHandler(t, EmailBind, http.MethodPost, body, func(c *gin.Context) { c.Set("id", other.Id) })
	require.Equal(t, false, result["success"], "a code sent for one account cannot bind another")
}

func TestIsHTTPSRequestHonorsForwardedProto(t *testing.T) {
	for proto, want := range map[string]bool{"": false, "http": false, "https": true, "HTTPS, http": true} {
		ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
		ctx.Request = httptest.NewRequest(http.MethodGet, "/", strings.NewReader(""))
		if proto != "" {
			ctx.Request.Header.Set("X-Forwarded-Proto", proto)
		}
		require.Equal(t, want, isHTTPSRequest(ctx), proto)
	}
}
