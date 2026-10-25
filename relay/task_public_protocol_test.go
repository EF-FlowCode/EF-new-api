package relay

import (
	"encoding/json"
	"fmt"
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

func TestPublicProtocolDurableSubmissionMySQL(t *testing.T) {
	source, err := plugins.Source("doubao")
	require.NoError(t, err)
	m := durableTestManager(t, "doubao", source, 54)
	mapping := `{"Movie":"doubao-seedance-1-0-lite-t2v"}`
	require.NoError(t, model.DB.Model(&model.Channel{}).Where("id = 54").Updates(map[string]any{"status": common.ChannelStatusEnabled, "models": "Movie", "model_mapping": mapping}).Error)
	require.NoError(t, m.BindProduction(t.Context(), 54, management.ProductionBindingRequest{Key: "doubao", Version: "1.0.2", Factory: true, Enabled: true}))
	old := ratio_setting.ModelPrice2JSONString()
	t.Cleanup(func() { require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(old)) })
	require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(`{"Movie":0.0123,"doubao-seedance-1-0-lite-t2v":0.00001}`))
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var wire map[string]any
		require.NoError(t, json.NewDecoder(r.Body).Decode(&wire))
		require.Equal(t, "doubao-seedance-1-0-lite-t2v", wire["model"])
		require.Contains(t, fmt.Sprint(wire["content"]), "a fox")
		_, _ = fmt.Fprintf(w, `{"id":"accepted-%d"}`, calls)
	}))
	defer srv.Close()
	submit := func(name, key string) (*TaskSubmitResult, *model.Task) {
		route, e := m.ResolvePublicRoute(t.Context(), name, "/v1/responses")
		require.NoError(t, e)
		require.NotNil(t, route)
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest("POST", "/v1/responses", strings.NewReader(fmt.Sprintf(`{"model":%q,"input":"a fox","seconds":5,"background":true}`, name)))
		c.Request.Header.Set("Content-Type", "application/json")
		c.Request.Header.Set("Idempotency-Key", key)
		c.Set(management.PublicRouteContextKey, route)
		info := &relaycommon.RelayInfo{UserId: 1, TokenId: 1, OriginModelName: name, UsingGroup: "default", ChannelMeta: &relaycommon.ChannelMeta{ChannelId: 54, ChannelType: 54, ChannelBaseUrl: srv.URL, ApiKey: "provider-key"}}
		result, e2 := relayPluginTaskSubmitWithExecutor(c, info, "54", m, host.Executor{Client: srv.Client(), ValidateURL: func(string) error { return nil }})
		require.Nil(t, e2)
		require.NotNil(t, result.SavedTask)
		return result, result.SavedTask
	}
	first, task := submit("MOVIE", "one")
	require.False(t, first.Replayed)
	require.Equal(t, "MOVIE", task.Properties.OriginModelName)
	require.Equal(t, "doubao-seedance-1-0-lite-t2v", task.Properties.UpstreamModelName)
	require.Equal(t, "openai_responses", task.PrivateData.PluginContext.PublicProtocol)
	require.Equal(t, "background", task.PrivateData.PluginContext.PublicMode)
	require.Equal(t, 6150, task.Quota, "case-folding must retain the advertised alias price")
	replay, _ := submit("MOVIE", "one")
	require.True(t, replay.Replayed)
	require.Equal(t, 1, calls)
	_, second := submit("Movie", "two")
	require.Equal(t, task.Quota, second.Quota)
	var user model.User
	var token model.Token
	require.NoError(t, model.DB.First(&user, 1).Error)
	require.NoError(t, model.DB.First(&token, 1).Error)
	require.Equal(t, 100000000-task.Quota-second.Quota, user.Quota)
	require.Equal(t, user.Quota, token.RemainQuota)
	// Completion remains host-owned and atomic after the client has received
	// acceptance. Repeated failure observations refund only once.
	failed := *task
	failed.Status = model.TaskStatusFailure
	failed.FailReason = "fixture"
	failed.Progress = "100%"
	_, err = service.FinalizeTaskWithBilling(t.Context(), &failed, task.Status, 0, "fixture")
	require.NoError(t, err)
	_, err = service.FinalizeTaskWithBilling(t.Context(), &failed, task.Status, 0, "fixture")
	require.NoError(t, err)
	require.NoError(t, model.DB.First(&user, 1).Error)
	require.Equal(t, 100000000-second.Quota, user.Quota)
	require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(`{"Movie":0.0123,"MOVIE":0,"doubao-seedance-1-0-lite-t2v":0.00001}`))
	_, zero := submit("MOVIE", "explicit-zero")
	require.Zero(t, zero.Quota, "explicit original-spelling prices remain authoritative")
}

func TestPublicProtocolBodyPreservesOriginal(t *testing.T) {
	raw := "{ \"model\": \"chat\", \"input\":\"x\", \"extra\":9007199254740993 }"
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/responses", strings.NewReader(raw))
	c.Request.Header.Set("Content-Type", "application/json")
	body, err := readPluginProtocolBody(c, nil)
	require.NoError(t, err)
	require.Equal(t, "json", body["kind"])
	storage, err := common.GetBodyStorage(c)
	require.NoError(t, err)
	_, err = storage.Seek(0, io.SeekStart)
	require.NoError(t, err)
	saved, err := io.ReadAll(storage)
	require.NoError(t, err)
	require.Equal(t, raw, string(saved))
}
