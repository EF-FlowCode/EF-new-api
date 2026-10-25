package router

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"eflow.local/new-api/common"
	"eflow.local/new-api/i18n"
	"eflow.local/new-api/internal/testdb"
	"eflow.local/new-api/middleware"
	"eflow.local/new-api/model"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func registeredRouteKeys(r *gin.Engine) map[string]bool {
	keys := map[string]bool{}
	for _, route := range r.Routes() {
		keys[route.Method+" "+route.Path] = true
	}
	return keys
}

// Every dedicated audit action must name a route that exists, otherwise the
// event silently degrades to "generic".
func TestAuditRouteActionsReferenceRegisteredRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	SetApiRouter(r)
	registered := registeredRouteKeys(r)
	for _, key := range middleware.AuditRouteActionKeys() {
		require.True(t, registered[key], "audit action route %q is not registered", key)
	}
}

func TestStaffWriteRequestsAreAudited(t *testing.T) {
	gin.SetMode(gin.TestMode)
	require.NoError(t, i18n.Init())
	db := testdb.OpenMySQL8(t, &model.User{}, &model.AuditLog{})
	oldDB, oldLogDB := model.DB, model.LOG_DB
	oldRedis := common.RedisEnabled
	model.DB, model.LOG_DB = db, db
	common.RedisEnabled = false
	t.Cleanup(func() {
		model.DB, model.LOG_DB = oldDB, oldLogDB
		common.RedisEnabled = oldRedis
	})

	admin := &model.User{Username: "audit-admin", Role: common.RoleAdminUser, Status: common.UserStatusEnabled, AffCode: "audit-admin"}
	member := &model.User{Username: "audit-member", Role: common.RoleCommonUser, Status: common.UserStatusEnabled, AffCode: "audit-member"}
	require.NoError(t, db.Create(admin).Error)
	require.NoError(t, db.Create(member).Error)

	r := gin.New()
	r.Use(sessions.Sessions("session", cookie.NewStore([]byte("audit-routes-test"))))
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
		return w.Result().Cookies()[0]
	}
	do := func(u *model.User, method, path, body string) {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("New-Api-User", strconv.Itoa(u.Id))
		req.AddCookie(login(u))
		r.ServeHTTP(httptest.NewRecorder(), req)
	}

	// A staff read is not audited; a staff write is, with route params only.
	do(admin, http.MethodGet, "/api/user/"+strconv.Itoa(member.Id), "")
	do(admin, http.MethodPost, "/api/user/manage", `{"id":987654321,"action":"disable"}`)
	// Stacked AdminAuth + RootAuth audits once, even when the inner check rejects.
	do(admin, http.MethodPost, "/api/channel/fetch_models", `{}`)
	// A common user's own writes do not go through the staff fallback audit.
	do(member, http.MethodPut, "/api/user/setting", `{}`)

	var rows []model.AuditLog
	require.NoError(t, db.Order("id").Find(&rows).Error)
	require.Len(t, rows, 2)
	stacked := rows[1]
	require.Equal(t, "/api/channel/fetch_models", stacked.Route)
	require.False(t, stacked.Success)
	row := rows[0]
	require.Equal(t, admin.Id, row.UserId)
	require.Equal(t, common.RoleAdminUser, row.ActorRole)
	require.Equal(t, model.AuditCategoryOperation, row.Category)
	require.Equal(t, "user.manage", row.Action)
	require.Equal(t, "/api/user/manage", row.Route)
	require.False(t, row.Success, "managing a missing user is recorded as a failure")
	require.NotContains(t, row.Content, "987654321", "request bodies are never stored")
	require.NotContains(t, must(row.Other.Value()).(string), "987654321")
}

func must[T any](value T, err error) T {
	if err != nil {
		panic(err)
	}
	return value
}

// A request whose main session is gone clears the sub-site identity cookie, so
// sub-sites stop trusting a JWT issued for a revoked session.
func TestMissingSessionClearsSubSiteCookie(t *testing.T) {
	gin.SetMode(gin.TestMode)
	require.NoError(t, i18n.Init())
	r := gin.New()
	r.Use(sessions.Sessions("session", cookie.NewStore([]byte("eflow-auth-clear"))))
	SetApiRouter(r)
	req := httptest.NewRequest(http.MethodGet, "/api/user/self", nil)
	req.AddCookie(&http.Cookie{Name: common.EflowAuthCookieName, Value: "stale"})
	req.Header.Set("New-Api-User", "1")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	require.Equal(t, http.StatusUnauthorized, rec.Code)
	var cleared bool
	for _, c := range rec.Result().Cookies() {
		if c.Name == common.EflowAuthCookieName && c.MaxAge < 0 {
			cleared = true
		}
	}
	require.True(t, cleared, "the stale e-flowcode-auth cookie is expired")
}
