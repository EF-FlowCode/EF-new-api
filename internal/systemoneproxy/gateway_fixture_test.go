package systemoneproxy_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"

	"eflow.local/new-api/common"
	"eflow.local/new-api/constant"
	"eflow.local/new-api/dto"
	"eflow.local/new-api/i18n"
	"eflow.local/new-api/internal/systemoneproxy"
	"eflow.local/new-api/internal/testdb"
	"eflow.local/new-api/middleware"
	"eflow.local/new-api/model"
	"eflow.local/new-api/router"
	"eflow.local/new-api/service"
	"eflow.local/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

const systemOneMinimal = `{"model":"jev-1.13","state":"Please refund the duplicate charge.","questions":{"refund_requested":{"type":"noul","instructions":"Does the customer explicitly request a refund?"}}}`
const systemOneAllQuestions = `{"model":"decision-alias","state":{"text":"Please refund the duplicate charge."},"questions":{"refund_requested":{"type":"noul","instructions":"Refund requested?"},"team":{"type":"choice","instructions":"Which team?","criteria":{"billing":"Billing questions","other":null}},"urgency":{"type":"score","instructions":"How urgent?","criteria":["low","high"]}},"extension":{"exact":9007199254740993}}`
const systemOneAnswers = `{"model":"typesafe/jev-1.13-20260917","answers":{"refund_requested":{"type":"noul","noul":0.99,"confidence":0.9},"team":{"type":"choice","choice":"billing","probabilities":{"billing":0.9,"other":0.1}},"urgency":{"type":"score","score":0.8,"legend":["low","high"]}},"usage":{"prompt_tokens":14,"input_tokens":0,"completion_tokens":3,"output_tokens":0},"extension":{"exact":9007199254740993}}`

type systemOneFixture struct {
	inFlight sync.WaitGroup
	engine   *gin.Engine
	user     model.User
	token    model.Token
}

func setupSystemOne(t *testing.T) *systemOneFixture {
	t.Helper()
	gin.SetMode(gin.TestMode)
	db := testdb.OpenMySQL8(t, &model.User{}, &model.Token{}, &model.Channel{}, &model.Ability{}, &model.Log{}, &model.Option{})
	oldDB, oldLogDB := model.DB, model.LOG_DB
	oldRedis, oldMemory, oldBatch := common.RedisEnabled, common.MemoryCacheEnabled, common.BatchUpdateEnabled
	oldFeatures := common.GetFeatureSettings()
	oldRatio, oldCompletion, oldGroup := ratio_setting.ModelRatio2JSONString(), ratio_setting.CompletionRatio2JSONString(), ratio_setting.GroupRatio2JSONString()
	model.DB, model.LOG_DB = db, db
	common.RedisEnabled, common.MemoryCacheEnabled, common.BatchUpdateEnabled = false, false, false
	require.NoError(t, common.UpdateFeatureSettings(map[string]string{"LogConsumeEnabled": "true", "DataExportEnabled": "false"}))
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"jev-1.13":1,"decision-alias":1}`))
	require.NoError(t, ratio_setting.UpdateCompletionRatioByJSONString(`{"jev-1.13":2,"decision-alias":2}`))
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":1}`))
	t.Cleanup(func() {
		model.DB, model.LOG_DB = oldDB, oldLogDB
		common.RedisEnabled, common.MemoryCacheEnabled, common.BatchUpdateEnabled = oldRedis, oldMemory, oldBatch
		_ = common.UpdateFeatureSettings(map[string]string{"LogConsumeEnabled": strconv.FormatBool(oldFeatures.LogConsumeEnabled), "DataExportEnabled": strconv.FormatBool(oldFeatures.DataExportEnabled)})
		_ = ratio_setting.UpdateModelRatioByJSONString(oldRatio)
		_ = ratio_setting.UpdateCompletionRatioByJSONString(oldCompletion)
		_ = ratio_setting.UpdateGroupRatioByJSONString(oldGroup)
	})
	require.NoError(t, i18n.Init())
	service.InitHttpClient()
	service.InitTokenEncoders()
	f := &systemOneFixture{user: model.User{Username: "systemoneuser", Status: common.UserStatusEnabled, Group: "default", Quota: 100000}, token: model.Token{Key: "systemonetestkey", Status: common.TokenStatusEnabled, ExpiredTime: -1, RemainQuota: 100000}}
	require.NoError(t, db.Create(&f.user).Error)
	f.token.UserId = f.user.Id
	require.NoError(t, db.Create(&f.token).Error)
	f.engine = gin.New()
	f.engine.Use(middleware.RequestId())
	router.SetRelayRouter(f.engine)
	return f
}

func (f *systemOneFixture) channel(t *testing.T, base, key string, channelType int, settings dto.ChannelOtherSettings) *model.Channel {
	t.Helper()
	raw, err := json.Marshal(settings)
	require.NoError(t, err)
	mapping := `{"decision-alias":"jev-1.13"}`
	setting := `{"force_format":true,"pass_through_body_enabled":false}`
	channel := &model.Channel{Type: channelType, Status: common.ChannelStatusEnabled, Name: "systemone", Key: key, BaseURL: &base, Models: "jev-1.13,decision-alias", Group: "default", OtherSettings: string(raw), ModelMapping: &mapping, Setting: &setting, AutoBan: common.GetPointer(0)}
	require.NoError(t, channel.ValidateSettings())
	require.NoError(t, model.DB.Create(channel).Error)
	require.NoError(t, channel.AddAbilities(model.DB))
	return channel
}

func (f *systemOneFixture) assertBilling(t *testing.T, amount, logCount int) []model.Log {
	t.Helper()
	// Chat can flush its HTTP body before its synchronous settlement finishes.
	// Wait for the real gateway handler, not just the proxy's response body.
	f.inFlight.Wait()
	var user model.User
	var token model.Token
	require.NoError(t, model.DB.First(&user, f.user.Id).Error)
	require.NoError(t, model.DB.First(&token, f.token.Id).Error)
	require.Equal(t, f.user.Quota-amount, user.Quota, "wallet settlement")
	require.Equal(t, f.token.RemainQuota-amount, token.RemainQuota, "token settlement")
	var logs []model.Log
	require.NoError(t, model.LOG_DB.Where("type = ?", model.LogTypeConsume).Find(&logs).Error)
	require.Len(t, logs, logCount)
	return logs
}

func (f *systemOneFixture) proxy(t *testing.T, endpoint, protocol, key string, liveCredential ...string) http.Handler {
	t.Helper()
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.inFlight.Add(1)
		defer f.inFlight.Done()
		f.engine.ServeHTTP(w, r)
	}))
	t.Cleanup(gateway.Close)
	t.Cleanup(f.inFlight.Wait)
	p, err := systemoneproxy.New(systemoneproxy.Config{GatewayChatEndpoint: gateway.URL + "/v1/chat/completions", Targets: map[string]systemoneproxy.Target{"test": {Protocol: protocol, Endpoint: endpoint}}})
	require.NoError(t, err)
	upstreamHandler := p.UpstreamHandler()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Live smoke tests keep the real key in process memory only. The test
		// database and the gateway itself see a dummy channel key.
		if len(liveCredential) > 0 {
			if r.Header.Get("Authorization") != "Bearer "+key {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			r.Header.Set("Authorization", "Bearer "+liveCredential[0])
		}
		upstreamHandler.ServeHTTP(w, r)
	}))
	t.Cleanup(upstream.Close)
	f.channel(t, upstream.URL+"/test", key, constant.ChannelTypeOpenAI, dto.ChannelOtherSettings{SupportedEndpointTypes: []string{"openai"}})
	return p.PublicHandler()
}

func nativeRequest(handler http.Handler, body, key string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodPost, "/v1/systemone", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	if key != "" {
		r.Header.Set("Authorization", "Bearer "+key)
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	return w
}
