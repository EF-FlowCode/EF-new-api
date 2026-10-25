package taskplugin

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"eflow.local/new-api/common"
	"eflow.local/new-api/internal/testdb"
	"eflow.local/new-api/model"
	"eflow.local/new-api/plugins"
	management "eflow.local/new-api/service/taskplugin"
	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type forbiddenStore struct{ t *testing.T }

func (s forbiddenStore) Read(context.Context) (model.TaskPluginSnapshot, error) {
	s.t.Fatal("unauthenticated request reached storage")
	return model.TaskPluginSnapshot{}, errors.New("forbidden")
}
func (s forbiddenStore) Update(context.Context, func(*model.TaskPluginSnapshot) error) (model.TaskPluginSnapshot, error) {
	s.t.Fatal("unauthenticated request reached storage")
	return model.TaskPluginSnapshot{}, errors.New("forbidden")
}

func TestAllManagementRoutesRequireAuthentication(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(sessions.Sessions("session", cookie.NewStore([]byte("task-plugin-test-session-secret"))))
	RegisterRoutes(r.Group("/api"), management.NewManager(forbiddenStore{t}, nil))
	for _, route := range r.Routes() {
		path := strings.NewReplacer(":key", "test", ":version", "1.0.0").Replace(route.Path)
		recorder := httptest.NewRecorder()
		r.ServeHTTP(recorder, httptest.NewRequest(route.Method, path, strings.NewReader(`{}`)))
		require.Equal(t, http.StatusUnauthorized, recorder.Code, route.Method+" "+path)
		require.Contains(t, recorder.Body.String(), `"success":false`)
	}
}

func TestBoundedJSONRejectsOversizeAndTrailingObjects(t *testing.T) {
	for _, tc := range []struct {
		body   string
		status int
	}{{strings.Repeat(" ", 65), 413}, {`{} {}`, 400}} {
		r := gin.New()
		r.POST("/", func(c *gin.Context) {
			var req management.Settings
			if bind(c, &req, 64) {
				c.Status(204)
			}
		})
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/", strings.NewReader(tc.body)))
		require.Equal(t, tc.status, w.Code)
	}
}

func TestStorageFailureIsServiceUnavailable(t *testing.T) {
	r := gin.New()
	r.GET("/", func(c *gin.Context) { respond(c, nil, management.ErrUnavailable) })
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
	require.Equal(t, http.StatusServiceUnavailable, w.Code)
}

func TestRootManagementMySQL(t *testing.T) {
	db := testdb.OpenMySQL8(t, &model.User{}, &model.Channel{}, &model.Task{}, &model.TaskPlugin{}, &model.TaskPluginControl{}, &model.TaskPluginShadowBinding{}, &model.TaskPluginSourceSnapshot{}, &model.TaskPluginTaskReference{}, &model.TaskPluginProductionBinding{})
	oldDB, oldRedis, oldLimit := model.DB, common.RedisEnabled, common.CriticalRateLimitEnable
	model.DB, common.RedisEnabled, common.CriticalRateLimitEnable = db, false, false
	t.Cleanup(func() { model.DB, common.RedisEnabled, common.CriticalRateLimitEnable = oldDB, oldRedis, oldLimit })
	require.NoError(t, db.Create(&model.TaskPluginControl{Id: 1, Revision: 1, FactoryEnabled: true, DisabledFactory: "[]"}).Error)
	for _, user := range []model.User{
		{Id: 1, Username: "plugin-root", AffCode: "plugin-root", Password: "test", Role: common.RoleRootUser, Status: common.UserStatusEnabled},
		{Id: 2, Username: "plugin-admin", AffCode: "plugin-admin", Password: "test", Role: common.RoleAdminUser, Status: common.UserStatusEnabled},
		{Id: 3, Username: "plugin-user", AffCode: "plugin-user", Password: "test", Role: common.RoleCommonUser, Status: common.UserStatusEnabled},
	} {
		require.NoError(t, db.Create(&user).Error)
	}
	m := management.NewManager(model.SQLTaskPluginStore{DB: func() *gorm.DB { return db }}, nil)
	r := gin.New()
	r.Use(sessions.Sessions("session", cookie.NewStore([]byte("task-plugin-test-session-secret"))))
	// Only session creation is a test fixture; every request traverses the real
	// RootAuth and loads the actual user's current database role and status.
	r.GET("/login/:id", func(c *gin.Context) {
		id, _ := strconv.Atoi(c.Param("id"))
		s := sessions.Default(c)
		s.Set("id", id)
		require.NoError(t, s.Save())
		c.Status(204)
	})
	RegisterRoutes(r.Group("/api"), m)
	sessionCookies := make(map[int][]*http.Cookie)
	request := func(user int, method, path, body string) *httptest.ResponseRecorder {
		if sessionCookies[user] == nil {
			login := httptest.NewRecorder()
			r.ServeHTTP(login, httptest.NewRequest(http.MethodGet, "/login/"+strconv.Itoa(user), nil))
			sessionCookies[user] = login.Result().Cookies()
		}
		req := httptest.NewRequest(method, "/api/plugin/task"+path, strings.NewReader(body))
		req.Header.Set("New-Api-User", strconv.Itoa(user))
		req.Header.Set("Content-Type", "application/json")
		for _, cookie := range sessionCookies[user] {
			req.AddCookie(cookie)
		}
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}
	for _, user := range []int{2, 3} {
		for _, route := range r.Routes() {
			if !strings.HasPrefix(route.Path, "/api/") {
				continue
			}
			path := strings.TrimPrefix(strings.NewReplacer(":key", "test", ":version", "1.0.0").Replace(route.Path), "/api/plugin/task")
			w := request(user, route.Method, path, `{}`)
			require.Contains(t, w.Body.String(), `"success":false`, route.Method+" "+path)
		}
	}
	source := `export const meta={apiVersion:1,key:"test",version:"1.0.0",name:"Test",author:{name:"Test"},models:["test"],fetchMode:"per_task"};
export function buildSubmitRequest(){return {ok:true};}
export function parseSubmitResponse(){return {};}
export function buildQueryRequest(){return {};}
export function parseTaskResult(){return {};}`
	encoded, err := json.Marshal(management.UploadRequest{Source: source})
	require.NoError(t, err)
	w := request(1, http.MethodPost, "", string(encoded))
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), `"success":true`)
	w = request(1, http.MethodGet, "", "")
	require.NotContains(t, w.Body.String(), "export function")
	w = request(1, http.MethodGet, "/test", "")
	require.Contains(t, w.Body.String(), "export function")
	w = request(1, http.MethodPut, "/runtime/settings", `{"enabled":true}`)
	require.Equal(t, 200, w.Code, w.Body.String())
	require.True(t, m.Status().Enabled)
	w = request(1, http.MethodPost, "/test/dryrun", `{"hook":"buildSubmitRequest","args":[]}`)
	require.Contains(t, w.Body.String(), `"ok":true`)
	w = request(1, http.MethodPost, "/test/status", `{"enabled":false}`)
	require.Equal(t, 200, w.Code, w.Body.String())
	w = request(1, http.MethodPost, "/test/activate", `{"version":"1.0.0"}`)
	require.Equal(t, 200, w.Code, w.Body.String())
	w = request(1, http.MethodDelete, "/test/versions/1.0.0", "")
	require.Equal(t, 200, w.Code, w.Body.String())
	// The binding is not a writable Channel field. An ordinary channel save
	// leaves it untouched and a cloned row does not inherit its primary key.
	require.NoError(t, db.Create(&model.Channel{Id: 17, Type: 17, Name: "bound-channel"}).Error)
	alibabaSource := strings.Replace(source, `key:"test"`, `key:"alibaba"`, 1)
	encoded, err = json.Marshal(management.UploadRequest{Source: alibabaSource})
	require.NoError(t, err)
	w = request(1, http.MethodPost, "", string(encoded))
	require.Equal(t, 200, w.Code, w.Body.String())
	w = request(1, http.MethodPut, "/runtime/shadow/bindings/17", `{"key":"alibaba","version":"1.0.0","enabled":true,"sample_every":1}`)
	require.Equal(t, 200, w.Code, w.Body.String())
	w = request(1, http.MethodDelete, "/alibaba/versions/1.0.0", "")
	require.Equal(t, 409, w.Code)
	require.NoError(t, db.Model(&model.Channel{}).Where("id = ?", 17).Update("name", "renamed").Error)
	require.NoError(t, db.Create(&model.Channel{Id: 19, Type: 17, Name: "copied"}).Error)
	var bindings []model.TaskPluginShadowBinding
	require.NoError(t, db.Find(&bindings).Error)
	require.Len(t, bindings, 1)
	require.Equal(t, 17, bindings[0].ChannelID)
	w = request(2, http.MethodPut, "/runtime/shadow/bindings/17", `{"enabled":false}`)
	require.Contains(t, w.Body.String(), `"success":false`)
	w = request(1, http.MethodGet, "/runtime/shadow/bindings", "")
	require.NotContains(t, w.Body.String(), "export function")
	w = request(1, http.MethodDelete, "/runtime/shadow/bindings/17", "")
	require.Equal(t, 200, w.Code, w.Body.String())
	w = request(1, http.MethodDelete, "/alibaba/versions/1.0.0", "")
	require.Equal(t, 200, w.Code, w.Body.String())
	// A historical Task reference protects deletion independently of bindings.
	w = request(1, http.MethodPost, "", string(encoded))
	require.Equal(t, 200, w.Code, w.Body.String())
	_, err = m.CreatePluginTask(t.Context(), model.Task{TaskID: "private-plugin-task", UserId: 1, ChannelId: 17, Status: model.TaskStatusSuccess, PrivateData: model.TaskPrivateData{UpstreamTaskID: "upstream"}}, management.TaskVersion{Key: "alibaba", Version: "1.0.0"})
	require.NoError(t, err)
	w = request(1, http.MethodDelete, "/alibaba/versions/1.0.0", "")
	require.Equal(t, 409, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), "referenced by a task")
	// Happyhorse binding/adoption traverses the same Root-only routes.
	happyhorseSource, err := plugins.Source("happyhorse")
	require.NoError(t, err)
	encoded, err = json.Marshal(management.UploadRequest{Source: happyhorseSource})
	require.NoError(t, err)
	w = request(1, http.MethodPost, "", string(encoded))
	require.Equal(t, 200, w.Code, w.Body.String())
	require.NoError(t, db.Create(&model.Channel{Id: 58, Type: 1001, Name: "happyhorse"}).Error)
	w = request(1, http.MethodPut, "/runtime/production/bindings/58", `{"key":"happyhorse","version":"1.0.0","enabled":true,"go_fallback":true}`)
	require.Equal(t, 400, w.Code, "removed fallback configuration must not silently succeed")
	w = request(1, http.MethodPut, "/runtime/production/bindings/17", `{"key":"happyhorse","version":"1.0.0","enabled":true}`)
	require.NotEqual(t, 200, w.Code, "binding rejects wrong channel type")
	w = request(1, http.MethodPut, "/runtime/production/bindings/58", `{"key":"happyhorse","version":"1.0.0","enabled":true}`)
	require.Equal(t, 200, w.Code, w.Body.String())
	w = request(1, http.MethodDelete, "/happyhorse/versions/1.0.0", "")
	require.Equal(t, 409, w.Code, "production binding protects source")
	legacy := model.Task{TaskID: "legacy-happyhorse", Platform: "1001", ChannelId: 58, UserId: 1, Quota: 500, Status: model.TaskStatusInProgress, Properties: model.Properties{OriginModelName: "happyhorse-1.0-t2v"}, PrivateData: model.TaskPrivateData{UpstreamTaskID: "provider-happyhorse", Key: "never-expose"}}
	require.NoError(t, db.Create(&legacy).Error)
	adoptPath := "/runtime/tasks/" + strconv.FormatInt(legacy.ID, 10) + "/adopt"
	w = request(2, http.MethodPost, adoptPath, "")
	require.Contains(t, w.Body.String(), `"success":false`)
	for range 2 {
		w = request(1, http.MethodPost, adoptPath, "")
		require.Equal(t, 200, w.Code, w.Body.String())
		require.Contains(t, w.Body.String(), `"task_id":"legacy-happyhorse"`)
		require.NotContains(t, w.Body.String(), "never-expose")
	}
	w = request(1, http.MethodDelete, "/runtime/production/bindings/58", "")
	require.Equal(t, 409, w.Code, "adopted task protects binding")
	// Recovery routes traverse real RootAuth and expose only the review DTO.
	require.NoError(t, db.AutoMigrate(&model.TaskPluginSubmission{}, &model.TaskPluginReview{}))
	require.NoError(t, db.Model(&legacy).Updates(map[string]any{"status": model.TaskStatusSubmitUnknown, "revision": 1, "quota": 0}).Error)
	require.NoError(t, db.First(&legacy, legacy.ID).Error)
	legacy.PrivateData.PluginContext = &model.TaskPluginContext{State: map[string]any{"secret": "private-state"}}
	require.NoError(t, db.Save(&legacy).Error)
	require.NoError(t, db.Create(&model.TaskPluginSubmission{TaskID: legacy.ID, ScopeKey: strings.Repeat("a", 64), Fingerprint: strings.Repeat("b", 64), Phase: "sending", ParseInput: "private-prompt", Response: "private-response"}).Error)
	reviewPath := "/runtime/tasks/" + strconv.FormatInt(legacy.ID, 10) + "/review"
	for _, path := range []string{reviewPath, "/runtime/reviews"} {
		w = request(1, http.MethodGet, path, "")
		require.Equal(t, 200, w.Code, w.Body.String())
		require.Contains(t, w.Body.String(), "legacy-happyhorse")
		for _, secret := range []string{"never-expose", "private-prompt", "private-response", "private-state", "export function"} {
			require.NotContains(t, w.Body.String(), secret)
		}
	}
	w = request(1, http.MethodPost, reviewPath+"/preview", `{"revision":0,"action":"refund","reason":"checked","evidence":"provider confirms","confirmed":true}`)
	require.Equal(t, 409, w.Code, w.Body.String())
	w = request(1, http.MethodPost, reviewPath+"/preview", `{"revision":1,"action":"refund","reason":"checked","evidence":"provider confirms","confirmed":true}`)
	require.Equal(t, 200, w.Code, w.Body.String())
	var preview struct {
		Data model.TaskPluginReview `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &preview))
	confirmPath := "/runtime/reviews/" + preview.Data.OperationID + "/confirm"
	w = request(2, http.MethodPost, confirmPath, "")
	require.Contains(t, w.Body.String(), `"success":false`)
	for range 2 {
		w = request(1, http.MethodPost, confirmPath, "")
		require.Equal(t, 200, w.Code, w.Body.String())
		require.Contains(t, w.Body.String(), `"status":"FAILURE"`)
	}
	w = request(1, http.MethodPost, reviewPath+"/preview", `{"revision":1,"action":"refund","reason":"checked","evidence":"provider confirms","confirmed":true}`)
	require.Equal(t, 409, w.Code, w.Body.String())
	// An existing signed session must lose access when its database role changes.
	require.NoError(t, db.Model(&model.User{}).Where("id = ?", 1).Update("role", common.RoleAdminUser).Error)
	w = request(1, http.MethodGet, "/runtime/status", "")
	require.Contains(t, w.Body.String(), `"success":false`)
}
