package controller

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"eflow.local/new-api/common"
	"eflow.local/new-api/constant"
	"eflow.local/new-api/dto"
	"eflow.local/new-api/internal/testdb"
	"eflow.local/new-api/model"
	"eflow.local/new-api/plugins"
	"eflow.local/new-api/relay"
	host "eflow.local/new-api/relay/channel/task/jsplugin"
	relaycommon "eflow.local/new-api/relay/common"
	"eflow.local/new-api/service"
	management "eflow.local/new-api/service/taskplugin"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestTaskPluginErrorsNeverRetryAcrossChannels(t *testing.T) {
	for _, status := range []int{429, 307, 500, 502, 503} {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		err := &dto.TaskError{StatusCode: status, SkipRetry: true}
		require.False(t, shouldRetryTaskRelay(c, 17, err, 3))
	}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	require.True(t, shouldRetryTaskRelay(c, 17, &dto.TaskError{StatusCode: 502}, 3), "legacy retry policy remains available")
}

func TestTaskPluginPersistenceAndCompensationMySQL(t *testing.T) {
	for _, mode := range []string{"success", "reference_insert_failure", "settlement_failure", "source_changed"} {
		t.Run(mode, func(t *testing.T) {
			db := testdb.OpenMySQL8(t, &model.Task{}, &model.User{}, &model.Token{}, &model.Channel{}, &model.Log{}, &model.QuotaData{}, &model.TaskPlugin{}, &model.TaskPluginControl{}, &model.TaskPluginShadowBinding{}, &model.TaskPluginProductionBinding{}, &model.TaskPluginSourceSnapshot{}, &model.TaskPluginTaskReference{})
			oldDB, oldLog, oldManager := model.DB, model.LOG_DB, management.Default
			oldRedis, oldCache, oldBatch := common.RedisEnabled, common.MemoryCacheEnabled, common.BatchUpdateEnabled
			model.DB, model.LOG_DB = db, db
			common.RedisEnabled, common.MemoryCacheEnabled, common.BatchUpdateEnabled = false, false, false
			t.Cleanup(func() {
				model.DB, model.LOG_DB, management.Default = oldDB, oldLog, oldManager
				common.RedisEnabled, common.MemoryCacheEnabled, common.BatchUpdateEnabled = oldRedis, oldCache, oldBatch
			})
			const initialQuota = 100000000
			require.NoError(t, db.Create(&model.User{Id: 1, Username: "plugin-test", Quota: initialQuota, Status: common.UserStatusEnabled}).Error)
			require.NoError(t, db.Create(&model.Token{Id: 1, UserId: 1, Key: "plugin-test-token", RemainQuota: initialQuota, Status: common.TokenStatusEnabled}).Error)
			require.NoError(t, db.Create(&model.Channel{Id: 17, Type: constant.ChannelTypeAli, Key: "fake-provider-key"}).Error)
			require.NoError(t, db.Create(&model.TaskPluginControl{Id: 1, Revision: 1, Enabled: true, FactoryEnabled: true, DisabledFactory: "[]"}).Error)
			source, err := plugins.Source("alibaba")
			require.NoError(t, err)
			h, err := host.Compile(source)
			require.NoError(t, err)
			management.Default = management.NewManager(model.SQLTaskPluginStore{DB: func() *gorm.DB { return db }}, map[string]string{"alibaba": source})
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest("POST", "/v1/videos", nil)
			info := &relaycommon.RelayInfo{UserId: 1, TokenId: 1, TokenKey: "plugin-test-token", ForcePreConsume: true, OriginModelName: "wan2.6-t2v", ChannelMeta: &relaycommon.ChannelMeta{ChannelId: 17, ApiKey: "fake-provider-key", UpstreamModelName: "wan2.6-t2v"}, TaskRelayInfo: &relaycommon.TaskRelayInfo{PublicTaskID: "public-id"}}
			info.PriceData.Quota = 100
			session, apiErr := service.NewBillingSession(c, info, 100)
			require.Nil(t, apiErr)
			info.Billing = session
			result := &relay.TaskSubmitResult{Platform: "17", UpstreamTaskID: "accepted-id", Quota: 100, TaskData: []byte(`{"output":{"task_id":"accepted-id"}}`), Plugin: &model.TaskPluginIdentity{IdentityVersion: 1, APIVersion: 1, Key: "alibaba", Version: h.Identity().Version, SourceHash: h.Identity().SourceHash, Factory: true}}
			if mode == "reference_insert_failure" {
				require.NoError(t, db.Callback().Create().Before("gorm:create").Register("reject_plugin_reference", func(tx *gorm.DB) {
					if tx.Statement.Table == "task_plugin_task_references" {
						tx.AddError(errors.New("injected reference failure"))
					}
				}))
			}
			if mode == "settlement_failure" {
				// Settlement no longer has a balance floor, so inject a one-shot
				// wallet write failure; the deferred refund must still succeed.
				result.Quota = initialQuota * 2
				failed := false
				require.NoError(t, db.Callback().Update().Before("gorm:update").Register("reject_settlement_once", func(tx *gorm.DB) {
					if tx.Statement.Table == "users" && !failed {
						failed = true
						tx.AddError(errors.New("injected settlement failure"))
					}
				}))
			}
			if mode == "source_changed" {
				result.Plugin.SourceHash = "wrong-submission-source"
			}
			taskErr := completeTaskSubmission(c, info, result)
			// Exercise RelayTask's deferred refund twice: compensation and refunds
			// must remain idempotent for both wallet and token.
			if taskErr != nil {
				session.Refund(c)
				session.Refund(c)
			}
			var user model.User
			var token model.Token
			require.NoError(t, db.First(&user, 1).Error)
			require.NoError(t, db.First(&token, 1).Error)
			var tasks, refs, sources int64
			require.NoError(t, db.Model(&model.Task{}).Count(&tasks).Error)
			require.NoError(t, db.Model(&model.TaskPluginTaskReference{}).Count(&refs).Error)
			require.NoError(t, db.Model(&model.TaskPluginSourceSnapshot{}).Count(&sources).Error)
			if mode == "success" {
				require.Nil(t, taskErr)
				require.Equal(t, http.StatusOK, recorder.Code)
				require.Contains(t, recorder.Body.String(), "public-id")
				require.EqualValues(t, 1, tasks)
				require.Equal(t, tasks, refs)
				require.Equal(t, tasks, sources)
				require.Equal(t, initialQuota-100, user.Quota)
				require.Equal(t, initialQuota-100, token.RemainQuota)
				var saved model.Task
				require.NoError(t, db.First(&saved).Error)
				require.Equal(t, result.Plugin, saved.PrivateData.Plugin)
				require.Equal(t, "accepted-id", saved.PrivateData.UpstreamTaskID)
			} else {
				require.NotNil(t, taskErr)
				require.False(t, c.Writer.Written(), "never advertise success before commit")
				respondTaskError(c, taskErr)
				require.Equal(t, http.StatusInternalServerError, recorder.Code)
				require.NotContains(t, recorder.Body.String(), "queued")
				require.Zero(t, tasks)
				require.Zero(t, refs)
				require.Zero(t, sources)
				require.Equal(t, initialQuota, user.Quota)
				require.Equal(t, initialQuota, token.RemainQuota)
				require.Zero(t, token.UsedQuota)
			}
		})
	}
}
