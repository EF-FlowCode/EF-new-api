package controller

import (
	"net/http/httptest"
	"testing"

	"eflow.local/new-api/common"
	"eflow.local/new-api/constant"
	"eflow.local/new-api/internal/testdb"
	"eflow.local/new-api/model"
	"eflow.local/new-api/plugins"
	"eflow.local/new-api/relay"
	management "eflow.local/new-api/service/taskplugin"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestTaskEntryNativePresentationMySQL(t *testing.T) {
	db := testdb.OpenMySQL8(t, &model.Task{}, &model.Channel{}, &model.TaskPlugin{}, &model.TaskPluginControl{}, &model.TaskPluginShadowBinding{}, &model.TaskPluginProductionBinding{}, &model.TaskPluginSourceSnapshot{}, &model.TaskPluginTaskReference{})
	oldDB, oldManager := model.DB, management.Default
	t.Cleanup(func() { model.DB, management.Default = oldDB, oldManager })
	model.DB = db
	require.NoError(t, db.Create(&model.Channel{Id: 54, Type: 54, Status: common.ChannelStatusEnabled, Models: "Movie"}).Error)
	require.NoError(t, db.Create(&model.TaskPluginControl{Id: 1, Revision: 1, Enabled: true, FactoryEnabled: true, DisabledFactory: "[]"}).Error)
	source, err := plugins.Source("doubao")
	require.NoError(t, err)
	m := management.NewManager(model.SQLTaskPluginStore{DB: func() *gorm.DB { return db }}, map[string]string{"doubao": source})
	management.Default = m
	require.NoError(t, m.BindProduction(t.Context(), 54, management.ProductionBindingRequest{Key: "doubao", Version: "1.0.2", Factory: true, Enabled: true}))
	path := "/doubao/api/v3/contents/generations/tasks"
	task, err := m.CreatePluginTask(t.Context(), model.Task{TaskID: "local-task", UserId: 1, ChannelId: 54, Platform: "54", Status: model.TaskStatusSubmitted, Data: []byte(`{"id":"supplier-task"}`), Properties: model.Properties{OriginModelName: "MOVIE", UpstreamModelName: "doubao-seedance-1-0-lite-t2v"}, PrivateData: model.TaskPrivateData{Key: "secret-credential", UpstreamTaskID: "supplier-task", PluginContext: &model.TaskPluginContext{PublicProtocol: "native", NativePath: path, NativeMethod: "POST"}}}, management.TaskVersion{Key: "doubao", Version: "1.0.2", Factory: true})
	require.NoError(t, err)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest("POST", path, nil)
	require.Nil(t, completeTaskSubmission(c, nil, &relay.TaskSubmitResult{SavedTask: task}))
	require.JSONEq(t, `{"id":"local-task"}`, rec.Body.String())
	// An upstream terminal payload cannot bypass pending host settlement.
	task.Status = model.TaskStatusBillingPending
	task.Data = []byte(`{"id":"supplier-task","status":"succeeded"}`)
	require.NoError(t, db.Save(task).Error)
	for _, owner := range []int{1, 2} {
		n := &management.NativeRequest{Method: "GET", Path: path + "/local-task"}
		pin, err := m.ResolveNativeRequest(t.Context(), n)
		require.NoError(t, err)
		require.Nil(t, pin)
		rec := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(rec)
		c.Request = httptest.NewRequest("GET", n.Path, nil)
		common.SetContextKey(c, constant.ContextKeyUserId, owner)
		c.Set(management.NativeQueryContextKey, n)
		RelayNativeTask(c)
		if owner == 1 {
			require.Equal(t, 200, rec.Code)
			require.NotContains(t, rec.Body.String(), "succeeded")
			require.Contains(t, rec.Body.String(), "local-task")
		} else {
			require.Equal(t, 404, rec.Code)
		}
		require.NotContains(t, rec.Body.String(), "supplier-task")
		require.NotContains(t, rec.Body.String(), "secret-credential")
	}
	rec = httptest.NewRecorder()
	c, _ = gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest("GET", "/v1/tasks/local-task", nil)
	c.Params = gin.Params{{Key: "key", Value: "local-task"}}
	common.SetContextKey(c, constant.ContextKeyUserId, 1)
	RetrieveGenericTask(c)
	require.Equal(t, 200, rec.Code)
	require.Contains(t, rec.Body.String(), "BILLING_PENDING")
	require.NotContains(t, rec.Body.String(), "supplier-task")
}
