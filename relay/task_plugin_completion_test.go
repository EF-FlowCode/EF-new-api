package relay

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"eflow.local/new-api/common"
	"eflow.local/new-api/constant"
	"eflow.local/new-api/dto"
	"eflow.local/new-api/model"
	"eflow.local/new-api/plugins"
	host "eflow.local/new-api/relay/channel/task/jsplugin"
	relaycommon "eflow.local/new-api/relay/common"
	"eflow.local/new-api/service"
	management "eflow.local/new-api/service/taskplugin"
	"eflow.local/new-api/setting/config"
	"eflow.local/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestWANCompletionSubmission(t *testing.T) {
	oldPrices, oldRatios, oldPatches := ratio_setting.ModelPrice2JSONString(), ratio_setting.ModelRatio2JSONString(), constant.TaskPricePatches
	t.Cleanup(func() {
		require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(oldPrices))
		require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(oldRatios))
		constant.TaskPricePatches = oldPatches
	})
	source, err := plugins.Source("alibaba")
	require.NoError(t, err)
	m := durableTestManager(t, "alibaba", source, 17)
	require.NoError(t, m.BindProduction(t.Context(), 17, management.ProductionBindingRequest{Key: "alibaba", Version: "1.3.0", Factory: true, Enabled: true}))
	for _, name := range []string{"wan2.2-s2v", "wan3.0-video", "wan3.0-video-prime"} {
		for _, mode := range []string{"duration", "fixed", "expression", "missing_price"} {
			t.Run(name+"/"+mode, func(t *testing.T) {
				savedConfig := map[string]string{}
				require.NoError(t, config.GlobalConfig.SaveToDB(func(k, v string) error { savedConfig[k] = v; return nil }))
				t.Cleanup(func() { require.NoError(t, config.GlobalConfig.LoadFromDB(savedConfig)) })
				if mode == "expression" {
					modes, _ := json.Marshal(map[string]string{name: "tiered_expr"})
					exprs, _ := json.Marshal(map[string]string{name: `tier("task", param("duration") * 200)`})
					require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{"billing_setting.billing_mode": string(modes), "billing_setting.billing_expr": string(exprs)}))
				}
				prices, _ := json.Marshal(map[string]float64{name: 0.001})
				require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(string(prices)))
				constant.TaskPricePatches = oldPatches
				if mode == "fixed" {
					constant.TaskPricePatches = append(append([]string(nil), oldPatches...), name)
				}
				if mode == "missing_price" {
					require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(`{}`))
					ratios, _ := json.Marshal(map[string]float64{name: 1})
					require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(string(ratios)))
				}
				var calls atomic.Int32
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					calls.Add(1)
					_, _ = io.WriteString(w, `{"output":{"task_id":"accepted-`+name+mode+`"}}`)
				}))
				defer srv.Close()
				c, _ := gin.CreateTestContext(httptest.NewRecorder())
				c.Request = httptest.NewRequest("POST", "/v1/videos", strings.NewReader(`{"prompt":"test","duration":5,"resolution":"720P","image_url":"https://input.example/image.png","audio_url":"https://input.example/audio.mp3"}`))
				c.Request.Header.Set("Content-Type", "application/json")
				info := &relaycommon.RelayInfo{UserId: 1, OriginModelName: name, UsingGroup: "default", ChannelMeta: &relaycommon.ChannelMeta{ChannelId: 17, ChannelType: 17, ChannelBaseUrl: srv.URL, ApiKey: "key"}, Billing: &service.BillingSession{}}
				result, e := relayPluginTaskSubmitWithExecutor(c, info, "17", m, host.Executor{Client: srv.Client(), ValidateURL: func(string) error { return nil }})
				if mode == "missing_price" {
					require.NotNil(t, e)
					require.EqualValues(t, 0, calls.Load())
					return
				}
				require.Nil(t, e)
				require.EqualValues(t, 1, calls.Load())
				p := result.PluginContext.Billing
				require.NotNil(t, p)
				wantMode := mode
				if mode == "expression" {
					wantMode = "fixed"
				}
				require.Equal(t, wantMode, p.Mode)
				base := int(0.001 * common.GetQuotaPerUnit())
				require.Equal(t, base, p.BaseQuota)
				if wantMode == "fixed" {
					require.Equal(t, base, result.Quota)
				} else if name == "wan2.2-s2v" {
					require.Equal(t, base*36, result.Quota)
				} else {
					require.Equal(t, base*10, result.Quota)
				}
			})
		}
	}
}

func TestPluginPendingVideoResponse(t *testing.T) {
	task := &model.Task{TaskID: "task", Status: model.TaskStatusBillingPending, Progress: "99%"}
	var response dto.OpenAIVideo
	require.NoError(t, json.Unmarshal(pluginVideoResponse(task), &response))
	require.Equal(t, dto.VideoStatusInProgress, response.Status)
	require.Equal(t, dto.VideoStatusInProgress, task.ToOpenAIVideo().Status)
	for _, status := range []model.TaskStatus{model.TaskStatusSubmitting, model.TaskStatusSubmitUnknown} {
		task.Status = status
		require.NoError(t, json.Unmarshal(pluginVideoResponse(task), &response))
		require.Equal(t, dto.VideoStatusUnknown, response.Status)
		require.Equal(t, dto.VideoStatusUnknown, task.ToOpenAIVideo().Status)
	}
}
