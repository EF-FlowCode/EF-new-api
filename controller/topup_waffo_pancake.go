package controller

import (
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"
	"time"

	"eflow.local/new-api/common"
	"eflow.local/new-api/logger"
	"eflow.local/new-api/model"
	"eflow.local/new-api/service"
	"eflow.local/new-api/setting"
	"eflow.local/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
	"github.com/thanhpk/randstr"
	pancake "github.com/waffo-com/waffo-pancake-sdk-go"
)

type WaffoPancakePayRequest struct {
	Amount int64 `json:"amount"`
}

func RequestWaffoPancakeAmount(c *gin.Context) {
	runtimeConfig := setting.GetWaffoPancakeConfig()
	generalConfig := operation_setting.GetGeneralSetting()
	paymentConfig := operation_setting.GetPaymentSetting()
	quotaPerUnit := common.GetQuotaPerUnit()

	var req WaffoPancakePayRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "参数错误"})
		return
	}

	minTopUp, err := getWaffoPancakeMinTopUp(runtimeConfig.MinTopUp, generalConfig.QuotaDisplayType, quotaPerUnit)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "充值配置错误"})
		return
	}
	if req.Amount < minTopUp {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": fmt.Sprintf("充值数量不能小于 %d", minTopUp)})
		return
	}

	id := c.GetInt("id")
	if rejectInvalidWaffoPancakeTopUpQuota(c, id, req.Amount, generalConfig.QuotaDisplayType, quotaPerUnit) {
		return
	}
	group, err := model.GetUserGroup(id, true)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "获取用户分组失败"})
		return
	}

	payMoney, err := getWaffoPancakePayMoney(
		req.Amount,
		group,
		runtimeConfig.UnitPrice,
		generalConfig.QuotaDisplayType,
		paymentConfig.AmountDiscount,
		quotaPerUnit,
	)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "充值配置错误"})
		return
	}
	if payMoney <= 0.01 {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "充值金额过低"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "success", "data": fmt.Sprintf("%.2f", payMoney)})
}

func getWaffoPancakePayMoney(
	amount int64,
	group string,
	unitPrice float64,
	quotaDisplayType string,
	amountDiscount map[int]float64,
	quotaPerUnit float64,
) (float64, error) {
	dAmount := decimal.NewFromInt(amount)
	if quotaDisplayType == operation_setting.QuotaDisplayTypeTokens {
		if err := validateWaffoPancakeQuotaPerUnit(quotaPerUnit); err != nil {
			return 0, err
		}
		dAmount = dAmount.Div(decimal.NewFromFloat(quotaPerUnit))
	}

	topupGroupRatio := common.GetTopupGroupRatio(group)
	if topupGroupRatio == 0 {
		topupGroupRatio = 1
	}

	discount := 1.0
	if ds, ok := amountDiscount[int(amount)]; ok && ds > 0 {
		discount = ds
	}

	payMoney := dAmount.
		Mul(decimal.NewFromFloat(unitPrice)).
		Mul(decimal.NewFromFloat(topupGroupRatio)).
		Mul(decimal.NewFromFloat(discount))

	return payMoney.InexactFloat64(), nil
}

func getWaffoPancakeMinTopUp(minTopUp int, quotaDisplayType string, quotaPerUnit float64) (int64, error) {
	if quotaDisplayType != operation_setting.QuotaDisplayTypeTokens {
		return int64(minTopUp), nil
	}
	if err := validateWaffoPancakeQuotaPerUnit(quotaPerUnit); err != nil {
		return 0, err
	}
	converted, err := common.QuotaFromDecimalStrict(
		decimal.NewFromInt(int64(minTopUp)).Mul(decimal.NewFromFloat(quotaPerUnit)),
	)
	return int64(converted), err
}

func normalizeWaffoPancakeTopUpAmount(amount int64, quotaDisplayType string, quotaPerUnit float64) (int64, error) {
	if quotaDisplayType != operation_setting.QuotaDisplayTypeTokens {
		return amount, nil
	}
	if err := validateWaffoPancakeQuotaPerUnit(quotaPerUnit); err != nil {
		return 0, err
	}

	normalized := decimal.NewFromInt(amount).
		Div(decimal.NewFromFloat(quotaPerUnit)).
		IntPart()
	if normalized < 1 {
		return 1, nil
	}
	return normalized, nil
}

func validateWaffoPancakeTopUpQuota(amount int64, quotaDisplayType string, quotaPerUnit float64) (int, error) {
	if err := validateWaffoPancakeQuotaPerUnit(quotaPerUnit); err != nil {
		return 0, err
	}

	quota := decimal.NewFromInt(amount)
	if quotaDisplayType == operation_setting.QuotaDisplayTypeTokens {
		quota = decimal.NewFromInt(quota.Div(decimal.NewFromFloat(quotaPerUnit)).IntPart()).
			Mul(decimal.NewFromFloat(quotaPerUnit))
	} else {
		quota = quota.Mul(decimal.NewFromFloat(quotaPerUnit))
	}
	return validateCreditedQuota(quota)
}

func rejectInvalidWaffoPancakeTopUpQuota(c *gin.Context, userID int, amount int64, quotaDisplayType string, quotaPerUnit float64) bool {
	creditedQuota, err := validateWaffoPancakeTopUpQuota(amount, quotaDisplayType, quotaPerUnit)
	if err == nil {
		err = model.ValidateTopUpQuotaCapacity(userID, creditedQuota)
	}
	if err == nil {
		return false
	}
	c.JSON(http.StatusOK, gin.H{"message": "error", "data": err.Error()})
	return true
}

func validateWaffoPancakeQuotaPerUnit(quotaPerUnit float64) error {
	if quotaPerUnit <= 0 || math.IsNaN(quotaPerUnit) || math.IsInf(quotaPerUnit, 0) {
		return fmt.Errorf("QuotaPerUnit must be a positive finite number")
	}
	return nil
}

func formatWaffoPancakeAmount(payMoney float64) string {
	return decimal.NewFromFloat(payMoney).StringFixed(2)
}

func getWaffoPancakeBuyerEmail(user *model.User) string {
	if user != nil && strings.TrimSpace(user.Email) != "" {
		return user.Email
	}
	return ""
}

// The admin config endpoints below accept typed-but-not-yet-saved creds in
// the body and fall back to persisted creds when the body is blank (see
// resolveWaffoPancakeAdminCreds). Only SaveWaffoPancake writes to OptionMap.

type waffoPancakeCredsRequest struct {
	MerchantID string `json:"merchant_id"`
	PrivateKey string `json:"private_key"`
}

type saveWaffoPancakeRequest struct {
	MerchantID string `json:"merchant_id"`
	PrivateKey string `json:"private_key"`
	ReturnURL  string `json:"return_url"`
	StoreID    string `json:"store_id"`
	ProductID  string `json:"product_id"`
}

type createWaffoPancakePairRequest struct {
	MerchantID string `json:"merchant_id"`
	PrivateKey string `json:"private_key"`
	ReturnURL  string `json:"return_url"`
}

// SaveWaffoPancake atomically persists all five operator-controlled fields.
// Catalog / pair endpoints are transient — only this one writes the OptionMap.
func SaveWaffoPancake(c *gin.Context) {
	var req saveWaffoPancakeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "参数错误"})
		return
	}
	if err := service.SaveWaffoPancakeConfig(
		c.Request.Context(),
		req.MerchantID,
		req.PrivateKey,
		req.ReturnURL,
		req.StoreID,
		req.ProductID,
	); err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf(
			"Waffo Pancake 保存配置失败 store_id=%q product_id=%q error=%q",
			req.StoreID, req.ProductID, err.Error(),
		))
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "保存配置失败"})
		return
	}
	runtimeConfig := setting.GetWaffoPancakeConfig()
	c.JSON(http.StatusOK, gin.H{
		"message": "success",
		"data": gin.H{
			"product_id": runtimeConfig.ProductID,
			"store_id":   runtimeConfig.StoreID,
		},
	})
}

// resolveWaffoPancakeAdminCreds prefers body creds (typed-but-not-yet-saved
// values, for verification) and falls back to persisted creds when the body
// is blank (so returning admins don't have to re-paste the private key,
// which is stripped from GET /api/option/).
func resolveWaffoPancakeAdminCreds(bodyMerchantID, bodyPrivateKey string) (string, string) {
	m := strings.TrimSpace(bodyMerchantID)
	k := strings.TrimSpace(bodyPrivateKey)
	if m == "" && k == "" {
		runtimeConfig := setting.GetWaffoPancakeConfig()
		return runtimeConfig.MerchantID, runtimeConfig.PrivateKey
	}
	return m, k
}

// CreateWaffoPancakePair mints a Store + OnetimeProduct pair in one round-
// trip. Surfaces an orphan-store flag when the product half fails so the
// frontend can preselect / retry without losing context.
func CreateWaffoPancakePair(c *gin.Context) {
	var req createWaffoPancakePairRequest
	if c.Request.ContentLength > 0 {
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusOK, gin.H{"message": "error", "data": "参数错误"})
			return
		}
	}
	merchantID, privateKey := resolveWaffoPancakeAdminCreds(req.MerchantID, req.PrivateKey)
	if merchantID == "" || privateKey == "" {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "Waffo Pancake 凭证未配置"})
		return
	}
	result, err := service.CreateWaffoPancakePrimaryPair(
		c.Request.Context(), merchantID, privateKey, req.ReturnURL,
	)
	if err != nil {
		orphan := result != nil && result.OrphanStore
		logger.LogError(c.Request.Context(), fmt.Sprintf(
			"Waffo Pancake 创建店铺与产品失败 orphan_store=%t store_id=%q error=%q",
			orphan, func() string {
				if result == nil {
					return ""
				}
				return result.StoreID
			}(), err.Error(),
		))
		data := gin.H{"error": err.Error()}
		if orphan {
			data["store_id"] = result.StoreID
			data["store_name"] = result.StoreName
			data["orphan_store"] = true
		}
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": data})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"message": "success",
		"data": gin.H{
			"store_id":     result.StoreID,
			"store_name":   result.StoreName,
			"product_id":   result.ProductID,
			"product_name": result.ProductName,
		},
	})
}

// ListWaffoPancakeCatalog returns the merchant's Stores + OnetimeProducts.
// Doubles as a credential probe (a successful 200 proves the resolved creds
// authenticate). See resolveWaffoPancakeAdminCreds for credential resolution.
func ListWaffoPancakeCatalog(c *gin.Context) {
	var req waffoPancakeCredsRequest
	// An empty body means "use persisted creds"; only fail on malformed JSON.
	if c.Request.ContentLength > 0 {
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusOK, gin.H{"message": "error", "data": "参数错误"})
			return
		}
	}
	merchantID, privateKey := resolveWaffoPancakeAdminCreds(req.MerchantID, req.PrivateKey)
	if merchantID == "" || privateKey == "" {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "Waffo Pancake 凭证未配置"})
		return
	}
	catalog, err := service.ListWaffoPancakeCatalog(c.Request.Context(), merchantID, privateKey)
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf(
			"Waffo Pancake 拉取店铺与产品目录失败 error=%q", err.Error(),
		))
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "拉取目录失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "success", "data": catalog})
}

func getWaffoPancakeBuyerIdentity(user *model.User) string {
	if user == nil {
		return ""
	}
	return service.WaffoPancakeBuyerIdentityFromUserID(user.Id)
}

func isWaffoPancakeEnabledWithSnapshots(runtimeConfig setting.WaffoPancakeConfig, paymentConfig *operation_setting.PaymentSetting) bool {
	return paymentConfig.ComplianceConfirmed &&
		paymentConfig.ComplianceTermsVersion == operation_setting.CurrentComplianceTermsVersion &&
		isWaffoPancakeConfigured(runtimeConfig)
}

func RequestWaffoPancakePay(c *gin.Context) {
	runtimeConfig := setting.GetWaffoPancakeConfig()
	generalConfig := operation_setting.GetGeneralSetting()
	paymentConfig := operation_setting.GetPaymentSetting()
	quotaPerUnit := common.GetQuotaPerUnit()

	if !isWaffoPancakeEnabledWithSnapshots(runtimeConfig, paymentConfig) {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "Waffo Pancake 配置不完整"})
		return
	}

	var req WaffoPancakePayRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "参数错误"})
		return
	}
	minTopUp, err := getWaffoPancakeMinTopUp(runtimeConfig.MinTopUp, generalConfig.QuotaDisplayType, quotaPerUnit)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "充值配置错误"})
		return
	}
	if req.Amount < minTopUp {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": fmt.Sprintf("充值数量不能小于 %d", minTopUp)})
		return
	}

	id := c.GetInt("id")
	if rejectInvalidWaffoPancakeTopUpQuota(c, id, req.Amount, generalConfig.QuotaDisplayType, quotaPerUnit) {
		return
	}
	user, err := model.GetUserById(id, false)
	if err != nil || user == nil {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "用户不存在"})
		return
	}

	group, err := model.GetUserGroup(id, true)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "获取用户分组失败"})
		return
	}

	payMoney, err := getWaffoPancakePayMoney(
		req.Amount,
		group,
		runtimeConfig.UnitPrice,
		generalConfig.QuotaDisplayType,
		paymentConfig.AmountDiscount,
		quotaPerUnit,
	)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "充值配置错误"})
		return
	}
	if payMoney < 0.01 {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "充值金额过低"})
		return
	}

	normalizedAmount, err := normalizeWaffoPancakeTopUpAmount(req.Amount, generalConfig.QuotaDisplayType, quotaPerUnit)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "充值配置错误"})
		return
	}
	now := time.Now()
	tradeNo := fmt.Sprintf("WAFFO_PANCAKE-%d-%d-%s", id, now.UnixMilli(), randstr.String(6))
	topUp, checkoutParams := buildWaffoPancakePaymentArtifacts(
		runtimeConfig, user, normalizedAmount, payMoney, tradeNo, now.Unix(),
	)
	if err := topUp.Insert(); err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("Waffo Pancake 创建充值订单失败 user_id=%d trade_no=%s amount=%d error=%q", id, tradeNo, req.Amount, err.Error()))
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "创建订单失败"})
		return
	}

	session, err := service.CreateWaffoPancakeCheckoutSession(c.Request.Context(), checkoutParams)
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("Waffo Pancake 创建结账会话失败 user_id=%d trade_no=%s error=%q", id, tradeNo, err.Error()))
		topUp.Status = common.TopUpStatusFailed
		_ = topUp.Update()
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "拉起支付失败"})
		return
	}
	logger.LogInfo(c.Request.Context(), fmt.Sprintf("Waffo Pancake 充值订单创建成功 user_id=%d trade_no=%s session_id=%s amount=%d money=%.2f", id, tradeNo, session.SessionID, req.Amount, payMoney))

	c.JSON(http.StatusOK, gin.H{
		"message": "success",
		"data": gin.H{
			"checkout_url":     session.CheckoutURL,
			"session_id":       session.SessionID,
			"expires_at":       session.ExpiresAt,
			"order_id":         tradeNo,
			"token":            session.Token,
			"token_expires_at": session.TokenExpiresAt,
		},
	})
}

func buildWaffoPancakePaymentArtifacts(
	runtimeConfig setting.WaffoPancakeConfig,
	user *model.User,
	normalizedAmount int64,
	payMoney float64,
	tradeNo string,
	createdAt int64,
) (*model.TopUp, *service.WaffoPancakeCreateSessionParams) {
	expiresInSeconds := 45 * 60
	topUp := &model.TopUp{
		UserId:          user.Id,
		Amount:          normalizedAmount,
		Money:           payMoney,
		TradeNo:         tradeNo,
		PaymentMethod:   model.PaymentMethodWaffoPancake,
		PaymentProvider: model.PaymentProviderWaffoPancake,
		CreateTime:      createdAt,
		Status:          common.TopUpStatusPending,
	}
	checkoutParams := &service.WaffoPancakeCreateSessionParams{
		ProductID:     runtimeConfig.ProductID,
		BuyerIdentity: getWaffoPancakeBuyerIdentity(user),
		PriceSnapshot: &service.WaffoPancakePriceSnapshot{
			Amount:      formatWaffoPancakeAmount(payMoney),
			TaxCategory: "saas",
		},
		BuyerEmail:              getWaffoPancakeBuyerEmail(user),
		ExpiresInSeconds:        &expiresInSeconds,
		OrderMerchantExternalID: tradeNo,
		RuntimeConfig:           runtimeConfig,
	}
	return topUp, checkoutParams
}

func WaffoPancakeWebhook(c *gin.Context) {
	runtimeConfig := setting.GetWaffoPancakeConfig()
	paymentConfig := operation_setting.GetPaymentSetting()
	if !isWaffoPancakeEnabledWithSnapshots(runtimeConfig, paymentConfig) {
		logger.LogWarn(c.Request.Context(), fmt.Sprintf("Waffo Pancake webhook 被拒绝 reason=webhook_disabled path=%q client_ip=%s", c.Request.RequestURI, c.ClientIP()))
		c.String(http.StatusForbidden, "webhook disabled")
		return
	}

	// :env splits test vs prod traffic at the routing layer — operator
	// registers each URL in the matching webhook slot in Pancake's dashboard.
	// We then enforce event.mode == expectedEnv to catch mis-registrations.
	expectedEnv := strings.TrimSpace(c.Param("env"))
	if expectedEnv != "test" && expectedEnv != "prod" {
		logger.LogWarn(c.Request.Context(), fmt.Sprintf(
			"Waffo Pancake webhook 路径环境段无效 env=%q path=%q client_ip=%s",
			expectedEnv, c.Request.RequestURI, c.ClientIP(),
		))
		c.String(http.StatusNotFound, "unknown env")
		return
	}

	bodyBytes, err := io.ReadAll(c.Request.Body)
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("Waffo Pancake webhook 读取请求体失败 path=%q client_ip=%s error=%q", c.Request.RequestURI, c.ClientIP(), err.Error()))
		c.String(http.StatusBadRequest, "bad request")
		return
	}

	signature := c.GetHeader("X-Waffo-Signature")
	logger.LogInfo(c.Request.Context(), fmt.Sprintf("Waffo Pancake webhook 收到请求 path=%q client_ip=%s signature=%q body=%q", c.Request.RequestURI, c.ClientIP(), signature, string(bodyBytes)))

	// Only production-key signatures can credit a wallet. A valid test-key
	// event is acknowledged and logged but never credited.
	event, err := service.VerifyConfiguredWaffoPancakeWebhook(string(bodyBytes), signature, pancake.EnvironmentProd)
	if err != nil {
		if testEvent, testErr := service.VerifyConfiguredWaffoPancakeWebhook(string(bodyBytes), signature, pancake.EnvironmentTest); testErr == nil {
			logger.LogWarn(c.Request.Context(), fmt.Sprintf("Waffo Pancake 测试模式事件不入账 event_type=%s event_id=%s order_id=%s client_ip=%s", testEvent.NormalizedEventType(), testEvent.ID, testEvent.Data.OrderID, c.ClientIP()))
			c.String(http.StatusOK, "OK")
			return
		}
		logger.LogWarn(c.Request.Context(), fmt.Sprintf("Waffo Pancake webhook 验签失败 path=%q client_ip=%s signature=%q body=%q error=%q", c.Request.RequestURI, c.ClientIP(), signature, string(bodyBytes), err.Error()))
		c.String(http.StatusUnauthorized, "invalid signature")
		return
	}

	// Credit only production events delivered to the production URL.
	if expectedEnv != string(pancake.EnvironmentProd) || !strings.EqualFold(strings.TrimSpace(event.Mode), expectedEnv) {
		logger.LogError(c.Request.Context(), fmt.Sprintf(
			"Waffo Pancake webhook 环境不匹配 expected=%q actual_mode=%q event_id=%s order_id=%s client_ip=%s",
			expectedEnv, event.Mode, event.ID, event.Data.OrderID, c.ClientIP(),
		))
		c.String(http.StatusOK, "OK")
		return
	}

	logger.LogInfo(c.Request.Context(), fmt.Sprintf("Waffo Pancake webhook 验签成功 event_type=%s event_id=%s order_id=%s client_ip=%s", event.NormalizedEventType(), event.ID, event.Data.OrderID, c.ClientIP()))
	if event.NormalizedEventType() != "order.completed" {
		c.String(http.StatusOK, "OK")
		return
	}

	tradeNo, err := service.ResolveWaffoPancakeTradeNo(event)
	if err != nil {
		// LogError (not LogWarn): covers order-not-found and buyer-identity
		// mismatch — both warrant human attention. 200 OK so Waffo doesn't
		// retry a permanently-unresolvable webhook.
		logger.LogError(c.Request.Context(), fmt.Sprintf(
			"Waffo Pancake webhook 订单解析失败 event_id=%s order_id=%s buyer_identity=%q client_ip=%s error=%q",
			event.ID, event.Data.OrderID, event.Data.MerchantProvidedBuyerIdentity, c.ClientIP(), err.Error(),
		))
		c.String(http.StatusOK, "OK")
		return
	}

	LockOrder(tradeNo)
	defer UnlockOrder(tradeNo)

	if topUp := model.GetTopUpByTradeNo(tradeNo); topUp == nil {
		c.String(http.StatusOK, "OK")
		return
	} else if err := service.ValidateWaffoPancakePaidOrder(event, runtimeConfig.StoreID, topUp.Money); err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("Waffo Pancake 回调与订单不符，拒绝入账 trade_no=%s event_id=%s order_id=%s client_ip=%s error=%q", tradeNo, event.ID, event.Data.OrderID, c.ClientIP(), err.Error()))
		c.String(http.StatusOK, "OK")
		return
	}

	if err := model.RechargeWaffoPancake(tradeNo); err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("Waffo Pancake 充值处理失败 trade_no=%s event_id=%s order_id=%s client_ip=%s error=%q", tradeNo, event.ID, event.Data.OrderID, c.ClientIP(), err.Error()))
		c.String(http.StatusInternalServerError, "retry")
		return
	}

	logger.LogInfo(c.Request.Context(), fmt.Sprintf("Waffo Pancake 充值成功 trade_no=%s event_id=%s order_id=%s client_ip=%s", tradeNo, event.ID, event.Data.OrderID, c.ClientIP()))
	c.String(http.StatusOK, "OK")
}
