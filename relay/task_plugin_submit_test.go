package relay

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"eflow.local/new-api/common"
	"eflow.local/new-api/constant"
	"eflow.local/new-api/dto"
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

type submitPluginStore struct{ state model.TaskPluginSnapshot }

func (s *submitPluginStore) Read(context.Context) (model.TaskPluginSnapshot, error) {
	return s.state.Clone(), nil
}

func TestHappyhorseUnifiedSubmission(t *testing.T) {
	oldPrices := ratio_setting.ModelPrice2JSONString()
	t.Cleanup(func() { require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(oldPrices)) })
	require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(`{"local-video":0.001}`))
	source, err := plugins.Source("happyhorse")
	require.NoError(t, err)
	m := durableTestManager(t, "happyhorse", source, 58)
	require.NoError(t, m.BindProduction(t.Context(), 58, management.ProductionBindingRequest{Key: "happyhorse", Version: "1.0.0", Factory: true, Enabled: true}))
	for _, name := range []string{"happyhorse-1.0-t2v", "happyhorse-1.0-i2v", "happyhorse-1.0-r2v", "happyhorse-1.0-video-edit"} {
		t.Run(name, func(t *testing.T) {
			var calls atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				require.Equal(t, "Bearer local-test-key", r.Header.Get("Authorization"))
				var body map[string]any
				require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
				require.Equal(t, name, body["model"])
				params := body["parameters"].(map[string]any)
				require.Equal(t, "1080P", params["resolution"])
				require.Equal(t, float64(5), params["duration"])
				_, _ = io.WriteString(w, `{"output":{"task_id":"accepted-happyhorse-`+name+`"}}`)
			}))
			defer srv.Close()
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest("POST", "/v1/videos", strings.NewReader(`{"model":"local-video","prompt":"hello","seconds":"5","input_reference":"https://example.com/image.png","metadata":{"resolution":"1080P","media_url":"https://example.com/video.mp4"}}`))
			c.Request.Header.Set("Content-Type", "application/json")
			c.Set("model_mapping", `{"local-video":"`+name+`"}`)
			info := &relaycommon.RelayInfo{UserId: 1, OriginModelName: "local-video", UsingGroup: "default", ChannelMeta: &relaycommon.ChannelMeta{ChannelId: 58, ChannelType: 1001, ChannelBaseUrl: srv.URL, ApiKey: "local-test-key"}, Billing: &service.BillingSession{}}
			result, taskErr := relayPluginTaskSubmitWithExecutor(c, info, "1001", m, host.Executor{Client: srv.Client(), ValidateURL: func(raw string) error { require.True(t, strings.HasPrefix(raw, srv.URL+"/")); return nil }})
			require.Nil(t, taskErr)
			require.EqualValues(t, 1, calls.Load())
			require.Equal(t, "accepted-happyhorse-"+name, result.UpstreamTaskID)
			require.Equal(t, "happyhorse", result.Plugin.Key)
			require.Equal(t, int(0.001*common.GetQuotaPerUnit()*1.778*5), result.Quota)
			require.False(t, c.Writer.Written())
		})
	}
}
func (s *submitPluginStore) Update(_ context.Context, fn func(*model.TaskPluginSnapshot) error) (model.TaskPluginSnapshot, error) {
	next := s.state.Clone()
	if err := fn(&next); err != nil {
		return model.TaskPluginSnapshot{}, err
	}
	next.Revision++
	s.state = next
	return next.Clone(), nil
}

func TestPluginSubmitSingleAttemptAndDeferredResponse(t *testing.T) {
	for _, tc := range []struct {
		name, response, code                      string
		status                                    int
		invalidUsage, timeout, canceled, disabled bool
		calls                                     int32
	}{
		{name: "success", status: 200, response: `{"output":{"task_id":"provider-id"}}`, calls: 1},
		{name: "non_2xx_with_valid_id", status: 503, response: `{"output":{"task_id":"provider-id"}}`, code: "plugin_upstream_failed", calls: 1},
		{name: "empty_id", status: 200, response: `{"output":{"task_id":""}}`, code: "plugin_response_invalid", calls: 1},
		{name: "malformed_json", status: 200, response: `{`, code: "plugin_request_failed", calls: 1},
		{name: "invalid_usage", invalidUsage: true, code: "plugin_usage_invalid"},
		{name: "timeout_after_acceptance", timeout: true, code: "plugin_request_failed", calls: 1},
		{name: "canceled", canceled: true, code: "plugin_prepare_failed"},
		{name: "db_switch_without_refresh", disabled: true, code: "plugin_prepare_failed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			received := make(chan map[string]any, 1)
			release := make(chan struct{})
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				var body map[string]any
				_ = json.NewDecoder(r.Body).Decode(&body)
				received <- body
				if tc.timeout {
					<-release
					return
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.response)
			}))
			defer srv.Close()
			source, err := plugins.Source("alibaba")
			require.NoError(t, err)
			if tc.invalidUsage {
				modified := strings.Replace(source, "return { seconds: seconds, resolution: resolution };", "return { seconds: -1, resolution: resolution };", 1)
				require.NotEqual(t, source, modified)
				source = modified
			}
			h, err := host.Compile(source)
			require.NoError(t, err)
			store := &submitPluginStore{state: model.TaskPluginSnapshot{Revision: 1, Enabled: true, FactoryEnabled: true}}
			manager := management.NewManager(store, map[string]string{"alibaba": source})
			require.NoError(t, manager.BindProduction(t.Context(), 17, management.ProductionBindingRequest{Key: "alibaba", Version: h.Identity().Version, Factory: true, Enabled: true}))
			if tc.disabled {
				store.state.Enabled = false
			}
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if tc.canceled {
				cancel()
			}
			c.Request = httptest.NewRequestWithContext(ctx, "POST", "/v1/videos", nil)
			info := &relaycommon.RelayInfo{OriginModelName: "wan2.6-t2v", ChannelMeta: &relaycommon.ChannelMeta{ChannelId: 17, ChannelBaseUrl: srv.URL, UpstreamModelName: "wan2.6-t2v"}}
			info.TaskRelayInfo = &relaycommon.TaskRelayInfo{PublicTaskID: "public-id"}
			info.PriceData.Quota = 400
			client := srv.Client()
			if tc.timeout {
				client.Timeout = 100 * time.Millisecond
			}
			executor := host.Executor{Client: client, ValidateURL: func(raw string) error { require.True(t, strings.HasPrefix(raw, srv.URL+"/")); return nil }}
			body := `{"model":"wan2.6-t2v","prompt":"keep this prompt","negative_prompt":"blur","duration":10,"size":"1920*1080","prompt_extend":false,"watermark":false}`
			result, taskErr := submitTaskPluginWithExecutor(c, info, constant.TaskPlatform("17"), strings.NewReader(body), manager, executor)
			close(release)
			require.Equal(t, tc.calls, calls.Load(), "submission is never replayed")
			require.False(t, c.Writer.Written(), "no success or error body before caller completes persistence")
			if tc.code != "" {
				require.Nil(t, result)
				require.NotNil(t, taskErr)
				require.Equal(t, tc.code, taskErr.Code)
				require.True(t, taskErr.SkipRetry)
				return
			}
			require.Nil(t, taskErr)
			require.Equal(t, "provider-id", result.UpstreamTaskID)
			require.Equal(t, 400, result.Quota)
			require.Equal(t, h.Identity().SourceHash, result.Plugin.SourceHash)
			sent := <-received
			require.Equal(t, "keep this prompt", sent["input"].(map[string]any)["prompt"])
			require.Equal(t, "blur", sent["input"].(map[string]any)["negative_prompt"])
			parameters := sent["parameters"].(map[string]any)
			require.Equal(t, float64(10), parameters["duration"])
			require.Equal(t, "1920*1080", parameters["size"])
			require.Equal(t, false, parameters["prompt_extend"])
		})
	}
}

func submitTaskPluginWithExecutor(c *gin.Context, info *relaycommon.RelayInfo, platform constant.TaskPlatform, requestBody io.Reader, manager *management.Manager, executor host.Executor) (result *TaskSubmitResult, taskErr *dto.TaskError) {
	// A bound plugin failure must not silently select a different channel or Go
	// adaptor. In particular a transport/parse failure may follow acceptance.
	defer func() {
		if taskErr != nil {
			taskErr.SkipRetry = true
		}
	}()
	raw, err := io.ReadAll(requestBody)
	if err != nil {
		return nil, service.TaskErrorWrapper(err, "plugin_request_body_failed", http.StatusInternalServerError)
	}
	var body map[string]any
	if err := common.Unmarshal(raw, &body); err != nil {
		return nil, service.TaskErrorWrapperLocal(err, "plugin_request_body_invalid", http.StatusInternalServerError)
	}
	input := pluginSubmitInput(info, body)
	prepared, err := manager.PrepareProductionSubmit(c.Request.Context(), info.ChannelId, input)
	if err != nil {
		return nil, service.TaskErrorWrapperLocal(err, "plugin_prepare_failed", http.StatusServiceUnavailable)
	}
	// This test helper isolates request execution; unified-host tests cover pricing.
	if _, err := prepared.Usage(c.Request.Context()); err != nil {
		return nil, service.TaskErrorWrapperLocal(err, "plugin_usage_invalid", http.StatusBadRequest)
	}
	return submitPreparedTaskPlugin(c, info, platform, prepared, executor)
}
