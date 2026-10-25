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
	"eflow.local/new-api/model"
	"eflow.local/new-api/plugins"
	host "eflow.local/new-api/relay/channel/task/jsplugin"
	relaycommon "eflow.local/new-api/relay/common"
	management "eflow.local/new-api/service/taskplugin"
	"eflow.local/new-api/setting/config"
	"eflow.local/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestPluginMappedModelKeepsPricingAndIdentityMySQL(t *testing.T) {
	for _, mode := range []string{"price", "fractional_price", "zero_price", "expression", "usage_expression", "invalid_usage", "missing_price", "plugin_original", "plugin_mapped", "model_mapped", "plugin_zero", "plugin_invalid", "structured_unit", "structured_zero", "structured_fixed", "structured_mismatch"} {
		t.Run(mode, func(t *testing.T) {
			oldPrices, oldRatios := ratio_setting.ModelPrice2JSONString(), ratio_setting.ModelRatio2JSONString()
			t.Cleanup(func() {
				require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(oldPrices))
				require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(oldRatios))
			})
			savedConfig := map[string]string{}
			require.NoError(t, config.GlobalConfig.SaveToDB(func(k, v string) error { savedConfig[k] = v; return nil }))
			t.Cleanup(func() { require.NoError(t, config.GlobalConfig.LoadFromDB(savedConfig)) })
			prices := `{"Local-WAN":0.001,"wan3.0-video":99}`
			if mode == "fractional_price" {
				prices = `{"Local-WAN":0.0000019,"wan3.0-video":99}`
			}
			if mode == "zero_price" {
				prices = `{"Local-WAN":0,"wan3.0-video":99}`
			} else if mode == "missing_price" || mode == "plugin_mapped" || mode == "model_mapped" {
				prices = `{"wan3.0-video":99}`
			}
			require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(prices))
			require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{}`))
			modes := map[string]string{"wan3.0-video": "tiered_expr"}
			expressions := map[string]string{"wan3.0-video": `tier("tail-must-not-be-used", 99000000)`}
			if mode == "expression" {
				modes["Local-WAN"] = "tiered_expr"
				expressions["Local-WAN"] = `tier("alias", param("duration") * 600)`
			}
			if mode == "usage_expression" || mode == "invalid_usage" {
				modes["Local-WAN"] = "tiered_expr"
				expressions["Local-WAN"] = `v2:u("seconds") * 0.01`
				if mode == "invalid_usage" {
					expressions["Local-WAN"] = `v2:false ? u("undeclared") : 0`
				}
			}
			if mode == "missing_price" {
				modes = map[string]string{}
				expressions = map[string]string{}
			}
			if mode == "model_mapped" {
				expressions["wan3.0-video"] = `v2:u("seconds") * 0.008`
			}
			taskExpressions := map[string]map[string]string{"wan3.0-video": {"alibaba": `v2:99`}}
			if mode == "missing_price" || mode == "model_mapped" {
				taskExpressions = map[string]map[string]string{}
			}
			if mode == "plugin_original" || mode == "plugin_zero" || mode == "plugin_invalid" {
				expr := `v2:u("seconds") * 0.006`
				if mode == "plugin_zero" {
					expr = `v2:0`
				}
				if mode == "plugin_invalid" {
					expr = `v2:u("undeclared")`
				}
				taskExpressions["Local-WAN"] = map[string]string{"alibaba": expr, "sora": `v2:98`}
			}
			if mode == "plugin_mapped" {
				taskExpressions["wan3.0-video"]["alibaba"] = `v2:u("seconds") * 0.004`
			}
			taskJSON, err := json.Marshal(taskExpressions)
			require.NoError(t, err)
			require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{"billing_setting.task_expressions": string(taskJSON)}))
			if strings.HasPrefix(mode, "structured_") {
				price, dimensions, settlement, tail := 0.000003, "provider_v1", "wan_duration", "wan3.0-video"
				if mode == "structured_zero" {
					price = 0
				}
				if mode == "structured_fixed" {
					dimensions, settlement = "none", "submit"
				}
				if mode == "structured_mismatch" {
					tail = "wan3.0-video-prime"
				}
				ruleJSON, err := json.Marshal(map[string]any{"Local-WAN": map[string]any{"alibaba": map[string]any{"version": 1, "kind": "unit_price", "unit_price": price, "upstream_model": tail, "dimensions": dimensions, "settlement": settlement}}})
				require.NoError(t, err)
				require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{"billing_setting.task_expressions": string(ruleJSON)}))
			}
			modeJSON, err := json.Marshal(modes)
			require.NoError(t, err)
			exprJSON, err := json.Marshal(expressions)
			require.NoError(t, err)
			require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{"billing_setting.billing_mode": string(modeJSON), "billing_setting.billing_expr": string(exprJSON)}))
			source, err := plugins.Source("alibaba")
			require.NoError(t, err)
			m := durableTestManager(t, "alibaba", source, 17)
			require.NoError(t, m.BindProduction(t.Context(), 17, management.ProductionBindingRequest{Key: "alibaba", Version: "1.3.0", Factory: true, Enabled: true}))
			var calls atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				var sent map[string]any
				require.NoError(t, json.NewDecoder(r.Body).Decode(&sent))
				require.Equal(t, "wan3.0-video", sent["model"])
				_, _ = io.WriteString(w, `{"output":{"task_id":"accepted-canonical"}}`)
			}))
			defer srv.Close()
			submit := func(mapping string) (*TaskSubmitResult, string) {
				c, _ := gin.CreateTestContext(httptest.NewRecorder())
				c.Request = httptest.NewRequest("POST", "/v1/videos", strings.NewReader(`{"model":"Local-WAN","prompt":"test","duration":5,"resolution":"720P","usage":{"seconds":999}}`))
				c.Request.Header.Set("Content-Type", "application/json")
				c.Request.Header.Set("Idempotency-Key", "canonical-once")
				c.Set("model_mapping", mapping)
				info := &relaycommon.RelayInfo{UserId: 1, TokenId: 1, OriginModelName: "Local-WAN", UsingGroup: "default", ChannelMeta: &relaycommon.ChannelMeta{ChannelId: 17, ChannelType: 17, ChannelBaseUrl: srv.URL, ApiKey: "test-key"}}
				result, taskErr := relayPluginTaskSubmitWithExecutor(c, info, "17", m, host.Executor{Client: srv.Client(), ValidateURL: func(string) error { return nil }})
				require.Equal(t, "Local-WAN", info.OriginModelName)
				if taskErr != nil {
					require.True(t, taskErr.SkipRetry)
					return result, taskErr.Code
				}
				return result, ""
			}
			result, code := submit(`{"Local-WAN":"hop","hop":"WAN3.0-VIDEO"}`)
			if mode == "missing_price" || mode == "invalid_usage" || mode == "plugin_invalid" || mode == "structured_mismatch" {
				require.Equal(t, "model_price_error", code)
				require.Nil(t, result)
				require.Zero(t, calls.Load())
				var tasks int64
				require.NoError(t, model.DB.Model(&model.Task{}).Count(&tasks).Error)
				require.Zero(t, tasks)
				var user model.User
				require.NoError(t, model.DB.First(&user, 1).Error)
				require.Equal(t, 100000000, user.Quota)
				return
			}
			require.Empty(t, code)
			require.NotNil(t, result.SavedTask)
			expected := int(0.001 * common.GetQuotaPerUnit() * 5 * 2)
			baseQuota, billingMode := int(0.001*common.GetQuotaPerUnit()), "duration"
			if mode == "fractional_price" {
				expected, baseQuota = 0, 0
			}
			if mode == "zero_price" {
				expected, baseQuota = 0, 0
			} else if mode == "expression" {
				expected = int(3000.0 / 1000000 * common.GetQuotaPerUnit())
				baseQuota, billingMode = expected, "fixed"
			} else if mode == "usage_expression" {
				expected = int(0.05 * common.GetQuotaPerUnit())
				baseQuota, billingMode = expected, "fixed"
			}
			if mode == "structured_unit" {
				expected, baseQuota = 10, 1
			}
			if mode == "structured_zero" {
				expected, baseQuota = 0, 0
			}
			if mode == "structured_fixed" {
				expected, baseQuota, billingMode = 1, 1, "fixed"
			}
			sourceKind, priceModel := "", "Local-WAN"
			switch mode {
			case "plugin_original":
				expected, sourceKind = int(0.03*common.GetQuotaPerUnit()), "plugin_original"
			case "plugin_zero":
				expected, sourceKind = 0, "plugin_original"
			case "plugin_mapped":
				expected, sourceKind, priceModel = int(0.02*common.GetQuotaPerUnit()), "plugin_mapped", "wan3.0-video"
			case "model_mapped":
				expected, sourceKind, priceModel = int(0.04*common.GetQuotaPerUnit()), "model_mapped", "wan3.0-video"
			}
			if sourceKind != "" {
				baseQuota, billingMode = expected, "fixed"
			}
			require.Equal(t, expected, result.Quota)
			if strings.HasPrefix(mode, "structured_") {
				require.NotEmpty(t, result.SavedTask.PrivateData.PluginContext.PriceRule)
				require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{"billing_setting.task_expressions": `{"Local-WAN":{"alibaba":"v2:99"}}`}))
			}
			if mode == "usage_expression" {
				require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{"billing_setting.billing_expr": `{"Local-WAN":"v2:u(\"seconds\") * 10"}`}))
			}
			if sourceKind != "" {
				require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{"billing_setting.task_expressions": `{"Local-WAN":{"alibaba":"v2:99"}}`}))
			}
			// Replaying the same public payload must not apply a newly changed mapping.
			replayed, code := submit(`{"Local-WAN":"wan3.0-video-prime"}`)
			require.Empty(t, code)
			require.Equal(t, result.SavedTask.ID, replayed.SavedTask.ID)
			require.EqualValues(t, 1, calls.Load())
			var task model.Task
			require.NoError(t, model.DB.First(&task, result.SavedTask.ID).Error)
			require.Equal(t, "Local-WAN", task.Properties.OriginModelName)
			require.Equal(t, "wan3.0-video", task.Properties.UpstreamModelName)
			require.Equal(t, "Local-WAN", task.PrivateData.BillingContext.OriginModelName)
			require.NotNil(t, task.PrivateData.PluginContext.Billing, "normalized WAN model must retain completion billing")
			require.Equal(t, billingMode, task.PrivateData.PluginContext.Billing.Mode)
			require.Equal(t, baseQuota, task.PrivateData.PluginContext.Billing.BaseQuota)
			require.Equal(t, float64(5), task.PrivateData.PluginContext.Usage["seconds"])
			if mode == "usage_expression" {
				require.NotNil(t, task.PrivateData.PluginContext.Expression)
				require.Equal(t, expressions["Local-WAN"], task.PrivateData.PluginContext.Expression.ExprString)
				require.Equal(t, expected, replayed.Quota)
			}
			if sourceKind != "" {
				require.NotNil(t, task.PrivateData.PluginContext.Expression)
				require.Equal(t, sourceKind, task.PrivateData.PluginContext.Expression.PriceSource)
				require.Equal(t, priceModel, task.PrivateData.PluginContext.Expression.PriceModel)
				require.Equal(t, "alibaba", task.PrivateData.PluginContext.Expression.PricePlugin)
				require.Equal(t, expected, replayed.Quota)
			}
			var user model.User
			var token model.Token
			require.NoError(t, model.DB.First(&user, 1).Error)
			require.NoError(t, model.DB.First(&token, 1).Error)
			require.Equal(t, 100000000-expected, user.Quota)
			require.Equal(t, user.Quota, token.RemainQuota)
			var response map[string]any
			require.NoError(t, json.Unmarshal(pluginVideoResponse(&task), &response))
			require.Equal(t, "Local-WAN", response["model"])
		})
	}
}
