package controller

import (
	"eflow.local/new-api/common"
	"eflow.local/new-api/model"
	"eflow.local/new-api/setting/operation_setting"
	"eflow.local/new-api/types"
	"github.com/gin-gonic/gin"
)

func GetSubscription(c *gin.Context) {
	// eflow: billing 接口永远返回账户余额（user quota），不再受 DisplayTokenStatEnabled 影响。
	// 中转站语义下用户关心的是账户真实余额而非 token 自身配额；
	// 同时让 EFlowCodeLine 等 OpenAI 兼容客户端无需配置 access_token 即拿到正确余额。
	var remainQuota int
	var usedQuota int
	var err error
	userId := c.GetInt("id")
	remainQuota, err = model.GetUserQuota(userId, false)
	usedQuota, err = model.GetUserUsedQuota(userId)
	if err != nil {
		openAIError := types.OpenAIError{
			Message: err.Error(),
			Type:    "upstream_error",
		}
		c.JSON(200, gin.H{
			"error": openAIError,
		})
		return
	}
	quota := remainQuota + usedQuota
	amount := float64(quota)
	quotaPerUnit := common.GetQuotaPerUnit()
	epayConfig := operation_setting.GetEpayConfig()
	// OpenAI 兼容接口中的 *_USD 字段含义保持“额度单位”对应值：
	// 我们将其解释为以“站点展示类型”为准：
	// - USD: 直接除以 QuotaPerUnit
	// - CNY: 先转 USD 再乘汇率
	// - TOKENS: 直接使用 tokens 数量
	switch operation_setting.GetQuotaDisplayType() {
	case operation_setting.QuotaDisplayTypeCNY:
		amount = amount / quotaPerUnit * epayConfig.USDExchangeRate
	case operation_setting.QuotaDisplayTypeTokens:
		// amount 保持 tokens 数值
	default:
		amount = amount / quotaPerUnit
	}
	subscription := OpenAISubscriptionResponse{
		Object:             "billing_subscription",
		HasPaymentMethod:   true,
		SoftLimitUSD:       amount,
		HardLimitUSD:       amount,
		SystemHardLimitUSD: amount,
		AccessUntil:        0,
	}
	c.JSON(200, subscription)
	return
}

func GetUsage(c *gin.Context) {
	// eflow: billing 接口永远返回账户余额（user quota），不再受 DisplayTokenStatEnabled 影响。
	// 中转站语义下用户关心的是账户真实余额而非 token 自身配额；
	// 同时让 EFlowCodeLine 等 OpenAI 兼容客户端无需配置 access_token 即拿到正确余额。
	var quota int
	var err error
	userId := c.GetInt("id")
	quota, err = model.GetUserUsedQuota(userId)
	if err != nil {
		openAIError := types.OpenAIError{
			Message: err.Error(),
			Type:    "new_api_error",
		}
		c.JSON(200, gin.H{
			"error": openAIError,
		})
		return
	}
	amount := float64(quota)
	quotaPerUnit := common.GetQuotaPerUnit()
	epayConfig := operation_setting.GetEpayConfig()
	switch operation_setting.GetQuotaDisplayType() {
	case operation_setting.QuotaDisplayTypeCNY:
		amount = amount / quotaPerUnit * epayConfig.USDExchangeRate
	case operation_setting.QuotaDisplayTypeTokens:
		// tokens 保持原值
	default:
		amount = amount / quotaPerUnit
	}
	usage := OpenAIUsageResponse{
		Object:     "list",
		TotalUsage: amount * 100,
	}
	c.JSON(200, usage)
	return
}
