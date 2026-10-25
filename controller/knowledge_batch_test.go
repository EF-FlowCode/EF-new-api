package controller

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"eflow.local/new-api/common"
	"eflow.local/new-api/constant"
	"eflow.local/new-api/internal/testdb"
	"eflow.local/new-api/middleware"
	"eflow.local/new-api/model"
	"eflow.local/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestCreateKnowledgeBatchBindsSelectedCredential(t *testing.T) {
	originalBatchPrices := ratio_setting.BatchInputRatio2JSONString()
	t.Cleanup(func() { require.NoError(t, ratio_setting.UpdateBatchInputRatioByJSONString(originalBatchPrices)) })
	require.NoError(t, ratio_setting.UpdateBatchInputRatioByJSONString(`{"text-embedding-v4":0.125}`))
	for _, tc := range []struct {
		name, keys, want string
		multi            bool
		statuses         map[int]int
	}{
		{name: "single", keys: "upstream-single", want: "upstream-single"},
		{name: "multi skips disabled", keys: "disabled-key\nupstream-enabled", want: "upstream-enabled", multi: true, statuses: map[int]int{0: common.ChannelStatusManuallyDisabled}},
		{name: "empty"},
		{name: "whitespace", keys: "  "},
		{name: "all disabled", keys: "disabled-key", multi: true, statuses: map[int]int{0: common.ChannelStatusManuallyDisabled}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			db := testdb.OpenMySQL8(t, &model.User{}, &model.Token{}, &model.Channel{}, &model.Task{}, &model.KnowledgeFile{}, &model.Log{})
			oldDB, oldLogDB, oldRedis := model.DB, model.LOG_DB, common.RedisEnabled
			model.DB, model.LOG_DB, common.RedisEnabled = db, db, false
			t.Cleanup(func() { model.DB, model.LOG_DB, common.RedisEnabled = oldDB, oldLogDB, oldRedis })
			u := model.User{Username: "batch_credentials", Quota: 1000, Group: "default"}
			require.NoError(t, db.Create(&u).Error)
			token := model.Token{UserId: u.Id, Key: "client-token", RemainQuota: 1000, Status: common.TokenStatusEnabled, ExpiredTime: -1}
			require.NoError(t, db.Create(&token).Error)
			base := "https://dashscope.aliyuncs.com"
			channel := model.Channel{Type: constant.ChannelTypeAli, Key: tc.keys, BaseURL: &base, Models: "text-embedding-v4", Group: "default", ChannelInfo: model.ChannelInfo{IsMultiKey: tc.multi, MultiKeyMode: constant.MultiKeyModeRandom, MultiKeyStatusList: tc.statuses}}
			require.NoError(t, db.Create(&channel).Error)
			file := model.KnowledgeFile{ID: "file_credentials", UserID: u.Id, Purpose: "batch", Model: "text-embedding-v4", Contents: []byte(`{"custom_id":"1","method":"POST","url":"/v1/embeddings","body":{"model":"text-embedding-v4","input":"knowledge base"}}`)}
			require.NoError(t, model.CreateKnowledgeFile(&file))
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/batches", strings.NewReader(`{}`))
			common.SetContextKey(c, constant.ContextKeyUserId, u.Id)
			common.SetContextKey(c, constant.ContextKeyTokenId, token.Id)
			common.SetContextKey(c, constant.ContextKeyUserGroup, "default")
			common.SetContextKey(c, constant.ContextKeyUsingGroup, "default")
			// The distributor publishes metadata but does not select a credential.
			require.Nil(t, middleware.SetupContextForChannel(c, &channel, file.Model))
			c.Set("knowledge_batch_input", &file)
			CreateKnowledgeBatch(c)
			var tasks []model.Task
			require.NoError(t, db.Find(&tasks).Error)
			require.NoError(t, db.First(&u, u.Id).Error)
			require.NoError(t, db.First(&token, token.Id).Error)
			if tc.want == "" {
				require.NotEqual(t, http.StatusOK, w.Code, w.Body.String())
				require.Empty(t, tasks)
				require.Equal(t, 1000, u.Quota)
				require.Equal(t, 1000, token.RemainQuota)
				return
			}
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())
			require.Len(t, tasks, 1)
			require.Equal(t, tc.want, tasks[0].PrivateData.Key)
			require.Equal(t, token.Id, tasks[0].PrivateData.TokenId)
			require.NotContains(t, w.Body.String(), tc.want)
			require.NotContains(t, string(tasks[0].Data), tc.want)
			require.Greater(t, tasks[0].Quota, 0)
			require.Equal(t, 1000-tasks[0].Quota, u.Quota)
			require.Equal(t, 1000-tasks[0].Quota, token.RemainQuota)
		})
	}
}
