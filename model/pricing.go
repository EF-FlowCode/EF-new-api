package model

import (
	"context"
	"fmt"
	"strings"

	"sync"
	"time"

	"eflow.local/new-api/common"
	"eflow.local/new-api/constant"
	"eflow.local/new-api/pkg/knowledge"
	"eflow.local/new-api/setting/billing_setting"
	"eflow.local/new-api/setting/ratio_setting"
	"eflow.local/new-api/types"
)

type Pricing struct {
	TaskPricing            map[string][]billing_setting.TaskBillingRule `json:"task_pricing,omitempty"`
	Knowledge              *KnowledgePricing                            `json:"knowledge,omitempty"`
	ModelName              string                                       `json:"model_name"`
	Description            string                                       `json:"description,omitempty"`
	Icon                   string                                       `json:"icon,omitempty"`
	Tags                   string                                       `json:"tags,omitempty"`
	VendorID               int                                          `json:"vendor_id,omitempty"`
	QuotaType              int                                          `json:"quota_type"`
	ModelRatio             float64                                      `json:"model_ratio"`
	ModelPrice             float64                                      `json:"model_price"`
	OwnerBy                string                                       `json:"owner_by"`
	CompletionRatio        float64                                      `json:"completion_ratio"`
	CacheRatio             *float64                                     `json:"cache_ratio,omitempty"`
	CreateCacheRatio       *float64                                     `json:"create_cache_ratio,omitempty"`
	ImageRatio             *float64                                     `json:"image_ratio,omitempty"`
	AudioRatio             *float64                                     `json:"audio_ratio,omitempty"`
	AudioCompletionRatio   *float64                                     `json:"audio_completion_ratio,omitempty"`
	EnableGroup            []string                                     `json:"enable_groups"`
	SupportedEndpointTypes []constant.EndpointType                      `json:"supported_endpoint_types"`
	BillingMode            string                                       `json:"billing_mode,omitempty"`
	BillingExpr            string                                       `json:"billing_expr,omitempty"`
	BillingUsage           *BillingUsageInfo                            `json:"billing_usage,omitempty"`
	DimensionalPricing     *billing_setting.DimensionalPricing          `json:"dimensional_pricing,omitempty"`
	ContextWindow          int64                                        `json:"context_window,omitempty"`
	PricingVersion         string                                       `json:"pricing_version,omitempty"`
}

type PricingVendor struct {
	ID          int    `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Icon        string `json:"icon,omitempty"`
}

func attachDimensionalPricing(pricing *Pricing, modelName string) bool {
	dimensionalPricing, ok := billing_setting.GetDimensionalPricing(modelName)
	if !ok {
		return false
	}
	pricing.BillingMode = billing_setting.BillingModeDimensional
	pricing.DimensionalPricing = &dimensionalPricing
	return true
}

var (
	pricingMap           []Pricing
	vendorsList          []PricingVendor
	supportedEndpointMap map[string]common.EndpointInfo
	lastGetPricingTime   time.Time
	updatePricingLock    sync.Mutex

	// 缓存映射：模型名 -> 启用分组 / 计费类型
	modelEnableGroups     = make(map[string][]string)
	modelQuotaTypeMap     = make(map[string]int)
	modelEnableGroupsLock = sync.RWMutex{}
)

var (
	modelSupportEndpointTypes = make(map[string][]constant.EndpointType)
	modelSupportEndpointsLock = sync.RWMutex{}
)

func GetPricing() []Pricing {
	if time.Since(lastGetPricingTime) > time.Minute*1 || len(pricingMap) == 0 {
		updatePricingLock.Lock()
		defer updatePricingLock.Unlock()
		// Double check after acquiring the lock
		if time.Since(lastGetPricingTime) > time.Minute*1 || len(pricingMap) == 0 {
			modelSupportEndpointsLock.Lock()
			defer modelSupportEndpointsLock.Unlock()
			updatePricing()
		}
	}
	return pricingMap
}

func InvalidatePricingCache() {
	updatePricingLock.Lock()
	defer updatePricingLock.Unlock()

	pricingMap = nil
	vendorsList = nil
	lastGetPricingTime = time.Time{}
}

// GetVendors 返回当前定价接口使用到的供应商信息
func GetVendors() []PricingVendor {
	if time.Since(lastGetPricingTime) > time.Minute*1 || len(pricingMap) == 0 {
		// 保证先刷新一次
		GetPricing()
	}
	return vendorsList
}

func GetModelSupportEndpointTypes(model string) []constant.EndpointType {
	if model == "" {
		return make([]constant.EndpointType, 0)
	}
	modelSupportEndpointsLock.RLock()
	defer modelSupportEndpointsLock.RUnlock()
	if endpoints, ok := modelSupportEndpointTypes[model]; ok {
		return endpoints
	}
	return make([]constant.EndpointType, 0)
}

// getPricingEndpointTypesForAbility honors per-route endpoint capabilities for
// Advanced Custom channels while preserving the legacy Custom defaults for
// management-only or unconfigured channels.
func getPricingEndpointTypesForAbility(ability AbilityWithChannel, channelCache map[int]*Channel) []constant.EndpointType {
	if !constant.SupportsAdvancedCustomRoutes(ability.ChannelType) {
		return common.GetEndpointTypesByChannelType(ability.ChannelType, ability.Model)
	}
	channel := channelCache[ability.ChannelId]
	if channel == nil {
		if cached, err := CacheGetChannel(ability.ChannelId); err == nil {
			channel = cached
		} else if loaded, loadErr := GetChannelById(ability.ChannelId, false); loadErr == nil {
			channel = loaded
		}
		channelCache[ability.ChannelId] = channel
	}
	if channel != nil {
		config := channel.GetOtherSettings().AdvancedCustom
		if config != nil && config.HasRelayRoutes() {
			endpoints := config.SupportedEndpointTypesForModel(ability.Model)
			converted := make([]constant.EndpointType, 0, len(endpoints))
			for _, endpoint := range endpoints {
				converted = append(converted, constant.EndpointType(string(endpoint)))
			}
			return converted
		}
	}
	return common.GetEndpointTypesByChannelType(ability.ChannelType, ability.Model)
}

func updatePricing() {
	//modelRatios := common.GetModelRatios()
	enableAbilities, err := GetAllEnableAbilityWithChannels()
	if err != nil {
		common.SysLog(fmt.Sprintf("GetAllEnableAbilityWithChannels error: %v", err))
		return
	}
	// 预加载模型元数据与供应商一次，避免循环查询
	var allMeta []Model
	_ = DB.Find(&allMeta).Error
	metaMap := make(map[string]*Model)
	prefixList := make([]*Model, 0)
	suffixList := make([]*Model, 0)
	containsList := make([]*Model, 0)
	for i := range allMeta {
		m := &allMeta[i]
		if m.NameRule == NameRuleExact {
			metaMap[m.ModelName] = m
		} else {
			switch m.NameRule {
			case NameRulePrefix:
				prefixList = append(prefixList, m)
			case NameRuleSuffix:
				suffixList = append(suffixList, m)
			case NameRuleContains:
				containsList = append(containsList, m)
			}
		}
	}

	// 将非精确规则模型匹配到 metaMap
	for _, m := range prefixList {
		for _, pricingModel := range enableAbilities {
			if strings.HasPrefix(pricingModel.Model, m.ModelName) {
				if _, exists := metaMap[pricingModel.Model]; !exists {
					metaMap[pricingModel.Model] = m
				}
			}
		}
	}
	for _, m := range suffixList {
		for _, pricingModel := range enableAbilities {
			if strings.HasSuffix(pricingModel.Model, m.ModelName) {
				if _, exists := metaMap[pricingModel.Model]; !exists {
					metaMap[pricingModel.Model] = m
				}
			}
		}
	}
	for _, m := range containsList {
		for _, pricingModel := range enableAbilities {
			if strings.Contains(pricingModel.Model, m.ModelName) {
				if _, exists := metaMap[pricingModel.Model]; !exists {
					metaMap[pricingModel.Model] = m
				}
			}
		}
	}

	// 预加载供应商
	var vendors []Vendor
	_ = DB.Find(&vendors).Error
	vendorMap := make(map[int]*Vendor)
	for i := range vendors {
		vendorMap[vendors[i].Id] = &vendors[i]
	}

	// 初始化默认供应商映射
	initDefaultVendorMapping(metaMap, vendorMap, enableAbilities)

	// 构建对前端友好的供应商列表
	vendorsList = make([]PricingVendor, 0, len(vendorMap))
	for _, v := range vendorMap {
		vendorsList = append(vendorsList, PricingVendor{
			ID:          v.Id,
			Name:        v.Name,
			Description: v.Description,
			Icon:        v.Icon,
		})
	}

	modelGroupsMap := make(map[string]*types.Set[string])
	for _, ability := range enableAbilities {
		groups, ok := modelGroupsMap[ability.Model]
		if !ok {
			groups = types.NewSet[string]()
			modelGroupsMap[ability.Model] = groups
		}
		groups.Add(ability.Group)
	}

	//这里使用切片而不是Set，因为一个模型可能支持多个端点类型，并且第一个端点是优先使用端点
	modelSupportEndpointsStr := make(map[string][]string)
	pricingChannelCache := make(map[int]*Channel)
	batchGroups := make(map[string][]string)

	// 先根据已有能力填充原生端点
	for _, ability := range enableAbilities {
		if knowledge.SupportsAliBatch(ability.Model) {
			channel, found := pricingChannelCache[ability.ChannelId]
			if !found {
				channel, _ = GetChannelById(ability.ChannelId, false)
				pricingChannelCache[ability.ChannelId] = channel
			}
			if ChannelSupportsKnowledgeBatch(channel, ability.Model) && !common.StringsContains(batchGroups[ability.Model], ability.Group) {
				batchGroups[ability.Model] = append(batchGroups[ability.Model], ability.Group)
			}
		}
		endpoints := modelSupportEndpointsStr[ability.Model]
		channelTypes := getPricingEndpointTypesForAbility(ability, pricingChannelCache)
		for _, channelType := range channelTypes {
			if !common.StringsContains(endpoints, string(channelType)) {
				endpoints = append(endpoints, string(channelType))
			}
		}
		modelSupportEndpointsStr[ability.Model] = endpoints
	}

	// 再补充模型自定义端点：若配置有效则替换默认端点，不做合并
	for modelName, meta := range metaMap {
		if strings.TrimSpace(meta.Endpoints) == "" {
			continue
		}
		var raw map[string]any
		if err := common.Unmarshal([]byte(meta.Endpoints), &raw); err == nil {
			endpoints := make([]string, 0, len(raw))
			for k, v := range raw {
				switch v.(type) {
				case string, map[string]any:
					if !common.StringsContains(endpoints, k) {
						endpoints = append(endpoints, k)
					}
				}
			}
			if len(endpoints) > 0 {
				modelSupportEndpointsStr[modelName] = endpoints
			}
		}
	}

	modelSupportEndpointTypes = make(map[string][]constant.EndpointType)
	for model, endpoints := range modelSupportEndpointsStr {
		supportedEndpoints := make([]constant.EndpointType, 0)
		for _, endpointStr := range endpoints {
			endpointType := constant.EndpointType(endpointStr)
			supportedEndpoints = append(supportedEndpoints, endpointType)
		}
		modelSupportEndpointTypes[model] = supportedEndpoints
	}

	// 构建全局 supportedEndpointMap（默认 + 自定义覆盖）
	supportedEndpointMap = make(map[string]common.EndpointInfo)
	// 1. 默认端点
	for _, endpoints := range modelSupportEndpointTypes {
		for _, et := range endpoints {
			if info, ok := common.GetDefaultEndpointInfo(et); ok {
				if _, exists := supportedEndpointMap[string(et)]; !exists {
					supportedEndpointMap[string(et)] = info
				}
			}
		}
	}
	// 2. 自定义端点（models 表）覆盖默认
	for _, meta := range metaMap {
		if strings.TrimSpace(meta.Endpoints) == "" {
			continue
		}
		var raw map[string]any
		if err := common.Unmarshal([]byte(meta.Endpoints), &raw); err == nil {
			for k, v := range raw {
				switch val := v.(type) {
				case string:
					supportedEndpointMap[k] = common.EndpointInfo{Path: val, Method: "POST"}
				case map[string]any:
					ep := common.EndpointInfo{Method: "POST"}
					if p, ok := val["path"].(string); ok {
						ep.Path = p
					}
					if m, ok := val["method"].(string); ok {
						ep.Method = strings.ToUpper(m)
					}
					supportedEndpointMap[k] = ep
				default:
					// ignore unsupported types
				}
			}
		}
	}

	pricingMap = make([]Pricing, 0)
	usageInfos, _ := GetBillingUsageInfo(context.Background(), DB)
	for model, groups := range modelGroupsMap {
		pricing := Pricing{
			Knowledge:              knowledgePricing(model),
			ModelName:              model,
			EnableGroup:            groups.Items(),
			SupportedEndpointTypes: modelSupportEndpointTypes[model],
			ContextWindow:          GetModelContextWindow(model),
		}
		if usage, ok := usageInfos[model]; ok {
			pricing.BillingUsage = &usage
			pricing.TaskPricing = taskPricingForUsage(model, usage)
		}
		if pricing.Knowledge != nil && len(batchGroups[model]) > 0 {
			pricing.Knowledge.BatchGroups = batchGroups[model]
		}

		// 补充模型元数据（描述、标签、供应商、状态）
		if meta, ok := metaMap[model]; ok {
			// 若模型被禁用(status!=1)，则直接跳过，不返回给前端
			if meta.Status != 1 {
				continue
			}
			pricing.Description = meta.Description
			pricing.Icon = meta.Icon
			pricing.Tags = meta.Tags
			pricing.VendorID = meta.VendorID
		}
		modelPrice, findPrice := ratio_setting.GetModelPrice(model, false)
		if findPrice {
			pricing.ModelPrice = modelPrice
			pricing.QuotaType = 1
		} else {
			// An unconfigured ratio would report the 37.5 fallback; expression
			// models are described by billing_expr, so leave the ratios unset.
			if modelRatio, configured, _ := ratio_setting.GetModelRatio(model); configured || !billing_setting.IsExpressionBillingMode(billing_setting.GetBillingMode(model)) {
				pricing.ModelRatio = modelRatio
				pricing.CompletionRatio = ratio_setting.GetCompletionRatio(model)
			}
			pricing.QuotaType = 0
		}
		if cacheRatio, ok := ratio_setting.GetCacheRatio(model); ok {
			pricing.CacheRatio = &cacheRatio
		}
		if createCacheRatio, ok := ratio_setting.GetCreateCacheRatio(model); ok {
			pricing.CreateCacheRatio = &createCacheRatio
		}
		if imageRatio, ok := ratio_setting.GetImageRatio(model); ok {
			pricing.ImageRatio = &imageRatio
		}
		if ratio_setting.ContainsAudioRatio(model) {
			audioRatio := ratio_setting.GetAudioRatio(model)
			pricing.AudioRatio = &audioRatio
		}
		if ratio_setting.ContainsAudioCompletionRatio(model) {
			audioCompletionRatio := ratio_setting.GetAudioCompletionRatio(model)
			pricing.AudioCompletionRatio = &audioCompletionRatio
		}
		billingMode, billingExpr, hasBillingExpr := billing_setting.GetBillingRule(model)
		if pricing.Knowledge != nil && pricing.Knowledge.Prices != nil {
			// Knowledge models charge input dimensions only. Keep the ordinary
			// model response shape while making the zero output price explicit.
			pricing.QuotaType = 0
			pricing.ModelPrice = 0
			pricing.CompletionRatio = 0
			pricing.CacheRatio = nil
			pricing.CreateCacheRatio = nil
			pricing.AudioRatio = nil
			pricing.AudioCompletionRatio = nil
		}
		if billing_setting.IsExpressionBillingMode(billingMode) {
			if hasBillingExpr && strings.TrimSpace(billingExpr) != "" {
				pricing.BillingMode = billing_setting.NormalizeExpressionBillingMode(billingMode, billingExpr)
				pricing.BillingExpr = billingExpr
			}
		} else if billingMode == billing_setting.BillingModeAudioSeconds {
			// eflow: 纯展示标记透传给前端；不带 expr → 结算不进 tiered_expr
			// （relay/helper/price.go 均只判 tiered_expr），仍按 ModelRatio
			// 结算（秒当 token），零计费影响。
			pricing.BillingMode = billingMode
		} else if attachDimensionalPricing(&pricing, model) {
			// eflow: 仅透传结构化展示元数据。实际任务计费仍由 adapter 的
			// EstimateBilling 写入时长与分辨率倍率，避免改变结算路径。
		}
		pricingMap = append(pricingMap, pricing)
	}

	// 防止大更新后数据不通用
	if len(pricingMap) > 0 {
		pricingMap[0].PricingVersion = "5a90f2b86c08bd983a9a2e6d66c255f4eaef9c4bc934386d2b6ae84ef0ff1f1f"
	}

	// 刷新缓存映射，供高并发快速查询
	modelEnableGroupsLock.Lock()
	modelEnableGroups = make(map[string][]string)
	modelQuotaTypeMap = make(map[string]int)
	for _, p := range pricingMap {
		modelEnableGroups[p.ModelName] = p.EnableGroup
		modelQuotaTypeMap[p.ModelName] = p.QuotaType
	}
	modelEnableGroupsLock.Unlock()

	lastGetPricingTime = time.Now()
}

// GetSupportedEndpointMap 返回全局端点到路径的映射
func GetSupportedEndpointMap() map[string]common.EndpointInfo {
	return supportedEndpointMap
}

// PublicPricing strips plugin internals that only the Root pricing editor
// needs: source identity (plugin/version/source_hash/upstream model) and the
// per-plugin task rules with mapped upstream models and expressions. Usage
// status/schema/examples stay for display. The cached entry is not mutated.
func PublicPricing(pricing Pricing) Pricing {
	pricing.TaskPricing = nil
	if pricing.BillingUsage != nil {
		usage := publicBillingUsage(*pricing.BillingUsage)
		pricing.BillingUsage = &usage
	}
	return pricing
}

func publicBillingUsage(info BillingUsageInfo) BillingUsageInfo {
	info.Sources = nil
	if info.Plugins != nil {
		plugins := make(map[string]BillingUsageInfo, len(info.Plugins))
		for key, plugin := range info.Plugins {
			plugins[key] = publicBillingUsage(plugin)
		}
		info.Plugins = plugins
	}
	return info
}
