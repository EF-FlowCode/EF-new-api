package controller

import (
	"encoding/base64"
	"net/http/httptest"
	"testing"

	"eflow.local/new-api/common"
	"eflow.local/new-api/internal/testdb"
	"eflow.local/new-api/model"
	"eflow.local/new-api/plugins"
	host "eflow.local/new-api/relay/channel/task/jsplugin"
	management "eflow.local/new-api/service/taskplugin"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestPluginVertexStoredContentMySQL(t *testing.T) {
	db := testdb.OpenMySQL8(t, &model.Task{}, &model.Channel{}, &model.TaskPlugin{}, &model.TaskPluginControl{}, &model.TaskPluginShadowBinding{}, &model.TaskPluginProductionBinding{}, &model.TaskPluginSourceSnapshot{}, &model.TaskPluginTaskReference{})
	oldDB, oldManager, oldCache := model.DB, management.Default, common.MemoryCacheEnabled
	model.DB = db
	common.MemoryCacheEnabled = false
	t.Cleanup(func() { model.DB, management.Default, common.MemoryCacheEnabled = oldDB, oldManager, oldCache })
	source, err := plugins.Source("vertex-ai")
	require.NoError(t, err)
	h, err := host.Compile(source)
	require.NoError(t, err)
	require.NoError(t, db.Create(&model.Channel{Id: 41, Type: 41}).Error)
	require.NoError(t, db.Create(&model.TaskPluginControl{Id: 1, Revision: 1, Enabled: true, FactoryEnabled: true, DisabledFactory: "[]"}).Error)
	management.Default = management.NewManager(model.SQLTaskPluginStore{DB: func() *gorm.DB { return db }}, map[string]string{"vertex-ai": source})
	require.NoError(t, management.Default.BindProduction(t.Context(), 41, management.ProductionBindingRequest{Key: "vertex-ai", Version: h.Identity().Version, Factory: true, Enabled: true}))
	task, err := management.Default.CreatePluginTask(t.Context(), model.Task{TaskID: "public-video", ChannelId: 41, UserId: 7, Platform: "41", Status: model.TaskStatusSuccess, Properties: model.Properties{OriginModelName: h.Meta().Models[0], UpstreamModelName: h.Meta().Models[0]}, PrivateData: model.TaskPrivateData{UpstreamTaskID: base64.RawURLEncoding.EncodeToString([]byte("projects/p/locations/l/publishers/google/models/m/operations/id"))}, Data: []byte(`{"response":{"videos":[{"bytesBase64Encoded":"dmlkZW8=","mimeType":"video/mp4"}]}}`)}, management.TaskVersion{Key: "vertex-ai", Version: h.Identity().Version, Factory: true})
	require.NoError(t, err)
	request := func(user int) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("GET", "/v1/videos/public-video/content", nil)
		c.Set("id", user)
		c.Params = gin.Params{{Key: "task_id", Value: task.TaskID}}
		VideoProxy(c)
		return w
	}
	w := request(7)
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Equal(t, "video", w.Body.String())
	require.Equal(t, "private, no-store", w.Header().Get("Cache-Control"))
	require.Equal(t, 404, request(8).Code)
	require.NoError(t, db.Model(&model.TaskPluginControl{}).Where("id = 1").Update("enabled", false).Error)
	w = request(7)
	require.Equal(t, 503, w.Code)
	require.NotEqual(t, "video", w.Body.String())
}
