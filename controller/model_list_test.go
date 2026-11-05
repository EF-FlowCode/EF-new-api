package controller

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"eflow.local/new-api/common"
	"eflow.local/new-api/constant"
	"eflow.local/new-api/dto"
	"eflow.local/new-api/internal/testdb"
	"eflow.local/new-api/model"
	"eflow.local/new-api/setting"
	"eflow.local/new-api/setting/config"
	"eflow.local/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type userModelsResponse struct {
	Success bool     `json:"success"`
	Data    []string `json:"data"`
}

type listModelsResponse struct {
	Success bool               `json:"success"`
	Data    []dto.OpenAIModels `json:"data"`
	Object  string             `json:"object"`
}

func setupModelListControllerTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	gin.SetMode(gin.TestMode)
	db := testdb.OpenMySQL8(t, &model.User{}, &model.Channel{}, &model.Ability{}, &model.Model{}, &model.Vendor{})
	oldDB, oldLogDB := model.DB, model.LOG_DB
	oldRedisEnabled := common.RedisEnabled
	model.DB, model.LOG_DB = db, db
	common.RedisEnabled = false

	t.Cleanup(func() {
		model.DB, model.LOG_DB = oldDB, oldLogDB
		common.RedisEnabled = oldRedisEnabled
	})

	return db
}

func withTieredBillingConfig(t *testing.T, modes map[string]string, exprs map[string]string) {
	t.Helper()

	saved := map[string]string{}
	require.NoError(t, config.GlobalConfig.SaveToDB(func(key, value string) error {
		if strings.HasPrefix(key, "billing_setting.") {
			saved[key] = value
		}
		return nil
	}))
	t.Cleanup(func() {
		require.NoError(t, config.GlobalConfig.LoadFromDB(saved))
		model.InvalidatePricingCache()
	})

	modeBytes, err := common.Marshal(modes)
	require.NoError(t, err)
	exprBytes, err := common.Marshal(exprs)
	require.NoError(t, err)

	require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{
		"billing_setting.billing_mode": string(modeBytes),
		"billing_setting.billing_expr": string(exprBytes),
	}))
	model.InvalidatePricingCache()
}

func withSelfUseModeDisabled(t *testing.T) {
	t.Helper()

	original := operation_setting.GetSiteModeSettings()
	require.NoError(t, operation_setting.UpdateSiteModeSettings(map[string]string{"SelfUseModeEnabled": "false"}))
	t.Cleanup(func() {
		require.NoError(t, operation_setting.UpdateSiteModeSettings(map[string]string{
			"DemoSiteEnabled":    strconv.FormatBool(original.DemoSiteEnabled),
			"SelfUseModeEnabled": strconv.FormatBool(original.SelfUseModeEnabled),
		}))
	})
}

func decodeListModelsResponse(t *testing.T, recorder *httptest.ResponseRecorder) map[string]struct{} {
	t.Helper()

	require.Equal(t, http.StatusOK, recorder.Code)
	var payload listModelsResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &payload))
	require.True(t, payload.Success)
	require.Equal(t, "list", payload.Object)

	ids := make(map[string]struct{}, len(payload.Data))
	for _, item := range payload.Data {
		ids[item.Id] = struct{}{}
	}
	return ids
}

func pricingByModelName(pricings []model.Pricing) map[string]model.Pricing {
	byName := make(map[string]model.Pricing, len(pricings))
	for _, pricing := range pricings {
		byName[pricing.ModelName] = pricing
	}
	return byName
}

func TestListModelsIncludesTieredBillingModel(t *testing.T) {
	withSelfUseModeDisabled(t)
	withTieredBillingConfig(t, map[string]string{
		"zz-tiered-visible-model":      "tiered_expr",
		"zz-dynamic-visible-model":     "tiered_expr",
		"zz-flat-visible-model":        "flat_price",
		"zz-tiered-empty-expr-model":   "tiered_expr",
		"zz-tiered-missing-expr-model": "tiered_expr",
	}, map[string]string{
		"zz-tiered-visible-model":    `tier("base", p * 1 + c * 2)`,
		"zz-dynamic-visible-model":   `tier("base", p * 1 + c * 2) * (hour("UTC") >= 8 ? 2 : 1)`,
		"zz-flat-visible-model":      `tier("default", p * 3 + c * 15 + cr * 1 + cc * 3.75 + cc1h * 6)`,
		"zz-tiered-empty-expr-model": "   ",
	})

	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.Create(&model.User{
		Id:       1001,
		Username: "model-list-user",
		Password: "password",
		Group:    "default",
		Status:   common.UserStatusEnabled,
	}).Error)
	require.NoError(t, db.Create(&[]model.Ability{
		{Group: "default", Model: "zz-tiered-visible-model", ChannelId: 1, Enabled: true},
		{Group: "default", Model: "zz-dynamic-visible-model", ChannelId: 1, Enabled: true},
		{Group: "default", Model: "zz-flat-visible-model", ChannelId: 1, Enabled: true},
		{Group: "default", Model: "zz-tiered-empty-expr-model", ChannelId: 1, Enabled: true},
		{Group: "default", Model: "zz-tiered-missing-expr-model", ChannelId: 1, Enabled: true},
		{Group: "default", Model: "zz-unpriced-model", ChannelId: 1, Enabled: true},
	}).Error)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	ctx.Set("id", 1001)

	ListModels(ctx, constant.ChannelTypeOpenAI)

	ids := decodeListModelsResponse(t, recorder)
	require.Contains(t, ids, "zz-tiered-visible-model")
	require.Contains(t, ids, "zz-dynamic-visible-model")
	require.Contains(t, ids, "zz-flat-visible-model")
	require.NotContains(t, ids, "zz-tiered-empty-expr-model")
	require.NotContains(t, ids, "zz-tiered-missing-expr-model")
	require.NotContains(t, ids, "zz-unpriced-model")

	pricingByName := pricingByModelName(model.GetPricing())
	visiblePricing, ok := pricingByName["zz-tiered-visible-model"]
	require.True(t, ok)
	require.Equal(t, "flat_price", visiblePricing.BillingMode)
	require.NotEmpty(t, visiblePricing.BillingExpr)
	dynamicPricing, ok := pricingByName["zz-dynamic-visible-model"]
	require.True(t, ok)
	require.Equal(t, "tiered_expr", dynamicPricing.BillingMode)
	flatPricing, ok := pricingByName["zz-flat-visible-model"]
	require.True(t, ok)
	require.Equal(t, "flat_price", flatPricing.BillingMode)
	require.Contains(t, flatPricing.BillingExpr, "cc1h * 6")

	emptyExprPricing, ok := pricingByName["zz-tiered-empty-expr-model"]
	require.True(t, ok)
	require.Empty(t, emptyExprPricing.BillingMode)
	require.Empty(t, emptyExprPricing.BillingExpr)

	missingExprPricing, ok := pricingByName["zz-tiered-missing-expr-model"]
	require.True(t, ok)
	require.Empty(t, missingExprPricing.BillingMode)
	require.Empty(t, missingExprPricing.BillingExpr)
}

func TestListModelsTokenLimitIncludesTieredBillingModel(t *testing.T) {
	withSelfUseModeDisabled(t)
	setupModelListControllerTestDB(t)
	withTieredBillingConfig(t, map[string]string{
		"zz-token-tiered-visible-model":      "tiered_expr",
		"zz-token-tiered-empty-expr-model":   "tiered_expr",
		"zz-token-tiered-missing-expr-model": "tiered_expr",
	}, map[string]string{
		"zz-token-tiered-visible-model":    `tier("base", p * 1 + c * 2)`,
		"zz-token-tiered-empty-expr-model": "",
	})

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	common.SetContextKey(ctx, constant.ContextKeyTokenModelLimitEnabled, true)
	common.SetContextKey(ctx, constant.ContextKeyUserGroup, "default")
	common.SetContextKey(ctx, constant.ContextKeyTokenModelLimit, map[string]bool{
		"zz-token-tiered-visible-model":      true,
		"zz-token-tiered-empty-expr-model":   true,
		"zz-token-tiered-missing-expr-model": true,
		"zz-token-unpriced-model":            true,
	})

	ListModels(ctx, constant.ChannelTypeOpenAI)

	ids := decodeListModelsResponse(t, recorder)
	require.Contains(t, ids, "zz-token-tiered-visible-model")
	require.NotContains(t, ids, "zz-token-tiered-empty-expr-model")
	require.NotContains(t, ids, "zz-token-tiered-missing-expr-model")
	require.NotContains(t, ids, "zz-token-unpriced-model")
}

func decodeUserModelsResponse(t *testing.T, recorder *httptest.ResponseRecorder) []string {
	t.Helper()

	require.Equal(t, http.StatusOK, recorder.Code)
	var payload userModelsResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &payload))
	require.True(t, payload.Success)
	return payload.Data
}

func requestUserModels(t *testing.T, userID int, query string) []string {
	t.Helper()

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/user/models"+query, nil)
	ctx.Set("id", userID)
	GetUserModels(ctx)
	return decodeUserModelsResponse(t, recorder)
}

func TestGetUserModelsFiltersByRequestedGroup(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.Create(&model.User{
		Id:       1002,
		Username: "playground-model-user",
		Password: "password",
		Group:    "default",
		Status:   common.UserStatusEnabled,
	}).Error)
	require.NoError(t, db.Create(&[]model.Ability{
		{Group: "default", Model: "zz-default-only-model", ChannelId: 1, Enabled: true},
		{Group: "default", Model: "zz-disabled-model", ChannelId: 1, Enabled: false},
	}).Error)

	require.ElementsMatch(t, []string{"zz-default-only-model"}, requestUserModels(t, 1002, "?group=default"))
	require.Empty(t, requestUserModels(t, 1002, "?group=vip"))
	require.ElementsMatch(t, []string{"zz-default-only-model"}, requestUserModels(t, 1002, ""))
}

func TestGetUserModelsExpandsAutoGroupsInConfiguredOrder(t *testing.T) {
	originalAutoGroups := setting.AutoGroups2JsonString()
	originalUsableGroups := setting.UserUsableGroups2JSONString()
	savedGroupRatioSetting := map[string]string{}
	require.NoError(t, config.GlobalConfig.SaveToDB(func(key, value string) error {
		if strings.HasPrefix(key, "group_ratio_setting.") {
			savedGroupRatioSetting[key] = value
		}
		return nil
	}))
	t.Cleanup(func() {
		require.NoError(t, config.GlobalConfig.LoadFromDB(savedGroupRatioSetting))
		require.NoError(t, setting.UpdateAutoGroupsByJsonString(originalAutoGroups))
		require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(originalUsableGroups))
	})

	require.NoError(t, setting.UpdateAutoGroupsByJsonString(`["vip","default","unavailable"]`))
	require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(`{"auto":"自动分组","default":"默认分组","unavailable":"不可用分组"}`))
	// eflow 只把配置了倍率的分组算作有效令牌分组；分组配置走快照，需经 GlobalConfig 写入。
	require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{
		"group_ratio_setting.group_ratio":                `{"default":1,"vip":1,"unavailable":1}`,
		"group_ratio_setting.group_special_usable_group": `{"default":{"+:vip":"VIP 分组","-:unavailable":""}}`,
	}))

	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.Create(&model.User{
		Id:       1003,
		Username: "playground-auto-model-user",
		Password: "password",
		Group:    "default",
		Status:   common.UserStatusEnabled,
	}).Error)
	require.NoError(t, db.Create(&[]model.Ability{
		{Group: "vip", Model: "zz-vip-model", ChannelId: 1, Enabled: true},
		{Group: "vip", Model: "zz-shared-model", ChannelId: 1, Enabled: true},
		{Group: "default", Model: "zz-default-model", ChannelId: 1, Enabled: true},
		{Group: "default", Model: "zz-shared-model", ChannelId: 2, Enabled: true},
		{Group: "unavailable", Model: "zz-unavailable-model", ChannelId: 1, Enabled: true},
	}).Error)

	models := requestUserModels(t, 1003, "?group=auto")
	require.Len(t, models, 3)
	require.ElementsMatch(t, []string{"zz-vip-model", "zz-shared-model"}, models[:2])
	require.Equal(t, "zz-default-model", models[2])
}
