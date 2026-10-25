package relay

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"eflow.local/new-api/model"
	"eflow.local/new-api/plugins"
	host "eflow.local/new-api/relay/channel/task/jsplugin"
	relaycommon "eflow.local/new-api/relay/common"
	management "eflow.local/new-api/service/taskplugin"
	"eflow.local/new-api/setting/billing_setting"
	"eflow.local/new-api/setting/config"
	"eflow.local/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestPluginTokenPricingSnapshotSubmissionMySQL(t *testing.T) {
	oldRatios, oldPrices, oldGroups, oldSpecial := ratio_setting.ModelRatio2JSONString(), ratio_setting.ModelPrice2JSONString(), ratio_setting.GroupRatio2JSONString(), ratio_setting.GroupGroupRatio2JSONString()
	t.Cleanup(func() {
		require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(oldRatios))
		require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(oldPrices))
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(oldGroups))
		require.NoError(t, ratio_setting.UpdateGroupGroupRatioByJSONString(oldSpecial))
	})
	for _, mode := range []string{"configured", "unset", "zero", "structured"} {
		t.Run(mode, func(t *testing.T) {
			savedConfig := map[string]string{}
			require.NoError(t, config.GlobalConfig.SaveToDB(func(k, v string) error { savedConfig[k] = v; return nil }))
			t.Cleanup(func() { require.NoError(t, config.GlobalConfig.LoadFromDB(savedConfig)) })
			require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(`{}`))
			raw := `{"task-token-alias":0.5}`
			if mode == "unset" {
				raw = `{}`
			}
			if mode == "zero" {
				raw = `{"task-token-alias":0}`
			}
			require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(raw))
			require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":2}`))
			require.NoError(t, ratio_setting.UpdateGroupGroupRatioByJSONString(`{"vip":{"default":0.3}}`))
			source, err := plugins.Source("doubao")
			require.NoError(t, err)
			handle, err := host.Compile(source)
			require.NoError(t, err)
			if mode == "structured" {
				rules, err := json.Marshal(map[string]any{"task-token-alias": map[string]any{"doubao": map[string]any{
					"version": 1, "kind": "token_completion", "model_ratio": 0.5, "ratio_configured": true,
					"upstream_model": handle.Meta().Models[0], "dimensions": "provider_v1", "settlement": "tokens",
				}}})
				require.NoError(t, err)
				require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{"billing_setting.task_expressions": string(rules)}))
				// The selected structured rule must win over the generic price.
				require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"task-token-alias":99}`))
			}
			m := durableTestManager(t, "doubao", source, 54)
			require.NoError(t, m.BindProduction(t.Context(), 54, management.ProductionBindingRequest{Key: "doubao", Version: handle.Identity().Version, Factory: true, Enabled: true}))
			var calls atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				_, _ = io.WriteString(w, `{"id":"provider-snapshot"}`)
			}))
			defer srv.Close()
			mapping, err := json.Marshal(map[string]string{"task-token-alias": handle.Meta().Models[0]})
			require.NoError(t, err)
			submit := func() *TaskSubmitResult {
				c, _ := gin.CreateTestContext(httptest.NewRecorder())
				c.Request = httptest.NewRequest("POST", "/v1/videos", strings.NewReader(`{"model":"task-token-alias","prompt":"test","duration":5}`))
				c.Request.Header.Set("Content-Type", "application/json")
				c.Request.Header.Set("Idempotency-Key", "token-snapshot-once")
				c.Set("model_mapping", string(mapping))
				info := &relaycommon.RelayInfo{UserId: 1, TokenId: 1, OriginModelName: "task-token-alias", UserGroup: "vip", UsingGroup: "default", ChannelMeta: &relaycommon.ChannelMeta{ChannelId: 54, ChannelType: 54, ChannelBaseUrl: srv.URL, ApiKey: "test-key"}}
				info.UserSetting.AcceptUnsetRatioModel = true
				result, taskErr := relayPluginTaskSubmitWithExecutor(c, info, "54", m, host.Executor{Client: srv.Client(), ValidateURL: func(string) error { return nil }})
				require.Nil(t, taskErr)
				return result
			}
			result := submit()
			var saved model.Task
			require.NoError(t, model.DB.First(&saved, result.SavedTask.ID).Error)
			snapshot := saved.PrivateData.PluginContext.TokenBilling
			require.NotNil(t, snapshot)
			require.Equal(t, 1, snapshot.Version)
			if mode == "configured" || mode == "structured" {
				require.Equal(t, "tokens", snapshot.Mode)
				require.Equal(t, 0.5, snapshot.ModelRatio)
				require.InDelta(t, 0.3, *snapshot.GroupRatio, 1e-12)
			} else {
				require.Equal(t, "reserved", snapshot.Mode)
			}
			if mode == "structured" {
				var selected billing_setting.TaskBillingRule
				require.NoError(t, json.Unmarshal(saved.PrivateData.PluginContext.PriceRule, &selected))
				require.Equal(t, "plugin_original", selected.Source)
				require.NotNil(t, selected.Rule)
				require.Equal(t, "token_completion", selected.Rule.Kind)
				require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{"billing_setting.task_expressions": `{}`}))
			}
			require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"task-token-alias":99}`))
			require.NoError(t, ratio_setting.UpdateGroupGroupRatioByJSONString(`{"vip":{"default":99}}`))
			replayed := submit()
			require.Equal(t, result.SavedTask.ID, replayed.SavedTask.ID)
			require.Equal(t, snapshot, replayed.SavedTask.PrivateData.PluginContext.TokenBilling)
			require.Equal(t, saved.PrivateData.PluginContext.PriceRule, replayed.SavedTask.PrivateData.PluginContext.PriceRule)
			require.EqualValues(t, 1, calls.Load())
		})
	}
}
