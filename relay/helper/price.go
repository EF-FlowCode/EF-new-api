package helper

import (
	"fmt"
	"maps"
	"strings"

	"eflow.local/new-api/common"
	"eflow.local/new-api/logger"
	"eflow.local/new-api/model"
	"eflow.local/new-api/pkg/billingexpr"
	"eflow.local/new-api/pkg/jsplugin"
	relaycommon "eflow.local/new-api/relay/common"
	"eflow.local/new-api/service"
	"eflow.local/new-api/setting/billing_setting"
	"eflow.local/new-api/setting/operation_setting"
	"eflow.local/new-api/setting/ratio_setting"
	"eflow.local/new-api/types"

	"github.com/gin-gonic/gin"
)

func modelPriceNotConfiguredError(modelName string, userId int) error {
	if model.IsAdmin(userId) {
		return fmt.Errorf(
			"模型 %s 的价格未配置。请前往「系统设置 → 运营设置」开启自用模式，或在「系统设置 → 分组与模型定价设置」中为该模型配置价格；"+
				"Model %s price not configured. Go to System Settings → Operation Settings to enable self-use mode, or configure the model price in System Settings → Group & Model Pricing.",
			modelName, modelName,
		)
	}
	return fmt.Errorf(
		"模型 %s 的价格尚未由管理员配置，暂时无法使用，请联系站点管理员开启该模型；"+
			"Model %s has not been priced by the administrator yet. Please contact the site administrator to enable this model.",
		modelName, modelName,
	)
}

// https://docs.claude.com/en/docs/build-with-claude/prompt-caching#1-hour-cache-duration
const claudeCacheCreation1hMultiplier = 6 / 3.75

// HandleGroupRatio checks for "auto_group" in the context and updates the group ratio and relayInfo.UsingGroup if present
func HandleGroupRatio(ctx *gin.Context, relayInfo *relaycommon.RelayInfo) types.GroupRatioInfo {
	groupRatioInfo := types.GroupRatioInfo{
		GroupRatio:        1.0, // default ratio
		GroupSpecialRatio: -1,
	}

	// check auto group
	autoGroup, exists := ctx.Get("auto_group")
	if exists {
		logger.LogDebug(ctx, "final group: %s", autoGroup)
		relayInfo.UsingGroup = autoGroup.(string)
	}

	// check user group special ratio
	userGroupRatio, ok := service.GetUserGroupRatio(relayInfo.UserGroup, relayInfo.AdditionalCustomerTiers, relayInfo.UsingGroup)
	if ok {
		// user group special ratio
		groupRatioInfo.GroupSpecialRatio = userGroupRatio
		groupRatioInfo.GroupRatio = userGroupRatio
		groupRatioInfo.HasSpecialRatio = true
	} else {
		// normal group ratio
		groupRatioInfo.GroupRatio = ratio_setting.GetGroupRatio(relayInfo.UsingGroup)
	}

	return groupRatioInfo
}

func ModelPriceHelper(c *gin.Context, info *relaycommon.RelayInfo, promptTokens int, meta *types.TokenCountMeta) (types.PriceData, error) {
	modelPrice, usePrice := ratio_setting.GetModelPrice(info.OriginModelName, false)

	groupRatioInfo := HandleGroupRatio(c, info)

	// Flat token prices and tiered prices share the expression billing engine.
	billingMode, billingExpr, hasBillingExpr := billing_setting.GetBillingRule(info.OriginModelName)
	if billing_setting.IsExpressionBillingMode(billingMode) {
		return modelPriceHelperTiered(c, info, promptTokens, meta, groupRatioInfo, billingMode, billingExpr, hasBillingExpr)
	}

	var preConsumedQuota int
	var modelRatio float64
	var completionRatio float64
	var cacheRatio float64
	var imageRatio float64
	var cacheCreationRatio float64
	var cacheCreationRatio5m float64
	var cacheCreationRatio1h float64
	var audioRatio float64
	var audioCompletionRatio float64
	var freeModel bool
	if !usePrice {
		preConsumedTokens := common.Max(promptTokens, common.GetUserQuotaSettings().PreConsumedQuota)
		if meta.MaxTokens != 0 {
			preConsumedTokens += meta.MaxTokens
		}
		var success bool
		var matchName string
		modelRatio, success, matchName = ratio_setting.GetModelRatio(info.OriginModelName)
		if !success {
			acceptUnsetRatio := false
			if info.UserSetting.AcceptUnsetRatioModel {
				acceptUnsetRatio = true
			}
			if !acceptUnsetRatio {
				return types.PriceData{}, modelPriceNotConfiguredError(matchName, info.UserId)
			}
		}
		completionRatio = ratio_setting.GetCompletionRatio(info.OriginModelName)
		cacheRatio, _ = ratio_setting.GetCacheRatio(info.OriginModelName)
		cacheCreationRatio, _ = ratio_setting.GetCreateCacheRatio(info.OriginModelName)
		cacheCreationRatio5m = cacheCreationRatio
		// 固定1h和5min缓存写入价格的比例
		cacheCreationRatio1h = cacheCreationRatio * claudeCacheCreation1hMultiplier
		imageRatio, _ = ratio_setting.GetImageRatio(info.OriginModelName)
		audioRatio = ratio_setting.GetAudioRatio(info.OriginModelName)
		audioCompletionRatio = ratio_setting.GetAudioCompletionRatio(info.OriginModelName)
		ratio := modelRatio * groupRatioInfo.GroupRatio
		quota, err := common.QuotaFromFloatStrict(float64(preConsumedTokens) * ratio)
		if err != nil {
			return types.PriceData{}, err
		}
		preConsumedQuota = quota
	} else {
		if meta.ImagePriceRatio != 0 {
			modelPrice = modelPrice * meta.ImagePriceRatio
		}
	}

	// check if free model pre-consume is disabled
	if !operation_setting.GetQuotaSetting().EnableFreeModelPreConsume {
		// if model price or ratio is 0, do not pre-consume quota
		if groupRatioInfo.GroupRatio == 0 {
			preConsumedQuota = 0
			freeModel = true
		} else if usePrice {
			if modelPrice == 0 {
				preConsumedQuota = 0
				freeModel = true
			}
		} else {
			if modelRatio == 0 {
				preConsumedQuota = 0
				freeModel = true
			}
		}
	}

	priceData := types.PriceData{
		FreeModel:            freeModel,
		ModelPrice:           modelPrice,
		ModelRatio:           modelRatio,
		CompletionRatio:      completionRatio,
		GroupRatioInfo:       groupRatioInfo,
		UsePrice:             usePrice,
		CacheRatio:           cacheRatio,
		ImageRatio:           imageRatio,
		AudioRatio:           audioRatio,
		AudioCompletionRatio: audioCompletionRatio,
		CacheCreationRatio:   cacheCreationRatio,
		CacheCreation5mRatio: cacheCreationRatio5m,
		CacheCreation1hRatio: cacheCreationRatio1h,
		QuotaToPreConsume:    preConsumedQuota,
	}
	if usePrice {
		for name, ratio := range meta.BillingRatios {
			priceData.AddOtherRatio(name, ratio)
		}
		quotaToPreConsume := priceData.ApplyOtherRatiosToFloat(modelPrice * common.GetQuotaPerUnit() * groupRatioInfo.GroupRatio)
		quota, err := common.QuotaFromFloatStrict(quotaToPreConsume)
		if err != nil {
			return types.PriceData{}, err
		}
		priceData.QuotaToPreConsume = quota
	}

	if common.DebugEnabled {
		logger.LogDebug(c, "model_price_helper result: %s", priceData.ToSetting())
	}
	info.PriceData = priceData
	return priceData, nil
}

// ModelPriceHelperPerCall 按次/按量计费的 PriceHelper (MJ、Task)
func ModelPriceHelperPerCall(c *gin.Context, info *relaycommon.RelayInfo) (types.PriceData, error) {
	return modelPriceHelperPerCall(c, info, nil)
}

// Usage is supplied only by the fixed production plugin, never from client JSON.
func ModelPriceHelperPerCallWithUsage(c *gin.Context, info *relaycommon.RelayInfo, schema map[string]jsplugin.UsageFieldSchema, facts map[string]any) (types.PriceData, error) {
	return modelPriceHelperPerCall(c, info, &billingexpr.RequestInput{Usage: maps.Clone(facts), UsageSchema: jsplugin.CloneUsageSchema(schema)})
}

// The plugin key is supplied by the prepared fixed binding, never by the client.
func ModelPriceHelperForPlugin(c *gin.Context, info *relaycommon.RelayInfo, plugin string, schema map[string]jsplugin.UsageFieldSchema, facts map[string]any) (types.PriceData, error) {
	rule := billing_setting.ResolveTaskBillingRule(plugin, info.OriginModelName, info.UpstreamModelName)
	if err := rule.Validate(schema); err != nil {
		return types.PriceData{}, err
	}
	if !rule.IsExpression() {
		if rule.Rule != nil {
			if err := rule.Validate(schema); err != nil {
				return types.PriceData{}, err
			}
			group := HandleGroupRatio(c, info)
			quota, err := rule.Rule.BaseQuota(common.GetQuotaPerUnit(), group.GroupRatio)
			if err != nil {
				return types.PriceData{}, err
			}
			raw, err := common.Marshal(rule)
			if err != nil {
				return types.PriceData{}, err
			}
			price := types.PriceData{Quota: quota, GroupRatioInfo: group, TaskRuleSnapshot: raw,
				ModelPrice: -1, TaskFixedTotal: rule.Rule.Dimensions == "none"}
			if rule.Rule.Kind == "unit_price" {
				price.ModelPrice, price.UsePrice = *rule.Rule.UnitPrice, true
			} else {
				price.ModelRatio, price.TaskModelRatioConfigured = *rule.Rule.ModelRatio, *rule.Rule.RatioConfigured
			}
			if !operation_setting.GetQuotaSetting().EnableFreeModelPreConsume {
				price.FreeModel = group.GroupRatio == 0 || (price.UsePrice && price.ModelPrice == 0) || (!price.UsePrice && price.ModelRatio == 0)
			}
			return price, nil
		}
		return ModelPriceHelperPerCallWithUsage(c, info, schema, facts)
	}
	if err := rule.Validate(schema); err != nil {
		return types.PriceData{}, err
	}
	usage := &billingexpr.RequestInput{Usage: maps.Clone(facts), UsageSchema: jsplugin.CloneUsageSchema(schema)}
	price, err := modelPriceHelperTieredPerCall(c, info, HandleGroupRatio(c, info), rule.ExpressionBillingMode(), rule.Expression, true, usage)
	if err == nil {
		info.TieredBillingSnapshot.PriceSource = rule.Source
		info.TieredBillingSnapshot.PriceModel = rule.Model
		info.TieredBillingSnapshot.PricePlugin = plugin
		price.TaskRuleSnapshot, err = common.Marshal(rule)
	}
	return price, err
}

func modelPriceHelperPerCall(c *gin.Context, info *relaycommon.RelayInfo, usage *billingexpr.RequestInput) (types.PriceData, error) {
	groupRatioInfo := HandleGroupRatio(c, info)

	// eflow: per-call models (image/video tasks) can also use tiered_expr.
	// upstream's ModelPriceHelperPerCall only checks model_price / model_ratio;
	// we delegate to a per-call expr evaluator that reads `param()` from the
	// client request body (e.g. param("metadata.resolution") + param("duration"))
	// and returns the final quota in one shot — no separate settlement.
	billingMode, billingExpr, hasBillingExpr := billing_setting.GetBillingRule(info.OriginModelName)
	if billing_setting.IsExpressionBillingMode(billingMode) {
		return modelPriceHelperTieredPerCall(c, info, groupRatioInfo, billingMode, billingExpr, hasBillingExpr, usage)
	}

	modelPrice, success := ratio_setting.GetModelPrice(info.OriginModelName, true)
	usePrice := success
	var modelRatio float64
	var taskModelRatioConfigured bool

	if !success {
		defaultPrice, ok := ratio_setting.GetDefaultModelPriceMap()[info.OriginModelName]
		if ok {
			modelPrice = defaultPrice
			usePrice = true
		} else {
			var ratioSuccess bool
			var matchName string
			modelRatio, ratioSuccess, matchName = ratio_setting.GetModelRatio(info.OriginModelName)
			taskModelRatioConfigured = ratioSuccess
			acceptUnsetRatio := false
			if info.UserSetting.AcceptUnsetRatioModel {
				acceptUnsetRatio = true
			}
			if !ratioSuccess && !acceptUnsetRatio {
				return types.PriceData{}, modelPriceNotConfiguredError(matchName, info.UserId)
			}
		}
	}

	var quota int
	freeModel := false
	quotaPerUnit := common.GetQuotaPerUnit()

	if usePrice {
		var err error
		quota, err = common.QuotaFromFloatStrict(modelPrice * quotaPerUnit * groupRatioInfo.GroupRatio)
		if err != nil {
			return types.PriceData{}, err
		}
		if !operation_setting.GetQuotaSetting().EnableFreeModelPreConsume {
			if groupRatioInfo.GroupRatio == 0 || modelPrice == 0 {
				quota = 0
				freeModel = true
			}
		}
	} else {
		// 按量计费：以模型倍率的一半作为预扣额度
		var err error
		quota, err = common.QuotaFromFloatStrict(modelRatio / 2 * quotaPerUnit * groupRatioInfo.GroupRatio)
		if err != nil {
			return types.PriceData{}, err
		}
		modelPrice = -1
		if !operation_setting.GetQuotaSetting().EnableFreeModelPreConsume {
			if groupRatioInfo.GroupRatio == 0 || modelRatio == 0 {
				quota = 0
				freeModel = true
			}
		}
	}

	priceData := types.PriceData{
		FreeModel:                freeModel,
		ModelPrice:               modelPrice,
		ModelRatio:               modelRatio,
		UsePrice:                 usePrice,
		Quota:                    quota,
		GroupRatioInfo:           groupRatioInfo,
		TaskModelRatioConfigured: taskModelRatioConfigured,
	}
	return priceData, nil
}

func HasModelBillingConfig(modelName string) bool {
	if _, ok := ratio_setting.GetModelPrice(modelName, false); ok {
		return true
	}
	if _, ok, _ := ratio_setting.GetModelRatio(modelName); ok {
		return true
	}
	billingMode, billingExpr, hasBillingExpr := billing_setting.GetBillingRule(modelName)
	if !billing_setting.IsExpressionBillingMode(billingMode) {
		return false
	}
	return hasBillingExpr && strings.TrimSpace(billingExpr) != ""
}

func modelPriceHelperTiered(c *gin.Context, info *relaycommon.RelayInfo, promptTokens int, meta *types.TokenCountMeta, groupRatioInfo types.GroupRatioInfo, billingMode, exprStr string, hasExpression bool) (types.PriceData, error) {
	keys, err := billingexpr.UsedUsageKeys(exprStr)
	if err != nil {
		return types.PriceData{}, err
	}
	if len(keys) != 0 {
		return types.PriceData{}, fmt.Errorf("u() is only available for plugin task billing")
	}
	if !hasExpression {
		return types.PriceData{}, fmt.Errorf("model %s is configured as %s but has no billing expression", info.OriginModelName, billingMode)
	}

	estimatedCompletionTokens := 0
	if meta.MaxTokens != 0 {
		estimatedCompletionTokens = meta.MaxTokens
	}

	requestInput, err := ResolveIncomingBillingExprRequestInput(c, info)
	if err != nil {
		return types.PriceData{}, err
	}

	rawCost, trace, err := billingexpr.RunExprWithRequest(exprStr, billingexpr.TokenParams{
		P:   float64(promptTokens),
		C:   float64(estimatedCompletionTokens),
		Len: float64(promptTokens),
	}, requestInput)
	if err != nil {
		return types.PriceData{}, fmt.Errorf("model %s tiered expr run failed: %w", info.OriginModelName, err)
	}

	// v2 returns USD amounts; legacy snapshots/imports retain their old units.
	quotaPerUnit := common.GetQuotaPerUnit()
	quotaBeforeGroup := billingexpr.ExpressionQuota(rawCost, exprStr, quotaPerUnit)
	preConsumedQuota, err := billingexpr.QuotaRoundStrict(quotaBeforeGroup * groupRatioInfo.GroupRatio)
	if err != nil {
		return types.PriceData{}, err
	}

	freeModel := false
	if !operation_setting.GetQuotaSetting().EnableFreeModelPreConsume {
		if groupRatioInfo.GroupRatio == 0 {
			preConsumedQuota = 0
			freeModel = true
		}
	}

	exprHash := billingexpr.ExprHashString(exprStr)
	snapshot := &billingexpr.BillingSnapshot{
		BillingMode:               billingMode,
		ModelName:                 info.OriginModelName,
		ExprString:                exprStr,
		ExprHash:                  exprHash,
		GroupRatio:                groupRatioInfo.GroupRatio,
		EstimatedPromptTokens:     promptTokens,
		EstimatedCompletionTokens: estimatedCompletionTokens,
		EstimatedQuotaBeforeGroup: quotaBeforeGroup,
		EstimatedQuotaAfterGroup:  preConsumedQuota,
		EstimatedTier:             trace.MatchedTier,
		QuotaPerUnit:              quotaPerUnit,
		ExprVersion:               billingexpr.ExprVersion(exprStr),
	}
	info.TieredBillingSnapshot = snapshot
	info.BillingRequestInput = &requestInput

	priceData := types.PriceData{
		FreeModel:         freeModel,
		GroupRatioInfo:    groupRatioInfo,
		QuotaToPreConsume: preConsumedQuota,
	}

	logger.LogDebug(c, "model_price_helper_tiered result: model=%s preConsume=%d quotaBeforeGroup=%.2f groupRatio=%.2f tier=%s", info.OriginModelName, preConsumedQuota, quotaBeforeGroup, groupRatioInfo.GroupRatio, trace.MatchedTier)

	info.PriceData = priceData
	return priceData, nil
}

// eflow: per-call (task) models using tiered_expr — single-shot evaluation,
// no token-based settlement. Designed for video / image generation where cost
// is fully determined by request parameters (resolution, duration, etc.) read
// via param() from the client request body. The upstream call remains a pure
// passthrough: this only affects how new-api charges the end user, not what
// gets forwarded to the upstream provider.
func modelPriceHelperTieredPerCall(c *gin.Context, info *relaycommon.RelayInfo, groupRatioInfo types.GroupRatioInfo, billingMode, exprStr string, hasExpression bool, usage *billingexpr.RequestInput) (types.PriceData, error) {
	// Flat token prices evaluate to zero with no tokens; billing a per-call
	// request with one would make it free, so refuse instead.
	if billingMode == billing_setting.BillingModeFlatPrice {
		return types.PriceData{}, fmt.Errorf("model %s uses flat token pricing and cannot be billed per call; configure a per-call price or expression", info.OriginModelName)
	}
	if !hasExpression || strings.TrimSpace(exprStr) == "" {
		return types.PriceData{}, fmt.Errorf("model %s is configured as %s but has no billing expression", info.OriginModelName, billingMode)
	}

	requestInput, err := ResolveIncomingBillingExprRequestInput(c, info)
	if err != nil {
		return types.PriceData{}, err
	}
	if usage != nil {
		requestInput.Usage, requestInput.UsageSchema = usage.Usage, usage.UsageSchema
	}

	rawCost, trace, err := billingexpr.RunExprWithRequest(exprStr, billingexpr.TokenParams{
		P:   0,
		C:   0,
		Len: 0,
	}, requestInput)
	if err != nil {
		return types.PriceData{}, fmt.Errorf("model %s tiered expr run failed: %w", info.OriginModelName, err)
	}

	quotaBeforeGroup := billingexpr.ExpressionQuota(rawCost, exprStr, common.GetQuotaPerUnit())
	quota, err := billingexpr.QuotaRoundStrict(quotaBeforeGroup * groupRatioInfo.GroupRatio)
	if err != nil {
		return types.PriceData{}, err
	}

	freeModel := false
	if !operation_setting.GetQuotaSetting().EnableFreeModelPreConsume {
		if groupRatioInfo.GroupRatio == 0 || quota == 0 {
			freeModel = true
		}
	}

	info.BillingRequestInput = &requestInput
	info.TieredBillingSnapshot = &billingexpr.BillingSnapshot{
		BillingMode: billingMode, ModelName: info.OriginModelName,
		ExprString: exprStr, ExprHash: billingexpr.ExprHashString(exprStr), ExprVersion: billingexpr.ExprVersion(exprStr),
		QuotaPerUnit: common.GetQuotaPerUnit(), GroupRatio: groupRatioInfo.GroupRatio,
		EstimatedQuotaBeforeGroup: quotaBeforeGroup, EstimatedQuotaAfterGroup: quota, EstimatedTier: trace.MatchedTier,
	}

	// UsePrice=true so downstream marks the task as PerCallBilling and skips
	// diff-settlement (controller/relay.go::PerCallBilling reads PriceData.UsePrice).
	priceData := types.PriceData{
		FreeModel:      freeModel,
		ModelPrice:     -1,
		UsePrice:       true,
		TaskFixedTotal: true,
		Quota:          quota,
		GroupRatioInfo: groupRatioInfo,
	}

	if common.DebugEnabled {
		println(fmt.Sprintf("model_price_helper_tiered_percall result: model=%s quota=%d quotaBeforeGroup=%.2f groupRatio=%.4f tier=%s", info.OriginModelName, quota, quotaBeforeGroup, groupRatioInfo.GroupRatio, trace.MatchedTier))
	}

	info.PriceData = priceData
	return priceData, nil
}
