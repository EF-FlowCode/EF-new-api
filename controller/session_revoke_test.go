package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"eflow.local/new-api/common"
	"github.com/alicebob/miniredis/v2"
	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/require"
)

func TestRevokeUserSessionsKeepsCurrentDeviceOnly(t *testing.T) {
	gin.SetMode(gin.TestMode)
	originalRedis, originalRedisEnabled := common.RDB, common.RedisEnabled
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	common.RDB, common.RedisEnabled = client, true
	t.Cleanup(func() {
		common.RDB, common.RedisEnabled = originalRedis, originalRedisEnabled
		_ = client.Close()
	})

	engine := gin.New()
	engine.Use(sessions.Sessions("session", common.NewSessionStore(sessions.Options{Path: "/", MaxAge: 3600})))
	engine.GET("/login", func(c *gin.Context) {
		session := sessions.Default(c)
		session.Set("id", 7)
		require.NoError(t, session.Save())
	})
	engine.GET("/whoami", func(c *gin.Context) {
		id, _ := sessions.Default(c).Get("id").(int)
		c.JSON(http.StatusOK, gin.H{"id": id})
	})
	engine.GET("/change-password", func(c *gin.Context) { revokeUserSessions(c, 7, true) })
	engine.GET("/reset-password", func(c *gin.Context) { revokeUserSessions(c, 7, false) })
	do := func(path string, cookies []*http.Cookie) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		for _, c := range cookies {
			req.AddCookie(c)
		}
		rec := httptest.NewRecorder()
		engine.ServeHTTP(rec, req)
		return rec
	}

	current := do("/login", nil).Result().Cookies()
	other := do("/login", nil).Result().Cookies()
	do("/change-password", current)
	require.JSONEq(t, `{"id":7}`, do("/whoami", current).Body.String())
	require.JSONEq(t, `{"id":0}`, do("/whoami", other).Body.String(), "other devices are signed out after a password change")

	do("/reset-password", nil)
	require.JSONEq(t, `{"id":0}`, do("/whoami", current).Body.String())
}
