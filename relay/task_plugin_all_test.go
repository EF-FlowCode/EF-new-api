package relay

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"eflow.local/new-api/common"
	"eflow.local/new-api/constant"
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

func TestAllPluginsUnifiedSubmission(t *testing.T) {
	old := ratio_setting.ModelPrice2JSONString()
	t.Cleanup(func() { require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(old)) })
	for _, key := range []string{"alibaba", "happyhorse", "doubao", "google", "hailuo", "jimeng", "kling", "sora", "sunoapi", "vertex-ai", "vidu"} {
		t.Run(key, func(t *testing.T) {
			source, err := plugins.Source(key)
			require.NoError(t, err)
			h, err := host.Compile(source)
			require.NoError(t, err)
			name := h.Meta().Models[0]
			if key == "alibaba" {
				name = "wan2.6-t2v"
			}
			prices, _ := json.Marshal(map[string]float64{name: 0.001})
			require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(string(prices)))
			channelType := model.TaskPluginChannelType(key)
			m := durableTestManager(t, key, source, 1)
			require.NoError(t, m.BindProduction(t.Context(), 1, management.ProductionBindingRequest{Key: key, Version: h.Identity().Version, Factory: true, Enabled: true}))
			for _, status := range []int{200, 503} {
				var calls atomic.Int32
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					calls.Add(1)
					var sent map[string]any
					require.NoError(t, json.NewDecoder(r.Body).Decode(&sent), "signed JSON must remain an object")
					require.NotEmpty(t, sent)
					if key == "vertex-ai" {
						require.Equal(t, "Bearer prepared-token", r.Header.Get("Authorization"))
					}
					if key == "jimeng" {
						require.Contains(t, r.Header.Get("Authorization"), "Credential=")
					}
					ack := map[string]any{"task_id": "provider-id", "id": "provider-id", "output": map[string]any{"task_id": "provider-id"}, "data": map[string]any{"task_id": "provider-id"}, "code": 0, "base_resp": map[string]any{"status_code": 0}, "name": "projects/test-project/locations/us-central1/publishers/google/models/veo-3.0-generate-001/operations/provider-id"}
					if key == "jimeng" {
						ack["code"] = 10000
					}
					if key == "sunoapi" {
						ack["code"], ack["data"] = "success", "provider-id"
					}
					w.WriteHeader(status)
					require.NoError(t, json.NewEncoder(w).Encode(ack))
				}))
				body, _ := json.Marshal(map[string]any{"model": name, "prompt": "a cat", "duration": 5})
				c, _ := gin.CreateTestContext(httptest.NewRecorder())
				c.Request = httptest.NewRequest("POST", "/v1/videos", bytes.NewReader(body))
				c.Request.Header.Set("Content-Type", "application/json")
				if key == "sunoapi" {
					c.Params = gin.Params{{Key: "action", Value: "MUSIC"}}
				}
				info := &relaycommon.RelayInfo{UserId: 1, OriginModelName: name, UsingGroup: "default", ChannelMeta: &relaycommon.ChannelMeta{ChannelId: 1, ChannelType: channelType, ChannelBaseUrl: srv.URL, ApiKey: "test-ak|test-sk"}, Billing: &service.BillingSession{}}
				executor := host.Executor{Client: srv.Client(), ValidateURL: func(raw string) error { require.True(t, strings.HasPrefix(raw, srv.URL+"/")); return nil }}
				auth := func(_ context.Context, _ int, base, _, _ string, _ host.Executor) (management.TaskQueryEnvironment, error) {
					return management.TaskQueryEnvironment{BaseURL: base, AuthHeader: "Bearer prepared-token", Auth: map[string]any{"projectId": "test-project"}}, nil
				}
				result, taskErr := relayPluginTaskSubmitWithAuth(c, info, constant.TaskPlatform(strconv.Itoa(channelType)), m, executor, auth)
				srv.Close()
				require.EqualValues(t, 1, calls.Load(), "exactly one provider submission")
				require.False(t, c.Writer.Written())
				if status != 200 {
					require.NotNil(t, taskErr)
					require.True(t, taskErr.SkipRetry)
					continue
				}
				require.Nil(t, taskErr)
				require.NotNil(t, result)
				require.Equal(t, key, result.Plugin.Key)
				require.NotEmpty(t, result.UpstreamTaskID)
				require.Positive(t, result.Quota)
				if key == "jimeng" {
					require.NotEmpty(t, result.PluginContext.State["req_key"])
				}
				require.NotEmpty(t, result.PluginContext.Usage)
			}
		})
	}
}

func TestSoraPluginRemixMySQL(t *testing.T) {
	db := testdb.OpenMySQL8(t, &model.Task{}, &model.Channel{}, &model.TaskPlugin{}, &model.TaskPluginControl{}, &model.TaskPluginShadowBinding{}, &model.TaskPluginProductionBinding{}, &model.TaskPluginSourceSnapshot{}, &model.TaskPluginTaskReference{}, &model.TaskPluginSubmission{}, &model.TaskPluginReview{}, &model.User{}, &model.Token{})
	oldDB := model.DB
	require.NoError(t, db.AutoMigrate(&model.Log{}))
	oldLogDB := model.LOG_DB
	model.LOG_DB = db
	t.Cleanup(func() { model.LOG_DB = oldLogDB })
	oldRedis := common.RedisEnabled
	common.RedisEnabled = false
	t.Cleanup(func() { common.RedisEnabled = oldRedis })
	model.DB = db
	t.Cleanup(func() { model.DB = oldDB })
	oldPrice := ratio_setting.ModelPrice2JSONString()
	t.Cleanup(func() { require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(oldPrice)) })
	require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(`{"sora-2":0.001}`))
	require.NoError(t, db.Create(&model.Channel{Id: 1, Type: 1}).Error)
	require.NoError(t, db.Create(&model.User{Id: 9, Username: "remix", Quota: 10000000, Status: 1}).Error)
	require.NoError(t, db.Create(&model.TaskPluginControl{Id: 1, Revision: 1, Enabled: true, FactoryEnabled: true, DisabledFactory: "[]"}).Error)
	source, err := plugins.Source("sora")
	require.NoError(t, err)
	h, err := host.Compile(source)
	require.NoError(t, err)
	m := management.NewManager(model.SQLTaskPluginStore{DB: func() *gorm.DB { return db }}, map[string]string{"sora": source})
	require.NoError(t, m.BindProduction(t.Context(), 1, management.ProductionBindingRequest{Key: "sora", Version: h.Identity().Version, Factory: true, Enabled: true}))
	_, err = m.CreatePluginTask(t.Context(), model.Task{TaskID: "public-origin", ChannelId: 1, UserId: 9, Platform: "1", Status: model.TaskStatusSuccess, Properties: model.Properties{OriginModelName: "sora-2", UpstreamModelName: "sora-2"}, PrivateData: model.TaskPrivateData{Key: "original-account", UpstreamTaskID: "real-origin", PluginContext: &model.TaskPluginContext{Usage: map[string]any{"seconds": float64(8), "size": "1792x1024"}}}}, management.TaskVersion{Key: "sora", Version: h.Identity().Version, Factory: true})
	require.NoError(t, err)
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		require.Equal(t, "/v1/videos/real-origin/remix", r.URL.Path)
		require.Equal(t, "Bearer original-account", r.Header.Get("Authorization"))
		_, _ = io.WriteString(w, `{"id":"remixed-`+strconv.Itoa(calls)+`"}`)
	}))
	defer srv.Close()
	for _, tc := range []struct {
		name          string
		user, channel int
		model         string
		mapping       string
		valid         bool
	}{
		{"valid", 9, 1, "sora-2", "", true},
		{"mapped_case", 9, 1, "sora-2", `{"sora-2":"SORA-2"}`, true},
		{"other_user", 8, 1, "sora-2", "", false},
		{"other_channel", 9, 2, "sora-2", "", false},
		{"changed_model", 9, 1, "sora-2-pro", "", false},
		{"changed_tail", 9, 1, "sora-2", `{"sora-2":"SORA-2-PRO"}`, false},
		{"changed_original_same_tail", 9, 1, "alias", `{"alias":"SORA-2"}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest("POST", "/v1/videos/public-origin/remix", strings.NewReader(`{"prompt":"change background"}`))
			c.Request.Header.Set("Content-Type", "application/json")
			c.Set("model_mapping", tc.mapping)
			info := &relaycommon.RelayInfo{UserId: tc.user, OriginModelName: tc.model, UsingGroup: "default", ChannelMeta: &relaycommon.ChannelMeta{ChannelId: tc.channel, ChannelType: 1, ChannelBaseUrl: srv.URL, ApiKey: "current-other-key"}, TaskRelayInfo: &relaycommon.TaskRelayInfo{OriginTaskID: "public-origin", Action: constant.TaskActionRemix}, Billing: &service.BillingSession{}}
			result, e := relayPluginTaskSubmitWithExecutor(c, info, "1", m, host.Executor{Client: srv.Client(), ValidateURL: func(string) error { return nil }})
			if !tc.valid {
				require.NotNil(t, e)
				require.True(t, e.SkipRetry)
				return
			}
			require.Nil(t, e)
			require.Equal(t, "remixed-"+strconv.Itoa(calls), result.UpstreamTaskID)
			require.Equal(t, float64(8), result.PluginContext.Usage["seconds"])
			require.Equal(t, "1792x1024", result.PluginContext.Usage["size"])
			require.Equal(t, 6666, result.Quota)
			require.Equal(t, "remix", info.Action)
			require.Equal(t, "sora-2", info.UpstreamModelName)
		})
	}
	require.Equal(t, 2, calls)
}

func TestSoraUnifiedMultipart(t *testing.T) {
	t.Run("legacy", func(t *testing.T) { testSoraMultipart(t, false) })
	t.Run("public_protocol", func(t *testing.T) { testSoraMultipart(t, true) })
}

func testSoraMultipart(t *testing.T, public bool) {
	old := ratio_setting.ModelPrice2JSONString()
	t.Cleanup(func() { require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(old)) })
	require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(`{"sora-2":0.001}`))
	source, err := plugins.Source("sora")
	require.NoError(t, err)
	h, err := host.Compile(source)
	require.NoError(t, err)
	m := durableTestManager(t, "sora", source, 1)
	if public {
		require.NoError(t, model.DB.Model(&model.Channel{}).Where("id = 1").Updates(map[string]any{"models": "sora-2", "status": common.ChannelStatusEnabled}).Error)
	}
	require.NoError(t, m.BindProduction(t.Context(), 1, management.ProductionBindingRequest{Key: "sora", Version: h.Identity().Version, Factory: true, Enabled: true}))
	var payload bytes.Buffer
	writer := multipart.NewWriter(&payload)
	require.NoError(t, writer.WriteField("model", "sora-2"))
	require.NoError(t, writer.WriteField("prompt", "test"))
	require.NoError(t, writer.WriteField("seconds", "4"))
	part, err := writer.CreateFormFile("input_reference", "image.png")
	require.NoError(t, err)
	_, err = part.Write([]byte("image-bytes"))
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, r.ParseMultipartForm(1<<20))
		defer r.MultipartForm.RemoveAll()
		require.Equal(t, "4", r.FormValue("seconds"))
		require.Empty(t, r.FormValue("image"))
		require.Empty(t, r.FormValue("duration"))
		f, _, err := r.FormFile("input_reference")
		require.NoError(t, err)
		defer f.Close()
		raw, err := io.ReadAll(f)
		require.NoError(t, err)
		require.Equal(t, "image-bytes", string(raw))
		_, _ = io.WriteString(w, `{"id":"sora-upload"}`)
	}))
	defer srv.Close()
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/videos", &payload)
	c.Request.Header.Set("Content-Type", writer.FormDataContentType())
	if public {
		route, err := m.ResolvePublicRoute(t.Context(), "sora-2", "/v1/videos")
		require.NoError(t, err)
		require.NotNil(t, route)
		c.Set(management.PublicRouteContextKey, route)
	}
	info := &relaycommon.RelayInfo{UserId: 1, OriginModelName: "sora-2", UsingGroup: "default", ChannelMeta: &relaycommon.ChannelMeta{ChannelId: 1, ChannelType: 1, ChannelBaseUrl: srv.URL}, Billing: &service.BillingSession{}}
	result, e := relayPluginTaskSubmitWithExecutor(c, info, "1", m, host.Executor{Client: srv.Client(), ValidateURL: func(string) error { return nil }})
	require.Nil(t, e)
	require.Equal(t, "sora-upload", result.UpstreamTaskID)
}
