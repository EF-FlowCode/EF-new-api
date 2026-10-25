package relay

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"eflow.local/new-api/common"
	"eflow.local/new-api/model"
	"eflow.local/new-api/plugins"
	host "eflow.local/new-api/relay/channel/task/jsplugin"
	relaycommon "eflow.local/new-api/relay/common"
	"eflow.local/new-api/service"
	management "eflow.local/new-api/service/taskplugin"
	"eflow.local/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestTaskEntryDurableMySQL(t *testing.T) {
	for _, protocol := range []string{"task", "native"} {
		t.Run(protocol, func(t *testing.T) {
			source, err := plugins.Source("doubao")
			require.NoError(t, err)
			m := durableTestManager(t, "doubao", source, 54)
			require.NoError(t, model.DB.Model(&model.Channel{}).Where("id = 54").Updates(map[string]any{"status": common.ChannelStatusEnabled, "models": "Movie", "model_mapping": `{"Movie":"doubao-seedance-1-0-lite-t2v"}`}).Error)
			require.NoError(t, m.BindProduction(t.Context(), 54, management.ProductionBindingRequest{Key: "doubao", Version: "1.0.2", Factory: true, Enabled: true}))
			old := ratio_setting.ModelPrice2JSONString()
			t.Cleanup(func() { require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(old)) })
			require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(`{"Movie":0.0123,"doubao-seedance-1-0-lite-t2v":0.00001}`))
			path, raw := "/v1/tasks/doubao", `{"model":"MOVIE","prompt":"a fox","seconds":5}`
			if protocol == "native" {
				path, raw = "/doubao/api/v3/contents/generations/tasks", `{"model":"MOVIE","content":[{"type":"text","text":"a fox"}],"duration":5}`
			}
			var pin *management.PublicRoute
			if protocol == "native" {
				var body map[string]any
				require.NoError(t, json.Unmarshal([]byte(raw), &body))
				pin, err = m.ResolveNativeRequest(t.Context(), &management.NativeRequest{Path: path, Method: "POST", Body: map[string]any{"kind": "json", "value": body}})
			} else {
				pin, err = m.ResolveGenericRoute(t.Context(), "doubao", "MOVIE")
			}
			require.NoError(t, err)
			require.NotNil(t, pin)
			calls := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				var body map[string]any
				require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
				require.Equal(t, "doubao-seedance-1-0-lite-t2v", body["model"])
				require.Contains(t, body["content"], map[string]any{"type": "text", "text": "a fox"})
				_, _ = io.WriteString(w, `{"id":"supplier-task"}`)
			}))
			defer srv.Close()
			submit := func() *TaskSubmitResult {
				c, _ := gin.CreateTestContext(httptest.NewRecorder())
				c.Request = httptest.NewRequest("POST", path, strings.NewReader(raw))
				c.Request.Header.Set("Content-Type", "application/json")
				c.Request.Header.Set("Idempotency-Key", "one")
				c.Set(management.PublicRouteContextKey, pin)
				info := &relaycommon.RelayInfo{UserId: 1, TokenId: 1, OriginModelName: "MOVIE", UsingGroup: "default", ChannelMeta: &relaycommon.ChannelMeta{ChannelId: 54, ChannelType: 54, ChannelBaseUrl: srv.URL, ApiKey: "fixture-key"}}
				result, taskErr := relayPluginTaskSubmitWithExecutor(c, info, "54", m, host.Executor{Client: srv.Client(), ValidateURL: func(string) error { return nil }})
				require.Nil(t, taskErr)
				require.NotNil(t, result)
				return result
			}
			result := submit()
			task := result.SavedTask
			require.Equal(t, 6150, task.Quota)
			require.Equal(t, "MOVIE", task.Properties.OriginModelName)
			require.Equal(t, protocol, task.PrivateData.PluginContext.PublicProtocol)
			require.True(t, submit().Replayed)
			require.Equal(t, 1, calls)
			var user model.User
			var token model.Token
			require.NoError(t, model.DB.First(&user, 1).Error)
			require.NoError(t, model.DB.First(&token, 1).Error)
			require.Equal(t, 100000000-6150, user.Quota)
			require.Equal(t, user.Quota, token.RemainQuota)
			_, _, err = m.PublicTask(t.Context(), task.TaskID, 2, "")
			require.Error(t, err)
			failed := *task
			failed.Status = model.TaskStatusFailure
			_, err = service.FinalizeTaskWithBilling(t.Context(), &failed, task.Status, 0, "fixture")
			require.NoError(t, err)
			_, err = service.FinalizeTaskWithBilling(t.Context(), &failed, task.Status, 0, "fixture")
			require.NoError(t, err)
			require.NoError(t, model.DB.First(&user, 1).Error)
			require.Equal(t, 100000000, user.Quota)
		})
	}
}

func TestTaskEntrySunoMismatchDoesNotDebit(t *testing.T) {
	source, err := plugins.Source("sunoapi")
	require.NoError(t, err)
	m := durableTestManager(t, "sunoapi", source, 36)
	require.NoError(t, model.DB.Model(&model.Channel{}).Where("id = 36").Updates(map[string]any{"status": common.ChannelStatusEnabled, "models": "suno_lyrics"}).Error)
	require.NoError(t, m.BindProduction(t.Context(), 36, management.ProductionBindingRequest{Key: "sunoapi", Version: "1.0.3", Factory: true, Enabled: true}))
	pin, err := m.ResolveGenericRoute(t.Context(), "sunoapi", "suno_lyrics")
	require.NoError(t, err)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/tasks/sunoapi", strings.NewReader(`{"model":"suno_lyrics","action":"music","prompt":"song"}`))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set(management.PublicRouteContextKey, pin)
	info := &relaycommon.RelayInfo{UserId: 1, TokenId: 1, OriginModelName: "suno_lyrics", UsingGroup: "default", ChannelMeta: &relaycommon.ChannelMeta{ChannelId: 36, ChannelType: 36, ChannelBaseUrl: "https://example.invalid", ApiKey: "fixture-key"}}
	result, taskErr := relayPluginTaskSubmitWithExecutor(c, info, "36", m, host.Executor{ValidateURL: func(string) error { t.Fatal("mismatched action reached HTTP execution"); return nil }})
	require.Nil(t, result)
	require.NotNil(t, taskErr)
	require.Equal(t, "plugin_protocol_invalid", taskErr.Code)
	require.Equal(t, 400, taskErr.StatusCode)
	var user model.User
	var token model.Token
	require.NoError(t, model.DB.First(&user, 1).Error)
	require.NoError(t, model.DB.First(&token, 1).Error)
	require.Equal(t, 100000000, user.Quota)
	require.Equal(t, 100000000, token.RemainQuota)
	var count int64
	require.NoError(t, model.DB.Model(&model.Task{}).Count(&count).Error)
	require.Zero(t, count)
}
