package common

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
	"github.com/gorilla/securecookie"
	"github.com/stretchr/testify/require"
)

const sessionStoreTestSecret = "session-store-test-secret"

type sessionHarness struct {
	t      *testing.T
	engine *gin.Engine
	redis  *miniredis.Miniredis
}

func newSessionHarness(t *testing.T) *sessionHarness {
	t.Helper()
	gin.SetMode(gin.TestMode)
	originalRedis, originalRedisEnabled, originalSecret := RDB, RedisEnabled, SessionSecret
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	RDB, RedisEnabled, SessionSecret = client, true, sessionStoreTestSecret
	t.Cleanup(func() {
		RDB, RedisEnabled, SessionSecret = originalRedis, originalRedisEnabled, originalSecret
		_ = client.Close()
	})

	store := NewSessionStore(sessions.Options{Path: "/", MaxAge: 2592000, HttpOnly: true})
	_, isRedisStore := store.(*redisSessionStore)
	require.True(t, isRedisStore)

	engine := gin.New()
	engine.Use(sessions.Sessions("session", store))
	engine.GET("/login/:id", func(c *gin.Context) {
		session := sessions.Default(c)
		require.NoError(t, RegenerateSession(session))
		session.Set("id", map[string]int{"1": 1, "2": 2}[c.Param("id")])
		require.NoError(t, session.Save())
		c.String(http.StatusOK, CurrentSessionID(session))
	})
	engine.GET("/whoami", func(c *gin.Context) {
		id, _ := sessions.Default(c).Get("id").(int)
		c.JSON(http.StatusOK, gin.H{"id": id})
	})
	engine.GET("/logout", func(c *gin.Context) {
		session := sessions.Default(c)
		session.Clear()
		require.NoError(t, session.Save())
	})
	return &sessionHarness{t: t, engine: engine, redis: server}
}

func (h *sessionHarness) do(path string, cookies []*http.Cookie) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	h.engine.ServeHTTP(rec, req)
	return rec
}

func (h *sessionHarness) whoami(cookies []*http.Cookie) string {
	return h.do("/whoami", cookies).Body.String()
}

func legacySessionCookie(t *testing.T, values map[any]any) *http.Cookie {
	t.Helper()
	encoded, err := securecookie.EncodeMulti("session", values, securecookie.CodecsFromPairs([]byte(sessionStoreTestSecret))...)
	require.NoError(t, err)
	return &http.Cookie{Name: "session", Value: encoded}
}

func TestRedisSessionStoreKeepsValuesServerSide(t *testing.T) {
	h := newSessionHarness(t)

	login := h.do("/login/1", nil)
	cookies := login.Result().Cookies()
	require.Len(t, cookies, 1)
	require.True(t, h.redis.Exists(sessionKeyPrefix+login.Body.String()))
	require.Less(t, len(cookies[0].Value), 200, "cookie should carry only the session id")
	require.JSONEq(t, `{"id":1}`, h.whoami(cookies))

	logout := h.do("/logout", cookies)
	require.Equal(t, `{"id":0}`, h.whoami(cookies), "logout must invalidate the server-side session")
	require.Equal(t, -1, logout.Result().Cookies()[0].MaxAge)
}

func TestRedisSessionStoreRegeneratesIDOnLogin(t *testing.T) {
	h := newSessionHarness(t)

	first := h.do("/login/1", nil)
	firstCookies := first.Result().Cookies()
	second := h.do("/login/2", firstCookies)
	require.NotEqual(t, first.Body.String(), second.Body.String())
	require.Equal(t, `{"id":0}`, h.whoami(firstCookies), "the pre-login session id must not stay usable")
	require.JSONEq(t, `{"id":2}`, h.whoami(second.Result().Cookies()))
}

func TestRevokeUserSessionsKeepsOnlyRequestedSession(t *testing.T) {
	h := newSessionHarness(t)

	a := h.do("/login/1", nil)
	b := h.do("/login/1", nil)
	other := h.do("/login/2", nil)

	require.NoError(t, RevokeUserSessions(1, a.Body.String()))
	require.JSONEq(t, `{"id":1}`, h.whoami(a.Result().Cookies()))
	require.Equal(t, `{"id":0}`, h.whoami(b.Result().Cookies()))
	require.JSONEq(t, `{"id":2}`, h.whoami(other.Result().Cookies()))

	require.NoError(t, RevokeUserSessions(1, ""))
	require.Equal(t, `{"id":0}`, h.whoami(a.Result().Cookies()))
}

func TestLegacyCookieSessionMigratesWithoutLogout(t *testing.T) {
	h := newSessionHarness(t)
	legacy := legacySessionCookie(t, map[any]any{"id": 1, "username": "alice"})

	require.JSONEq(t, `{"id":1}`, h.whoami([]*http.Cookie{legacy}))
	require.True(t, h.redis.Exists(sessionKeyPrefix+legacySessionID(legacy.Value)))
	require.JSONEq(t, `{"id":1}`, h.whoami([]*http.Cookie{legacy}))

	// 迁移后的旧 cookie 也纳入用户索引，可以被统一踢下线。
	require.NoError(t, RevokeUserSessions(1, ""))
	require.Equal(t, `{"id":0}`, h.whoami([]*http.Cookie{legacy}))
}

func TestLegacyCookieCannotRemigrateAfterLogout(t *testing.T) {
	h := newSessionHarness(t)
	legacy := legacySessionCookie(t, map[any]any{"id": 1})

	h.do("/logout", []*http.Cookie{legacy})
	require.Equal(t, `{"id":0}`, h.whoami([]*http.Cookie{legacy}), "a copied legacy cookie must stay dead after logout")
}

func TestLegacyCookieRejectedAfterUserRevocation(t *testing.T) {
	h := newSessionHarness(t)
	legacy := legacySessionCookie(t, map[any]any{"id": 1})

	require.NoError(t, RevokeUserSessions(1, ""))
	require.Equal(t, `{"id":0}`, h.whoami([]*http.Cookie{legacy}))
	require.False(t, h.redis.Exists(sessionKeyPrefix+legacySessionID(legacy.Value)))
}

func TestTamperedSessionCookieIsIgnored(t *testing.T) {
	h := newSessionHarness(t)
	forged, err := securecookie.EncodeMulti("session", map[any]any{"id": 1}, securecookie.CodecsFromPairs([]byte("other-secret"))...)
	require.NoError(t, err)

	require.Equal(t, `{"id":0}`, h.whoami([]*http.Cookie{{Name: "session", Value: forged}}))
}

func TestNewSessionStoreFallsBackToCookieWithoutRedis(t *testing.T) {
	originalRedis, originalRedisEnabled := RDB, RedisEnabled
	RDB, RedisEnabled = nil, false
	t.Cleanup(func() { RDB, RedisEnabled = originalRedis, originalRedisEnabled })

	store := NewSessionStore(sessions.Options{Path: "/", MaxAge: 60})
	_, isRedisStore := store.(*redisSessionStore)
	require.False(t, isRedisStore)
	require.NoError(t, RevokeUserSessions(1, ""))
}

func TestListAndRevokeUserSessions(t *testing.T) {
	h := newSessionHarness(t)
	first := httptest.NewRecorder()
	h.engine.ServeHTTP(first, httptest.NewRequest(http.MethodGet, "/login/1", nil))
	second := httptest.NewRecorder()
	h.engine.ServeHTTP(second, httptest.NewRequest(http.MethodGet, "/login/1", nil))
	other := httptest.NewRecorder()
	h.engine.ServeHTTP(other, httptest.NewRequest(http.MethodGet, "/login/2", nil))
	firstID, secondID := first.Body.String(), second.Body.String()

	TouchSession(firstID, "203.0.113.5", time.Hour)
	require.Eventually(t, func() bool { return h.redis.Exists(sessionActiveKeyPrefix + firstID) }, time.Second, 10*time.Millisecond)

	records, err := ListUserSessions(1)
	require.NoError(t, err)
	require.Len(t, records, 2, "only the user's own sessions are listed")
	for _, record := range records {
		if record.ID == firstID {
			require.Equal(t, "203.0.113.5", record.LastActiveIP)
		}
	}
	require.Len(t, SessionPublicID(firstID), 26)
	require.NotEqual(t, firstID, SessionPublicID(firstID))

	revoked, err := RevokeUserSession(2, firstID)
	require.NoError(t, err)
	require.False(t, revoked, "a user cannot revoke another user's session")
	revoked, err = RevokeUserSession(1, firstID)
	require.NoError(t, err)
	require.True(t, revoked)
	require.False(t, h.redis.Exists(sessionActiveKeyPrefix+firstID))
	records, err = ListUserSessions(1)
	require.NoError(t, err)
	require.Len(t, records, 1)
	require.Equal(t, secondID, records[0].ID)
}
