package router

import (
	"archive/zip"
	"bytes"
	"encoding/csv"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"eflow.local/new-api/common"
	"eflow.local/new-api/i18n"
	"eflow.local/new-api/internal/testdb"
	"eflow.local/new-api/model"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// eflow: 导出他人日志只允许超级管理员。锁定三条防线：
// 1) /api/log/export(-async) 挂 RootAuth，普通用户、客服、普通管理员都被拒；
// 2) /api/log/self/export 只按登录身份取 user_id，忽略调用方传入的 user_id；
// 3) root 导出必须显式带合法 user_id。
func TestLogExportOnlyRootCanExportOtherUsers(t *testing.T) {
	gin.SetMode(gin.TestMode)
	require.NoError(t, i18n.Init())
	db := testdb.OpenMySQL8(t, &model.User{}, &model.Log{}, &model.UsageRefundReceipt{})
	oldDB, oldLogDB := model.DB, model.LOG_DB
	oldRedis := common.RedisEnabled
	model.DB, model.LOG_DB = db, db
	common.RedisEnabled = false
	t.Cleanup(func() {
		model.DB, model.LOG_DB = oldDB, oldLogDB
		common.RedisEnabled = oldRedis
	})

	users := map[string]*model.User{
		"alice":   {Username: "alice", Role: common.RoleCommonUser, AffCode: "exp-alice"},
		"bob":     {Username: "bob", Role: common.RoleCommonUser, AffCode: "exp-bob"},
		"support": {Username: "support", Role: common.RoleCustomerServiceUser, AffCode: "exp-support"},
		"admin":   {Username: "admin", Role: common.RoleAdminUser, AffCode: "exp-admin"},
		"root":    {Username: "root", Role: common.RoleRootUser, AffCode: "exp-root"},
	}
	for _, u := range users {
		u.Status = common.UserStatusEnabled
		require.NoError(t, db.Create(u).Error)
	}
	for _, name := range []string{"alice", "bob"} {
		u := users[name]
		require.NoError(t, db.Create(&model.Log{UserId: u.Id, Username: u.Username, Type: model.LogTypeConsume, ModelName: "model-of-" + name, Quota: 1, CreatedAt: 100}).Error)
	}

	r := gin.New()
	store := cookie.NewStore([]byte("log-export-permission-test"))
	r.Use(sessions.Sessions("session", store))
	// 测试专用登录：把指定用户写入 session，模拟正常登录后的 cookie。
	r.GET("/test-login/:id", func(c *gin.Context) {
		id, _ := strconv.Atoi(c.Param("id"))
		session := sessions.Default(c)
		session.Set("id", id)
		require.NoError(t, session.Save())
	})
	SetApiRouter(r)

	login := func(u *model.User) *http.Cookie {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/test-login/"+strconv.Itoa(u.Id), nil))
		cookies := w.Result().Cookies()
		require.NotEmpty(t, cookies)
		return cookies[0]
	}
	get := func(u *model.User, path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.AddCookie(login(u))
		req.Header.Set("New-Api-User", strconv.Itoa(u.Id))
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}
	exportedModels := func(t *testing.T, w *httptest.ResponseRecorder) []string {
		t.Helper()
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		require.Equal(t, "application/zip", w.Header().Get("Content-Type"), w.Body.String())
		zr, err := zip.NewReader(bytes.NewReader(w.Body.Bytes()), int64(w.Body.Len()))
		require.NoError(t, err)
		f, err := zr.Open("使用明细.csv")
		require.NoError(t, err)
		defer f.Close()
		raw, err := io.ReadAll(f)
		require.NoError(t, err)
		rows, err := csv.NewReader(bytes.NewReader(bytes.TrimPrefix(raw, []byte{0xEF, 0xBB, 0xBF}))).ReadAll()
		require.NoError(t, err)
		var models []string
		for _, row := range rows[1:] {
			for _, cell := range row {
				if len(cell) > 9 && cell[:9] == "model-of-" {
					models = append(models, cell)
				}
			}
		}
		return models
	}
	bob := users["bob"]
	bobQuery := "?user_id=" + strconv.Itoa(bob.Id)

	t.Run("everyone but root is refused by export of others", func(t *testing.T) {
		for _, name := range []string{"alice", "support", "admin"} {
			for _, path := range []string{"/api/log/export", "/api/log/export-async"} {
				w := get(users[name], path+bobQuery)
				require.NotEqual(t, "application/zip", w.Header().Get("Content-Type"), name+path)
				require.Contains(t, w.Body.String(), `"success":false`, name+path)
			}
		}
	})

	t.Run("self export ignores a forged user_id", func(t *testing.T) {
		models := exportedModels(t, get(users["alice"], "/api/log/self/export"+bobQuery))
		require.Equal(t, []string{"model-of-alice"}, models)
	})

	t.Run("regular admin can still export their own logs", func(t *testing.T) {
		require.Empty(t, exportedModels(t, get(users["admin"], "/api/log/self/export"+bobQuery)))
	})

	t.Run("root exports the named user only", func(t *testing.T) {
		models := exportedModels(t, get(users["root"], "/api/log/export"+bobQuery))
		require.Equal(t, []string{"model-of-bob"}, models)
	})

	t.Run("root export requires a valid user_id", func(t *testing.T) {
		for _, query := range []string{"", "?user_id=0", "?user_id=abc", "?user_id=999999"} {
			w := get(users["root"], "/api/log/export"+query)
			require.NotEqual(t, "application/zip", w.Header().Get("Content-Type"), query)
			require.Contains(t, w.Body.String(), `"success":false`, query)
		}
	})
}
