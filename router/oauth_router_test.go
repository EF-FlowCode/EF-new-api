package router

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"eflow.local/new-api/common"
	"eflow.local/new-api/i18n"
	"eflow.local/new-api/oauth"
	"github.com/alicebob/miniredis/v2"
	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStandardOAuthProvidersUseUnifiedCallbackRoute(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	SetApiRouter(engine)

	var callbackHandler string
	for _, route := range engine.Routes() {
		if route.Method == http.MethodGet && route.Path == "/api/oauth/:provider" {
			callbackHandler = route.Handler
			break
		}
	}
	require.NotEmpty(t, callbackHandler)
	assert.True(t, strings.HasSuffix(callbackHandler, ".HandleOAuth"), callbackHandler)

	tests := []struct {
		name   string
		prefix string
		column string
	}{
		{name: "github", prefix: "github_", column: "github_id"},
		{name: "discord", prefix: "discord_", column: "discord_id"},
		{name: "oidc", prefix: "oidc_", column: "oidc_id"},
		{name: "linuxdo", prefix: "linuxdo_", column: "linux_do_id"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			provider := oauth.GetProvider(test.name)
			require.NotNil(t, provider)
			assert.Equal(t, test.prefix, provider.GetProviderPrefix())
			assert.Equal(t, test.column, provider.ProviderUserIDColumn())
		})
	}
}

func TestOAuthRoutesWorkAfterSharedCriticalLimitIsExhausted(t *testing.T) {
	gin.SetMode(gin.TestMode)
	require.NoError(t, i18n.Init())
	originalRedis, originalRedisEnabled := common.RDB, common.RedisEnabled
	originalCritical, originalNum, originalDuration := common.CriticalRateLimitEnable, common.CriticalRateLimitNum, common.CriticalRateLimitDuration
	originalGlobal := common.GlobalApiRateLimitEnable
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	common.RDB, common.RedisEnabled = client, true
	common.CriticalRateLimitEnable, common.CriticalRateLimitNum, common.CriticalRateLimitDuration = true, 1, 1200
	common.GlobalApiRateLimitEnable = false
	t.Cleanup(func() {
		common.RDB, common.RedisEnabled = originalRedis, originalRedisEnabled
		common.CriticalRateLimitEnable, common.CriticalRateLimitNum, common.CriticalRateLimitDuration = originalCritical, originalNum, originalDuration
		common.GlobalApiRateLimitEnable = originalGlobal
		_ = client.Close()
	})

	engine := gin.New()
	engine.Use(sessions.Sessions("session", cookie.NewStore([]byte("oauth-route-rate-limit-test-key"))))
	SetApiRouter(engine)
	request := func(method, path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader("{}"))
		req.Header.Set("Content-Type", "application/json")
		req.RemoteAddr = "192.0.2.230:12345"
		res := httptest.NewRecorder()
		engine.ServeHTTP(res, req)
		return res
	}

	// Empty credentials are rejected without accessing a user database, but
	// still consume the shared critical bucket used by the old OAuth routes.
	require.Equal(t, http.StatusOK, request(http.MethodPost, "/api/user/login").Code)
	for _, path := range []string{"/api/user/login", "/api/user/register", "/api/user/login/2fa"} {
		t.Run("protected_"+path, func(t *testing.T) {
			res := request(http.MethodPost, path)
			require.Equal(t, http.StatusTooManyRequests, res.Code)
			require.NotEmpty(t, res.Header().Get("Retry-After"))
		})
	}
	for range 3 {
		res := request(http.MethodGet, "/api/oauth/state")
		require.Equal(t, http.StatusOK, res.Code, "OAuth state must not share the exhausted login bucket")
		var result struct {
			Success bool   `json:"success"`
			Data    string `json:"data"`
		}
		require.NoError(t, json.Unmarshal(res.Body.Bytes(), &result))
		require.True(t, result.Success)
		require.NotEmpty(t, result.Data)
		require.NotEmpty(t, res.Result().Cookies(), "state must still be stored in the session")
	}
	for _, provider := range []string{"github", "discord", "oidc", "linuxdo"} {
		t.Run("callback_"+provider, func(t *testing.T) {
			res := request(http.MethodGet, "/api/oauth/"+provider+"?state=invalid")
			// Reaching the CSRF check proves the callback is not blocked by the
			// exhausted bucket. No token exchange or third-party request is made.
			require.Equal(t, http.StatusForbidden, res.Code)
			require.Contains(t, res.Body.String(), `"success":false`)
		})
	}
	// Requests without an email cannot send mail, but still exercise the
	// independent verification-code limiter through the production route.
	for range 2 {
		require.NotEqual(t, http.StatusTooManyRequests, request(http.MethodGet, "/api/verification").Code)
	}
	require.Equal(t, http.StatusTooManyRequests, request(http.MethodGet, "/api/verification").Code)
}

func TestAccountBindingRoutesRequireLogin(t *testing.T) {
	gin.SetMode(gin.TestMode)
	require.NoError(t, i18n.Init())
	originalCritical, originalGlobal := common.CriticalRateLimitEnable, common.GlobalApiRateLimitEnable
	common.CriticalRateLimitEnable, common.GlobalApiRateLimitEnable = false, false
	t.Cleanup(func() {
		common.CriticalRateLimitEnable, common.GlobalApiRateLimitEnable = originalCritical, originalGlobal
	})

	engine := gin.New()
	engine.Use(sessions.Sessions("session", cookie.NewStore([]byte("binding-route-auth-test-key"))))
	SetApiRouter(engine)
	for _, route := range []struct{ method, path string }{
		{http.MethodPost, "/api/oauth/wechat/bind"},
		{http.MethodPost, "/api/oauth/telegram/bind/start"},
		{http.MethodGet, "/api/oauth/telegram/bind?id=1&hash=00&auth_date=1"},
	} {
		t.Run(route.method+" "+route.path, func(t *testing.T) {
			req := httptest.NewRequest(route.method, route.path, strings.NewReader(`{"code":"x"}`))
			req.Header.Set("Content-Type", "application/json")
			res := httptest.NewRecorder()
			engine.ServeHTTP(res, req)
			// Anonymous callers are rejected by auth middleware instead of
			// reaching the handler (previously a nil session id panicked).
			require.Equal(t, http.StatusUnauthorized, res.Code, res.Body.String())
		})
	}
}
