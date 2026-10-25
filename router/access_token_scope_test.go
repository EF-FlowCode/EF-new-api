package router

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"

	"eflow.local/new-api/common"
	"eflow.local/new-api/i18n"
	"eflow.local/new-api/internal/testdb"
	"eflow.local/new-api/middleware"
	"eflow.local/new-api/model"
	"eflow.local/new-api/service"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// conditionallyAuthenticated routes only authenticate when a header-nav module
// requires sign-in, so the probe may not see them in the default settings.
var conditionallyAuthenticated = []string{
	"GET /api/pricing",
	"GET /api/perf-metrics",
	"GET /api/perf-metrics/summary",
}

var routeParam = regexp.MustCompile(`[:*][A-Za-z_]+`)

func newRouteCoverageEngine() *gin.Engine {
	r := gin.New()
	r.Use(sessions.Sessions("session", cookie.NewStore([]byte("route-coverage"))))
	SetApiRouter(r)
	SetDashboardRouter(r)
	SetRelayRouter(r)
	SetVideoRouter(r)
	return r
}

// Every dashboard route an access token can reach must declare its token rule,
// and every declared rule must belong to a route that authenticates.
func TestAccessTokenRouteRulesCoverEveryDashboardRoute(t *testing.T) {
	gin.SetMode(gin.TestMode)
	require.NoError(t, i18n.Init())
	db := testdb.OpenMySQL8(t, &model.User{}, &model.UserAccessToken{}, &model.AuditLog{}, &model.Option{})
	oldDB, oldLogDB, oldRedis := model.DB, model.LOG_DB, common.RedisEnabled
	model.DB, model.LOG_DB, common.RedisEnabled = db, db, false
	t.Cleanup(func() { model.DB, model.LOG_DB, common.RedisEnabled = oldDB, oldLogDB, oldRedis })

	root := model.User{Username: "coverage-root", Password: "hash", AffCode: "cvrt01", Role: common.RoleRootUser, Status: common.UserStatusEnabled}
	require.NoError(t, db.Create(&root).Error)
	raw := "nap_routecoverage00000000000000000000000000000"
	token := model.UserAccessToken{Name: "coverage", TokenHash: model.AccessTokenFingerprint(raw), TokenHint: "0000"}
	require.NoError(t, token.SetScopes(allCatalogScopes()))
	require.NoError(t, model.CreateUserAccessToken(root.Id, &token, 20))

	reached := map[string]bool{}
	restore := middleware.SetAccessTokenRouteProbeForTest(func(key string, declared bool) bool {
		reached[key] = declared
		return true
	})
	t.Cleanup(restore)

	r := newRouteCoverageEngine()
	registered := map[string]bool{}
	for i, route := range r.Routes() {
		key := route.Method + " " + route.Path
		registered[key] = true
		if !strings.HasPrefix(route.Path, "/api/") && !strings.HasPrefix(route.Path, "/pg/") {
			continue
		}
		path := routeParam.ReplaceAllString(route.Path, "1")
		req := httptest.NewRequest(route.Method, path, strings.NewReader("{}"))
		req.Header.Set("Authorization", "Bearer "+raw)
		req.Header.Set("Content-Type", "application/json")
		req.RemoteAddr = fmt.Sprintf("10.%d.%d.%d:1234", (i>>16)&255, (i>>8)&255, i&255)
		r.ServeHTTP(httptest.NewRecorder(), req)
	}

	// The probe must see the dashboard surface; a broken probe would pass vacuously.
	require.Greater(t, len(reached), 300, "the probe reached too few routes")
	t.Logf("access token rules checked on %d routes", len(reached))

	var undeclared, unreached, missing []string
	for key, declared := range reached {
		if !declared {
			undeclared = append(undeclared, key)
		}
	}
	for _, key := range middleware.AccessTokenRouteRuleKeys() {
		if !registered[key] {
			missing = append(missing, key)
			continue
		}
		if _, ok := reached[key]; !ok && !slices.Contains(conditionallyAuthenticated, key) {
			unreached = append(unreached, key)
		}
	}
	sort.Strings(undeclared)
	sort.Strings(unreached)
	sort.Strings(missing)
	require.Empty(t, undeclared, "authenticated routes without an access token rule")
	require.Empty(t, missing, "access token rules for routes that do not exist")
	require.Empty(t, unreached, "access token rules for routes that never authenticate with a token")
}

// Every scope a route requires must exist in the grantable catalog.
func TestAccessTokenRouteScopesExistInCatalog(t *testing.T) {
	for _, key := range middleware.AccessTokenRouteRuleKeys() {
		rule, _ := middleware.AccessTokenRouteRule(key)
		if rule.Kind() != "scope" {
			continue
		}
		require.True(t, service.KnownAccessTokenScope(rule.Scope()), "%s requires unknown scope %s", key, rule.Scope())
	}
}

func allCatalogScopes() []string {
	var scopes []string
	for _, resource := range service.AccessTokenScopeDictionary() {
		for _, action := range resource.Actions {
			scopes = append(scopes, resource.Resource+":"+action.Action)
		}
	}
	return scopes
}

func TestScopedAccessTokenEnforcement(t *testing.T) {
	gin.SetMode(gin.TestMode)
	require.NoError(t, i18n.Init())
	db := testdb.OpenMySQL8(t, &model.User{}, &model.UserAccessToken{}, &model.AuditLog{}, &model.Option{}, &model.Token{})
	oldDB, oldLogDB, oldRedis := model.DB, model.LOG_DB, common.RedisEnabled
	model.DB, model.LOG_DB, common.RedisEnabled = db, db, false
	t.Cleanup(func() { model.DB, model.LOG_DB, common.RedisEnabled = oldDB, oldLogDB, oldRedis })
	t.Cleanup(model.SetLegacyAccessTokenRetireAtForTest(common.GetTimestamp() + 3600))

	owner := model.User{Username: "pat-owner", Password: "hash", AffCode: "patow1", Role: common.RoleAdminUser, Status: common.UserStatusEnabled}
	require.NoError(t, db.Create(&owner).Error)
	legacy := "legacyaccesstoken000000000000000"
	require.NoError(t, model.SetLegacyUserAccessToken(owner.Id, legacy))
	newToken := func(name string, expiresAt int64, scopes ...string) string {
		raw := "nap_" + name + strings.Repeat("0", 43-len(name))
		token := model.UserAccessToken{Name: name, TokenHash: model.AccessTokenFingerprint(raw), TokenHint: "0000", ExpiresAt: expiresAt}
		require.NoError(t, token.SetScopes(scopes))
		require.NoError(t, model.CreateUserAccessToken(owner.Id, &token, 20))
		return raw
	}
	profile := newToken("profile", 0, "profile:read")
	expired := newToken("expired", 1, "profile:read")
	secret := newToken("secret", 0, "channel:secret_view", "profile:read")

	r := newRouteCoverageEngine()
	call := func(method, path, credential, userHeader string) (int, string, http.Header) {
		req := httptest.NewRequest(method, path, strings.NewReader("{}"))
		req.Header.Set("Authorization", "Bearer "+credential)
		if userHeader != "" {
			req.Header.Set("New-Api-User", userHeader)
		}
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		return rec.Code, rec.Body.String(), rec.Header()
	}
	ownerID := fmt.Sprint(owner.Id)

	status, body, header := call(http.MethodGet, "/api/user/self", profile, "")
	require.Equal(t, http.StatusOK, status, body)
	require.Equal(t, "0", header.Get(middleware.AccessTokenExpiresAtHeader), "never-expiring tokens report 0")
	status, _, _ = call(http.MethodGet, "/api/user/self", profile, ownerID)
	require.Equal(t, http.StatusOK, status)
	_, body, _ = call(http.MethodGet, "/api/user/self", profile, "999999")
	require.Contains(t, body, "user_id_mismatch")

	_, body, _ = call(http.MethodGet, "/api/token/", profile, "")
	require.Contains(t, body, "ACCESS_TOKEN_SCOPE_DENIED")
	require.Contains(t, body, "api_key:read")
	_, body, _ = call(http.MethodPost, "/pg/chat/completions", profile, "")
	require.Contains(t, body, "AUTH_SESSION_REQUIRED")
	_, body, _ = call(http.MethodGet, "/api/user/self", expired, "")
	require.Contains(t, body, "ACCESS_TOKEN_EXPIRED")
	_, body, _ = call(http.MethodGet, "/api/user/self", "nap_doesnotexist0000000000000000000000000000000", "")
	require.Contains(t, body, "access_token_invalid")

	// A scope above the owner's current role grants nothing (owner is admin, secret_view is root-only).
	_, body, _ = call(http.MethodPost, "/api/channel/1/key", secret, "")
	require.NotContains(t, body, `"success":true`)

	// Legacy tokens keep full access during the transition, still need New-Api-User,
	// and are rejected once retired.
	_, body, _ = call(http.MethodGet, "/api/user/self", legacy, "")
	require.Contains(t, body, "user_id_required")
	status, body, _ = call(http.MethodGet, "/api/token/", legacy, ownerID)
	require.Equal(t, http.StatusOK, status, body)
	_, body, _ = call(http.MethodPost, "/pg/chat/completions", legacy, ownerID)
	require.Contains(t, body, "AUTH_SESSION_REQUIRED")
	restore := model.SetLegacyAccessTokenRetireAtForTest(common.GetTimestamp() - 1)
	_, body, _ = call(http.MethodGet, "/api/user/self", legacy, ownerID)
	restore()
	require.Contains(t, body, "ACCESS_TOKEN_LEGACY_RETIRED")

	var audits []model.AuditLog
	require.NoError(t, db.Where("category = ?", model.AuditCategoryAccessToken).Find(&audits).Error)
	require.NotEmpty(t, audits)
	for _, audit := range audits {
		require.NotEmpty(t, audit.TokenRef)
		require.NotContains(t, audit.TokenRef, "nap_", "audits never store the credential")
	}
}

func TestAccessTokenCatalogByRole(t *testing.T) {
	scopesFor := func(role int) []string {
		var scopes []string
		for _, group := range service.AccessTokenCatalog(role) {
			for _, resource := range group.Resources {
				for _, action := range resource.Actions {
					scopes = append(scopes, resource.Resource+":"+action.Action)
				}
			}
		}
		return scopes
	}
	user := scopesFor(common.RoleCommonUser)
	require.Contains(t, user, "api_key:reveal")
	require.Contains(t, user, "ticket:write")
	require.NotContains(t, user, "user:read")

	for _, staff := range []int{common.RoleCustomerServiceUser, common.RoleFinanceUser} {
		scopes := scopesFor(staff)
		require.Contains(t, scopes, "user:read")
		require.Contains(t, scopes, "ticket_admin:write")
		require.Contains(t, scopes, "billing:read")
		require.NotContains(t, scopes, "user:write")
		require.NotContains(t, scopes, "channel:read")
	}

	admin := scopesFor(common.RoleAdminUser)
	require.Contains(t, admin, "user:write")
	require.Contains(t, admin, "channel:sensitive_write")
	require.NotContains(t, admin, "channel:secret_view")
	require.NotContains(t, admin, "option:read")

	root := scopesFor(common.RoleRootUser)
	require.Contains(t, root, "channel:secret_view")
	require.Contains(t, root, "usage_refund:write")

	_, err := service.NormalizeAccessTokenScopes(common.RoleAdminUser, []string{"option:write"})
	require.ErrorIs(t, err, service.ErrAccessTokenScopeForbidden)
	_, err = service.NormalizeAccessTokenScopes(common.RoleAdminUser, []string{"bogus:read"})
	require.ErrorIs(t, err, service.ErrAccessTokenScopeInvalid)
	normalized, err := service.NormalizeAccessTokenScopes(common.RoleAdminUser, []string{"user:write", "profile:read", "user:write"})
	require.NoError(t, err)
	require.Equal(t, []string{"profile:read", "user:write"}, normalized)
}

// admin-api manages users with a scoped token: enabling and disabling needs no
// proof, while other administrative actions still require one bound to the token.
func TestScopedAccessTokenUserManagement(t *testing.T) {
	gin.SetMode(gin.TestMode)
	require.NoError(t, i18n.Init())
	db := testdb.OpenMySQL8(t, &model.User{}, &model.UserAccessToken{}, &model.AuditLog{}, &model.Option{}, &model.Token{}, &model.TwoFA{}, &model.PasskeyCredential{}, &model.Log{})
	oldDB, oldLogDB, oldRedis := model.DB, model.LOG_DB, common.RedisEnabled
	model.DB, model.LOG_DB, common.RedisEnabled = db, db, false
	t.Cleanup(func() { model.DB, model.LOG_DB, common.RedisEnabled = oldDB, oldLogDB, oldRedis })

	hash, err := common.Password2Hash("root-password-1")
	require.NoError(t, err)
	root := model.User{Username: "pat-root", Password: hash, AffCode: "patrt1", Role: common.RoleRootUser, Status: common.UserStatusEnabled}
	target := model.User{Username: "pat-target", Password: "hash", AffCode: "pattg1", Role: common.RoleCommonUser, Status: common.UserStatusEnabled}
	require.NoError(t, db.Create(&root).Error)
	require.NoError(t, db.Create(&target).Error)
	raw := "nap_adminapi000000000000000000000000000000000"
	token := model.UserAccessToken{Name: "admin-api", TokenHash: model.AccessTokenFingerprint(raw), TokenHint: "0000"}
	require.NoError(t, token.SetScopes([]string{"profile:read", "user:read", "user:write"}))
	require.NoError(t, model.CreateUserAccessToken(root.Id, &token, 20))

	r := newRouteCoverageEngine()
	call := func(method, path, body, proof string) string {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+raw)
		req.Header.Set("New-Api-User", fmt.Sprint(root.Id))
		req.Header.Set("Content-Type", "application/json")
		if proof != "" {
			req.Header.Set(middleware.SecurityProofHeader, proof)
		}
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		return rec.Body.String()
	}

	require.Contains(t, call(http.MethodPost, "/api/user/manage", fmt.Sprintf(`{"id":%d,"action":"disable"}`, target.Id), ""), `"success":true`)
	require.Contains(t, call(http.MethodPost, "/api/user/manage", fmt.Sprintf(`{"id":%d,"action":"enable"}`, target.Id), ""), `"success":true`)
	promote := fmt.Sprintf(`{"id":%d,"action":"promote"}`, target.Id)
	require.Contains(t, call(http.MethodPost, "/api/user/manage", promote, ""), "SECURITY_PROOF_REQUIRED")

	// The token obtains a proof bound to itself, with the root password.
	verify := call(http.MethodPost, "/api/verify", fmt.Sprintf(`{"method":"password","scope":"admin.user.manage","context":{"user_id":%d,"action":"promote"},"password":"root-password-1"}`, target.Id), "")
	require.Contains(t, verify, "proof_token", verify)
	proof := regexp.MustCompile(`"proof_token":"([^"]+)"`).FindStringSubmatch(verify)[1]
	require.Contains(t, call(http.MethodPost, "/api/user/manage", promote, proof), `"success":true`)

	// Token management scopes can never be obtained with a token.
	require.Contains(t, call(http.MethodGet, "/api/verify/methods?scope=access_token.generate", "", ""), "SECURITY_ACTION_FORBIDDEN")
}

// channel:write alone cannot change a channel's key, address, type or proxy.
func TestChannelSensitiveFieldsNeedSensitiveScope(t *testing.T) {
	gin.SetMode(gin.TestMode)
	require.NoError(t, i18n.Init())
	db := testdb.OpenMySQL8(t, &model.User{}, &model.UserAccessToken{}, &model.AuditLog{}, &model.Option{}, &model.Channel{}, &model.Ability{})
	oldDB, oldLogDB, oldRedis := model.DB, model.LOG_DB, common.RedisEnabled
	model.DB, model.LOG_DB, common.RedisEnabled = db, db, false
	t.Cleanup(func() { model.DB, model.LOG_DB, common.RedisEnabled = oldDB, oldLogDB, oldRedis })

	root := model.User{Username: "chan-root", Password: "hash", AffCode: "chrt01", Role: common.RoleRootUser, Status: common.UserStatusEnabled}
	require.NoError(t, db.Create(&root).Error)
	baseURL := "https://upstream.example"
	channel := model.Channel{Name: "c", Type: 1, Key: "sk-old", BaseURL: &baseURL, Group: "default", Models: "gpt-x", Status: 1}
	require.NoError(t, db.Create(&channel).Error)
	makeToken := func(name string, scopes ...string) string {
		raw := "nap_" + name + strings.Repeat("0", 43-len(name))
		token := model.UserAccessToken{Name: name, TokenHash: model.AccessTokenFingerprint(raw), TokenHint: "0000"}
		require.NoError(t, token.SetScopes(scopes))
		require.NoError(t, model.CreateUserAccessToken(root.Id, &token, 20))
		return raw
	}
	writeOnly := makeToken("writeonly", "channel:write")
	sensitive := makeToken("sensitive", "channel:write", "channel:sensitive_write")

	r := newRouteCoverageEngine()
	put := func(token, body string) string {
		req := httptest.NewRequest(http.MethodPut, "/api/channel/", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		return rec.Body.String()
	}
	keyChange := fmt.Sprintf(`{"id":%d,"name":"c","type":1,"key":"sk-new","group":"default","models":"gpt-x"}`, channel.Id)
	require.Contains(t, put(writeOnly, keyChange), "channel:sensitive_write")
	require.NotContains(t, put(sensitive, keyChange), "ACCESS_TOKEN_SCOPE_DENIED")
	modelsOnly := fmt.Sprintf(`{"id":%d,"name":"c","type":1,"group":"default","models":"gpt-x,gpt-y"}`, channel.Id)
	require.NotContains(t, put(writeOnly, modelsOnly), "ACCESS_TOKEN_SCOPE_DENIED", "editing models only needs channel:write")
}
