package relay

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"eflow.local/new-api/common"
	"eflow.local/new-api/internal/testdb"
	"eflow.local/new-api/model"
	"eflow.local/new-api/plugins"
	host "eflow.local/new-api/relay/channel/task/jsplugin"
	relaycommon "eflow.local/new-api/relay/common"
	"eflow.local/new-api/service"
	management "eflow.local/new-api/service/taskplugin"
	"eflow.local/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func durableTestManager(t *testing.T, key, source string, channel int) *management.Manager {
	t.Helper()
	db := testdb.OpenMySQL8(t, &model.Task{}, &model.User{}, &model.Token{}, &model.Channel{}, &model.TaskPlugin{}, &model.TaskPluginControl{}, &model.TaskPluginShadowBinding{}, &model.TaskPluginProductionBinding{}, &model.TaskPluginSourceSnapshot{}, &model.TaskPluginTaskReference{}, &model.TaskPluginSubmission{}, &model.TaskPluginReview{})
	require.NoError(t, db.AutoMigrate(&model.Log{}))
	oldLogDB := model.LOG_DB
	oldLogEnabled := common.GetFeatureSettings().LogConsumeEnabled
	require.NoError(t, common.UpdateFeatureSettings(map[string]string{"LogConsumeEnabled": "true"}))
	t.Cleanup(func() {
		require.NoError(t, common.UpdateFeatureSettings(map[string]string{"LogConsumeEnabled": strconv.FormatBool(oldLogEnabled)}))
	})
	model.LOG_DB = db
	t.Cleanup(func() { model.LOG_DB = oldLogDB })
	oldDB, oldRedis, oldCache, oldBatch := model.DB, common.RedisEnabled, common.MemoryCacheEnabled, common.BatchUpdateEnabled
	model.DB, common.RedisEnabled, common.MemoryCacheEnabled, common.BatchUpdateEnabled = db, false, false, false
	t.Cleanup(func() {
		model.DB, common.RedisEnabled, common.MemoryCacheEnabled, common.BatchUpdateEnabled = oldDB, oldRedis, oldCache, oldBatch
	})
	require.NoError(t, db.Create(&model.User{Id: 1, Username: "submission", Quota: 100000000, Status: common.UserStatusEnabled}).Error)
	require.NoError(t, db.Create(&model.Token{Id: 1, UserId: 1, Key: "test-token", RemainQuota: 100000000, Status: common.TokenStatusEnabled}).Error)
	require.NoError(t, db.Create(&model.Channel{Id: channel, Type: model.TaskPluginChannelType(key)}).Error)
	require.NoError(t, db.Create(&model.TaskPluginControl{Id: 1, Revision: 1, Enabled: true, FactoryEnabled: true, DisabledFactory: "[]"}).Error)
	return management.NewManager(model.SQLTaskPluginStore{DB: func() *gorm.DB { return db }}, map[string]string{key: source})
}

func TestPluginDurableSubmissionMySQL(t *testing.T) {
	for _, mode := range []string{"success", "disconnect", "save_failure", "before_send_failure", "low_balance", "log_failure"} {
		t.Run(mode, func(t *testing.T) {
			source, err := plugins.Source("happyhorse")
			require.NoError(t, err)
			m := durableTestManager(t, "happyhorse", source, 58)
			require.NoError(t, m.BindProduction(t.Context(), 58, management.ProductionBindingRequest{Key: "happyhorse", Version: "1.0.0", Factory: true, Enabled: true}))
			old := ratio_setting.ModelPrice2JSONString()
			t.Cleanup(func() { require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(old)) })
			require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(`{"happyhorse-1.0-t2v":0.001}`))
			if mode == "low_balance" {
				require.NoError(t, model.DB.Model(&model.User{}).Where("id = 1").UpdateColumn("quota", 100).Error)
			}
			if mode == "log_failure" {
				require.NoError(t, model.DB.Migrator().DropTable(&model.Log{}))
			}
			var calls atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if mode == "disconnect" {
					conn, _, err := w.(http.Hijacker).Hijack()
					if err == nil {
						conn.Close()
					}
					return
				}
				_, _ = io.WriteString(w, `{"output":{"task_id":"accepted"}}`)
			}))
			defer srv.Close()
			if mode == "before_send_failure" {
				require.NoError(t, model.DB.Callback().Create().Before("gorm:create").Register("reject_reference", func(tx *gorm.DB) {
					if tx.Statement.Table == "task_plugin_task_references" {
						tx.AddError(errors.New("injected"))
					}
				}))
			}
			if mode == "save_failure" {
				require.NoError(t, model.DB.Callback().Update().Before("gorm:update").Register("reject_acceptance", func(tx *gorm.DB) {
					if task, ok := tx.Statement.Dest.(*model.Task); ok && task.Status == model.TaskStatusSubmitted {
						tx.AddError(errors.New("injected"))
					}
				}))
			}
			submit := func(payload, key string) (*TaskSubmitResult, string) {
				c, _ := gin.CreateTestContext(httptest.NewRecorder())
				c.Request = httptest.NewRequest("POST", "/v1/videos", strings.NewReader(payload))
				c.Request.Header.Set("Content-Type", "application/json")
				c.Request.Header.Set("Idempotency-Key", key)
				info := &relaycommon.RelayInfo{UserId: 1, TokenId: 1, OriginModelName: "happyhorse-1.0-t2v", UsingGroup: "default", ChannelMeta: &relaycommon.ChannelMeta{ChannelId: 58, ChannelType: 1001, ChannelBaseUrl: srv.URL, ApiKey: "provider-key"}}
				result, e := relayPluginTaskSubmitWithExecutor(c, info, "1001", m, host.Executor{Client: srv.Client(), ValidateURL: func(string) error { return nil }})
				if e != nil {
					return result, e.Code
				}
				return result, ""
			}
			payload := `{"model":"happyhorse-1.0-t2v","prompt":"test","seconds":5}`
			var wg sync.WaitGroup
			for range 6 {
				wg.Add(1)
				go func() { defer wg.Done(); submit(payload, "same-key") }()
			}
			wg.Wait()
			var user model.User
			var token model.Token
			require.NoError(t, model.DB.First(&user, 1).Error)
			require.NoError(t, model.DB.First(&token, 1).Error)
			if mode == "low_balance" {
				_, code := submit(payload, "same-key")
				require.Equal(t, "plugin_reservation_denied", code)
				require.Zero(t, calls.Load())
				require.Equal(t, 100, user.Quota)
				require.Equal(t, 100000000, token.RemainQuota)
				return
			}
			if mode == "before_send_failure" {
				require.Zero(t, calls.Load())
				require.Equal(t, 100000000, user.Quota)
				return
			}
			require.EqualValues(t, 1, calls.Load())
			require.Equal(t, 100000000-2500, user.Quota)
			require.Equal(t, user.Quota, token.RemainQuota)
			if mode != "log_failure" {
				var logs int64
				require.NoError(t, model.LOG_DB.Model(&model.Log{}).Count(&logs).Error)
				require.EqualValues(t, 1, logs, "concurrent retries must not repeat the consumption log")
				var log model.Log
				require.NoError(t, model.LOG_DB.First(&log).Error)
				var other map[string]any
				require.NoError(t, json.Unmarshal([]byte(log.Other), &other))
				require.Equal(t, "duration_seconds", other["billing_mode"])
				require.Equal(t, float64(5), other["dur_sec"])
			}
			_, code := submit(`{"model":"happyhorse-1.0-t2v","prompt":"different","seconds":5}`, "same-key")
			require.Equal(t, "plugin_idempotency_conflict", code)
			result, code := submit(payload, "same-key")
			var task model.Task
			require.NoError(t, model.DB.First(&task).Error)
			if mode == "success" || mode == "log_failure" {
				require.Empty(t, code)
				require.NotNil(t, result.SavedTask)
				require.Equal(t, "accepted", task.PrivateData.UpstreamTaskID)
			} else {
				require.Equal(t, "plugin_submission_unknown", code)
				require.EqualValues(t, model.TaskStatusSubmitUnknown, task.Status)
				require.Empty(t, model.GetTimedOutUnfinishedTasks(time.Now().Add(time.Hour).Unix(), 100))
				require.Empty(t, model.GetAllUnFinishSyncTasks(100))
			}
			if mode == "save_failure" {
				require.NoError(t, model.DB.Callback().Update().Remove("reject_acceptance"))
				// A fresh manager has no original request-local state.
				fresh := management.NewManager(model.SQLTaskPluginStore{DB: func() *gorm.DB { return model.DB }}, nil)
				recovered, err := service.RecoverPluginSubmission(context.Background(), fresh, task.ID)
				require.NoError(t, err)
				require.Equal(t, "accepted", recovered.PrivateData.UpstreamTaskID)
				require.EqualValues(t, 1, calls.Load())
			}
		})
	}
}

func TestPluginRequestFingerprint(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/videos", nil)
	c.Request.Header.Set("Idempotency-Key", "key")
	info := &relaycommon.RelayInfo{UserId: 1, TokenId: 2, OriginModelName: "model"}
	var a, b map[string]any
	require.NoError(t, json.Unmarshal([]byte(`{"x":1,"y":2}`), &a))
	require.NoError(t, json.Unmarshal([]byte(`{"y":2,"x":1}`), &b))
	files := map[string]host.File{"image": {Name: "image.png", ContentType: "image/png", Data: []byte("one")}}
	scope, fp, err := pluginRequestIdentity(c, info, a, files)
	require.NoError(t, err)
	scope2, fp2, err := pluginRequestIdentity(c, info, b, files)
	require.NoError(t, err)
	require.Equal(t, scope, scope2)
	require.Equal(t, fp, fp2)
	files["image"] = host.File{Name: "image.png", ContentType: "image/png", Data: []byte("two")}
	_, fp2, err = pluginRequestIdentity(c, info, b, files)
	require.NoError(t, err)
	require.NotEqual(t, fp, fp2)
	info.TokenId = 3
	scope2, _, err = pluginRequestIdentity(c, info, a, files)
	require.NoError(t, err)
	require.NotEqual(t, scope, scope2)
}
