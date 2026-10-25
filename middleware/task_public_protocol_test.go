package middleware

import (
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"eflow.local/new-api/common"
	"eflow.local/new-api/constant"
	"eflow.local/new-api/i18n"
	"eflow.local/new-api/internal/testdb"
	"eflow.local/new-api/model"
	"eflow.local/new-api/plugins"
	"eflow.local/new-api/service"
	management "eflow.local/new-api/service/taskplugin"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestPublicProtocolRoutingAndPermissions(t *testing.T) {
	require.NoError(t, i18n.Init())
	db := testdb.OpenMySQL8(t, &model.Channel{}, &model.Ability{}, &model.TaskPlugin{}, &model.TaskPluginControl{}, &model.TaskPluginShadowBinding{}, &model.TaskPluginProductionBinding{}, &model.TaskPluginSourceSnapshot{})
	oldDB, oldManager, oldCache := model.DB, management.Default, common.MemoryCacheEnabled
	t.Cleanup(func() { model.DB, management.Default, common.MemoryCacheEnabled = oldDB, oldManager, oldCache })
	model.DB = db
	common.MemoryCacheEnabled = false
	source, err := plugins.Source("doubao")
	require.NoError(t, err)
	m := management.NewManager(model.SQLTaskPluginStore{DB: func() *gorm.DB { return db }}, map[string]string{"doubao": source})
	management.Default = m
	require.NoError(t, db.Create(&model.TaskPluginControl{Id: 1, Revision: 1, Enabled: true, FactoryEnabled: true, DisabledFactory: "[]"}).Error)
	mapping := `{"Movie":"doubao-seedance-1-0-lite-t2v"}`
	ch := model.Channel{Id: 54, Type: 54, Key: "test-only", Status: common.ChannelStatusEnabled, Models: "Movie", ModelMapping: &mapping, Group: "default"}
	require.NoError(t, db.Create(&ch).Error)
	require.NoError(t, ch.AddAbilities(db))
	require.NoError(t, m.BindProduction(t.Context(), 54, management.ProductionBindingRequest{Key: "doubao", Version: "1.0.2", Factory: true, Enabled: true}))
	for _, entry := range []struct {
		path, body string
		pin        gin.HandlerFunc
	}{
		{"/v1/tasks/:key", `{"model":"MOVIE","prompt":"hi"}`, PinGenericTask()},
		{"/doubao/api/v3/contents/generations/tasks", `{"model":"MOVIE","content":[{"type":"text","text":"hi"}]}`, PinNativeTask()},
	} {
		for _, allow := range []bool{false, true} {
			r := gin.New()
			r.Use(func(c *gin.Context) {
				common.SetContextKey(c, constant.ContextKeyUsingGroup, "default")
				common.SetContextKey(c, constant.ContextKeyTokenModelLimitEnabled, true)
				name := "Movie"
				if allow {
					name = "MOVIE"
				}
				common.SetContextKey(c, constant.ContextKeyTokenModelLimit, map[string]bool{name: true})
			})
			r.POST(entry.path, entry.pin, Distribute(), func(c *gin.Context) {
				require.Equal(t, "MOVIE", c.GetString("original_model"))
				require.Equal(t, 54, common.GetContextKeyInt(c, constant.ContextKeyChannelId))
				storage, e := common.GetBodyStorage(c)
				require.NoError(t, e)
				raw, e := storage.Bytes()
				require.NoError(t, e)
				require.Equal(t, entry.body, string(raw))
				c.Status(204)
			})
			req := httptest.NewRequest("POST", strings.ReplaceAll(entry.path, ":key", "doubao"), strings.NewReader(entry.body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)
			if allow {
				require.Equal(t, 204, rec.Code, rec.Body.String())
			} else {
				require.Equal(t, 403, rec.Code, rec.Body.String())
			}
		}
	}
	t.Run("ordinary_body_untouched", func(t *testing.T) {
		payload := "{ \"model\":\"chat\", \"input\":\"hi\",\"vendor_value\":9007199254740993 }"
		r := gin.New()
		r.POST("/v1/responses", PinTaskPublicProtocol(), func(c *gin.Context) {
			require.Nil(t, service.TaskPublicRoute(c))
			body, e := io.ReadAll(c.Request.Body)
			require.NoError(t, e)
			require.Equal(t, payload, string(body))
			c.Status(204)
		})
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/v1/responses", strings.NewReader(payload))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(rec, req)
		require.Equal(t, 204, rec.Code)
	})
	for _, specific := range []bool{false, true} {
		for _, allow := range []bool{false, true} {
			r := gin.New()
			r.Use(func(c *gin.Context) {
				common.SetContextKey(c, constant.ContextKeyUsingGroup, "default")
				common.SetContextKey(c, constant.ContextKeyTokenModelLimitEnabled, true)
				name := "Movie"
				if allow {
					name = "MOVIE"
				}
				common.SetContextKey(c, constant.ContextKeyTokenModelLimit, map[string]bool{name: true})
				if specific {
					common.SetContextKey(c, constant.ContextKeyTokenSpecificChannelId, "54")
				}
			})
			r.POST("/v1/responses", PinTaskPublicProtocol(), Distribute(), func(c *gin.Context) {
				require.Equal(t, "MOVIE", c.GetString("original_model"))
				require.Equal(t, 54, common.GetContextKeyInt(c, constant.ContextKeyChannelId))
				c.Status(204)
			})
			req := httptest.NewRequest("POST", "/v1/responses", strings.NewReader(`{"model":"MOVIE","input":"hi"}`))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)
			if allow {
				require.Equal(t, 204, rec.Code, rec.Body.String())
			} else {
				require.Equal(t, 403, rec.Code, rec.Body.String())
			}
		}
	}
	disabled := false
	require.NoError(t, m.Configure(t.Context(), management.Settings{Enabled: &disabled}))
	_, err = m.ResolvePublicRoute(t.Context(), "MOVIE", "/v1/responses")
	require.ErrorIs(t, err, model.ErrTaskPluginDisabled, "paused task aliases cannot fall through to token relay")
	require.NoError(t, db.Model(&model.Channel{}).Where("id = 54").Update("models", "Movie,chat").Error)
	route, err := m.ResolvePublicRoute(t.Context(), "chat", "/v1/responses")
	require.NoError(t, err)
	require.Nil(t, route)
}
