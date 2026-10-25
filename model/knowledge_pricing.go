package model

import (
	"encoding/json"

	"eflow.local/new-api/constant"
	"eflow.local/new-api/dto"
	"eflow.local/new-api/pkg/billingexpr"
	"eflow.local/new-api/pkg/knowledge"
	"eflow.local/new-api/setting/billing_setting"
	"eflow.local/new-api/setting/ratio_setting"
	"eflow.local/new-api/setting/system_setting"
)

type KnowledgePricing struct {
	knowledge.Capability
	Prices      *knowledge.Prices `json:"prices,omitempty"`
	BatchGroups []string          `json:"batch_groups"`
}

func knowledgePricing(name string) *KnowledgePricing {
	c, ok := knowledge.Lookup(name)
	if !ok {
		return nil
	}
	result := &KnowledgePricing{Capability: c, BatchGroups: []string{}}
	mode, expression, _ := billing_setting.GetBillingRule(name)
	if billing_setting.IsExpressionBillingMode(mode) {
		if flat, ok := billingexpr.ParseFlatTokenBillingExpr(expression); ok {
			input := flat.Coefficients["p"]
			prices := knowledge.Prices{Input: &input}
			if image, exists := flat.Coefficients["img"]; exists {
				prices.Image = &image
			}
			if batch, exists := ratio_setting.GetBatchInputRatio(name); exists {
				value := batch * 2
				prices.BatchInput = &value
			}
			result.Prices = &prices
		}
		return result
	}
	if inputRatio, configured := ratio_setting.GetModelRatioCopy()[ratio_setting.FormatMatchingModelName(name)]; configured {
		input := inputRatio * 2
		prices := knowledge.Prices{Input: &input}
		if imageRatio, ok := ratio_setting.GetImageRatio(name); ok {
			image := input * imageRatio
			prices.Image = &image
		}
		if batchRatio, ok := ratio_setting.GetBatchInputRatio(name); ok {
			batch := batchRatio * 2
			prices.BatchInput = &batch
		}
		result.Prices = &prices
	}
	return result
}

func ChannelSupportsKnowledgeBatch(channel *Channel, name string) bool {
	if channel == nil || (channel.Type != constant.ChannelTypeAli && channel.Type != constant.ChannelTypeOpenAI) || !knowledge.SupportsAliBatch(name) || system_setting.GetWorkerSettings().Enabled() {
		return false
	}
	var settings dto.ChannelSettings
	if channel.Setting != nil && *channel.Setting != "" {
		if err := json.Unmarshal([]byte(*channel.Setting), &settings); err != nil || settings.Proxy != "" {
			return false
		}
	}
	base := channel.GetBaseURL()
	if base == "" && channel.Type == constant.ChannelTypeAli {
		base = constant.GetChannelBaseURL(constant.ChannelTypeAli)
	}
	if _, err := knowledge.AliBatchBase(base); err != nil {
		return false
	}
	var mapping map[string]string
	if raw := channel.GetModelMapping(); raw != "" {
		if err := json.Unmarshal([]byte(raw), &mapping); err != nil {
			return false
		}
	}
	return mapping[name] == "" || mapping[name] == name
}
