package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"eflow.local/new-api/common"
	"eflow.local/new-api/i18n"
	"eflow.local/new-api/internal/testdb"
	"eflow.local/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestTokenAuthReadOnlyEnforcesIpAllowlist(t *testing.T) {
	gin.SetMode(gin.TestMode)
	require.NoError(t, i18n.Init())
	db := testdb.OpenMySQL8(t, &model.User{}, &model.Token{})
	previous, previousRedis := model.DB, common.RedisEnabled
	model.DB, common.RedisEnabled = db, false
	t.Cleanup(func() { model.DB, common.RedisEnabled = previous, previousRedis })

	user := model.User{Username: "ro-ip", Password: "password-hash", AffCode: "roip01", Status: common.UserStatusEnabled}
	require.NoError(t, db.Create(&user).Error)
	allow := "10.1.2.0/24"
	token := model.Token{UserId: user.Id, Key: "readonlyipallowlisttestkey000001", Name: "ro", Status: common.TokenStatusEnabled, AllowIps: &allow}
	require.NoError(t, db.Create(&token).Error)

	engine := gin.New()
	engine.GET("/usage", TokenAuthReadOnly(), func(c *gin.Context) { c.Status(http.StatusOK) })
	call := func(remote string) int {
		req := httptest.NewRequest(http.MethodGet, "/usage", nil)
		req.RemoteAddr = remote + ":12345"
		req.Header.Set("Authorization", "Bearer sk-"+token.Key)
		rec := httptest.NewRecorder()
		engine.ServeHTTP(rec, req)
		return rec.Code
	}
	require.Equal(t, http.StatusOK, call("10.1.2.3"))
	require.Equal(t, http.StatusForbidden, call("203.0.113.9"))
}
