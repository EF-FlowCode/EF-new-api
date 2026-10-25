package controller

import (
	"errors"
	"fmt"
	"maps"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"eflow.local/new-api/common"
	"eflow.local/new-api/constant"
	"eflow.local/new-api/i18n"
	"eflow.local/new-api/model"
	"eflow.local/new-api/setting/operation_setting"
	"eflow.local/new-api/setting/ratio_setting"

	"github.com/gin-gonic/gin"
)

var completionRatioMetaOptionKeys = []string{
	"ModelPrice",
	"ModelRatio",
	"CompletionRatio",
	"CacheRatio",
	"CreateCacheRatio",
	"ImageRatio",
	"AudioRatio",
	"AudioCompletionRatio",
}

func isPaymentComplianceOptionKey(key string) bool {
	return strings.HasPrefix(key, "payment_setting.compliance_")
}

func isPositiveOptionValue(value string) bool {
	intValue, err := strconv.Atoi(strings.TrimSpace(value))
	if err == nil {
		return intValue > 0
	}
	floatValue, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
	return err == nil && floatValue > 0
}

var errPaymentComplianceRequired = errors.New("payment compliance confirmation is required")

type enabledOptionRequirement struct {
	enabledKey   string
	requiredKeys []string
	message      string
}

var enabledOptionRequirements = []enabledOptionRequirement{
	{
		enabledKey:   "GitHubOAuthEnabled",
		requiredKeys: []string{"GitHubClientId", "GitHubClientSecret"},
		message:      "无法启用 GitHub OAuth，请先填写 Client ID 和 Client Secret",
	},
	{
		enabledKey:   "discord.enabled",
		requiredKeys: []string{"discord.client_id", "discord.client_secret"},
		message:      "无法启用 Discord OAuth，请先填写 Client ID 和 Client Secret",
	},
	{
		enabledKey: "oidc.enabled",
		requiredKeys: []string{
			"oidc.client_id",
			"oidc.client_secret",
			"oidc.authorization_endpoint",
			"oidc.token_endpoint",
			"oidc.user_info_endpoint",
		},
		message: "无法启用 OIDC 登录，请先填写客户端凭据和 Authorization、Token、UserInfo 端点",
	},
	{
		enabledKey:   "TelegramOAuthEnabled",
		requiredKeys: []string{"TelegramBotToken", "TelegramBotName"},
		message:      "无法启用 Telegram OAuth，请先填写 Bot Token 和 Bot Name",
	},
	{
		enabledKey:   "LinuxDOOAuthEnabled",
		requiredKeys: []string{"LinuxDOClientId", "LinuxDOClientSecret"},
		message:      "无法启用 LinuxDO OAuth，请先填写 Client ID 和 Client Secret",
	},
	{
		enabledKey:   "WeChatAuthEnabled",
		requiredKeys: []string{"WeChatServerAddress", "WeChatServerToken"},
		message:      "无法启用微信登录，请先填写服务器地址和 Token",
	},
	{
		enabledKey:   "TurnstileCheckEnabled",
		requiredKeys: []string{"TurnstileSiteKey", "TurnstileSecretKey"},
		message:      "无法启用 Turnstile 校验，请先填写 Site Key 和 Secret Key",
	},
}

func optionKeysTouched(updates map[string]string, keys ...string) bool {
	for _, key := range keys {
		if _, ok := updates[key]; ok {
			return true
		}
	}
	return false
}

func isOptionEnabled(values map[string]string, key string) bool {
	enabled, err := strconv.ParseBool(strings.TrimSpace(values[key]))
	return err == nil && enabled
}

func hasValidEmailDomain(value string) bool {
	for _, candidate := range strings.FieldsFunc(value, func(r rune) bool {
		return r == ',' || r == ';' || r == '\n' || r == '\r'
	}) {
		domain := strings.TrimSpace(candidate)
		if len(domain) == 0 || len(domain) > 253 || strings.Contains(domain, "@") {
			continue
		}
		labels := strings.Split(domain, ".")
		if len(labels) < 2 {
			continue
		}
		valid := true
		for _, label := range labels {
			if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
				valid = false
				break
			}
			for _, char := range label {
				if (char < 'a' || char > 'z') && (char < 'A' || char > 'Z') &&
					(char < '0' || char > '9') && char != '-' {
					valid = false
					break
				}
			}
			if !valid {
				break
			}
		}
		if valid {
			return true
		}
	}
	return false
}

func validateOptionDependencies(current, updates map[string]string, complianceConfirmed bool) error {
	merged := make(map[string]string, len(current)+len(updates))
	maps.Copy(merged, current)
	maps.Copy(merged, updates)

	if !complianceConfirmed {
		for _, key := range []string{"QuotaForInviter", "QuotaForInvitee"} {
			if value, ok := updates[key]; ok && isPositiveOptionValue(value) {
				return errPaymentComplianceRequired
			}
		}
	}

	for _, requirement := range enabledOptionRequirements {
		keys := append([]string{requirement.enabledKey}, requirement.requiredKeys...)
		if !optionKeysTouched(updates, keys...) || !isOptionEnabled(merged, requirement.enabledKey) {
			continue
		}
		for _, key := range requirement.requiredKeys {
			if strings.TrimSpace(merged[key]) == "" {
				return errors.New(requirement.message)
			}
		}
	}

	if optionKeysTouched(updates, "EmailDomainRestrictionEnabled", "EmailDomainWhitelist") &&
		isOptionEnabled(merged, "EmailDomainRestrictionEnabled") &&
		!hasValidEmailDomain(merged["EmailDomainWhitelist"]) {
		return errors.New("无法启用邮箱域名限制，请至少填写一个有效域名")
	}
	return nil
}

func validateOptionUpdateDependencies(current, updates map[string]string) error {
	confirmed, _ := strconv.ParseBool(strings.TrimSpace(optionValue(current, updates, "payment_setting.compliance_confirmed")))
	termsVersion := strings.TrimSpace(optionValue(current, updates, "payment_setting.compliance_terms_version"))
	return validateOptionDependencies(current, updates, confirmed && termsVersion == operation_setting.CurrentComplianceTermsVersion)
}

func optionValue(current, updates map[string]string, key string) string {
	if value, ok := updates[key]; ok {
		return value
	}
	return current[key]
}

func writeOptionValidationError(c *gin.Context, err error) {
	if errors.Is(err, errPaymentComplianceRequired) {
		common.ApiErrorI18n(c, i18n.MsgPaymentComplianceRequired)
		return
	}
	common.ApiError(c, err)
}

// writeOptionSaveError reports a failed option write. When the values were
// committed but this process could not apply them, data.saved tells the page
// the database changed. Publishing fails on the values themselves, so the
// reload and periodic sync that reuse them usually fail too: the operator has
// to save a corrected value rather than wait.
func writeOptionSaveError(c *gin.Context, err error) {
	if errors.Is(err, model.ErrOptionSavedNotPublished) {
		common.SysError("option saved but not published: " + err.Error())
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "设置已写入数据库，但当前进程未能应用，请查看系统日志并修正后重新保存",
			"data":    gin.H{"saved": true},
		})
		return
	}
	writeOptionValidationError(c, err)
}

func collectModelNamesFromOptionValue(raw string, modelNames map[string]struct{}) {
	if strings.TrimSpace(raw) == "" {
		return
	}

	var parsed map[string]any
	if err := common.UnmarshalJsonStr(raw, &parsed); err != nil {
		return
	}

	for modelName := range parsed {
		modelNames[modelName] = struct{}{}
	}
}

func buildCompletionRatioMetaValue(optionValues map[string]string) string {
	modelNames := make(map[string]struct{})
	for _, key := range completionRatioMetaOptionKeys {
		collectModelNamesFromOptionValue(optionValues[key], modelNames)
	}

	meta := make(map[string]ratio_setting.CompletionRatioInfo, len(modelNames))
	for modelName := range modelNames {
		meta[modelName] = ratio_setting.GetCompletionRatioInfo(modelName)
	}

	jsonBytes, err := common.Marshal(meta)
	if err != nil {
		return "{}"
	}
	return string(jsonBytes)
}

// isSensitiveOptionKey hides credentials from the generic option listing.
// Domains that need to show a credential (moderation keys) expose it masked
// through their own endpoint.
func isSensitiveOptionKey(key string) bool {
	return strings.HasSuffix(key, "Token") ||
		strings.HasSuffix(key, "Secret") ||
		strings.HasSuffix(key, "Key") ||
		strings.HasSuffix(key, "secret") ||
		strings.HasSuffix(key, "api_key") ||
		strings.HasSuffix(key, "api_keys")
}

func GetOptions(c *gin.Context) {
	var options []*model.Option
	optionValues := make(map[string]string)
	common.OptionMapRWMutex.Lock()
	for k, v := range common.OptionMap {
		value := common.Interface2String(v)
		if isSensitiveOptionKey(k) {
			continue
		}
		options = append(options, &model.Option{
			Key:   k,
			Value: value,
		})
		if slices.Contains(completionRatioMetaOptionKeys, k) {
			optionValues[k] = value
		}
	}
	common.OptionMapRWMutex.Unlock()
	options = append(options, &model.Option{
		Key:   "CompletionRatioMeta",
		Value: buildCompletionRatioMetaValue(optionValues),
	})
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    options,
	})
}

type OptionUpdateRequest struct {
	Key   string `json:"key"`
	Value any    `json:"value"`
}

type OptionsBulkUpdateRequest struct {
	Options []OptionUpdateRequest `json:"options"`
}

var errOptionValueMissing = errors.New("设置值不能为空")

// optionValueString converts a decoded JSON value into its stored form. A
// missing value used to be stored as the literal "<nil>" and objects as Go's
// map[...] formatting.
func optionValueString(value any) (string, error) {
	switch typed := value.(type) {
	case nil:
		return "", errOptionValueMissing
	case string:
		return typed, nil
	case bool, float64, int:
		return common.Interface2String(typed), nil
	default:
		data, err := common.Marshal(value)
		if err != nil {
			return "", err
		}
		return string(data), nil
	}
}

var bulkOptionKeys = map[string]struct{}{
	model.ModelPricingOption:           {},
	"billing_setting.task_expressions": {},
	// Pricing and customer tiers.
	"ModelPrice": {}, "ModelRatio": {}, "CacheRatio": {}, "CreateCacheRatio": {},
	"CompletionRatio": {}, "ImageRatio": {}, "BatchInputRatio": {}, "AudioRatio": {}, "AudioCompletionRatio": {},
	"ExposeRatioEnabled": {}, "billing_setting.billing_mode": {}, "billing_setting.billing_expr": {},
	"GroupRatio": {}, "TopupGroupRatio": {}, "UserUsableGroups": {}, "GroupGroupRatio": {},
	"AutoGroups": {}, "DefaultUseAutoGroup": {}, "group_ratio_setting.group_special_usable_group": {},
	"payment_setting.amount_discount": {}, model.PricingGroupEndpointOverridesOption: {}, model.ModelContextWindowsOption: {},

	// Payment gateways. Compliance confirmation remains exclusive to its
	// dedicated endpoint and is intentionally absent from this list.
	"Price": {}, "MinTopUp": {}, "PayMethods": {}, "payment_setting.amount_options": {},
	"PayAddress": {}, "EpayId": {}, "EpayKey": {}, "CustomCallbackAddress": {},
	"StripeApiSecret": {}, "StripeWebhookSecret": {}, "StripePriceId": {},
	"StripeUnitPrice": {}, "StripeMinTopUp": {}, "StripePromotionCodesEnabled": {},
	"CreemApiKey": {}, "CreemWebhookSecret": {}, "CreemTestMode": {}, "CreemProducts": {},
	"WaffoEnabled": {}, "WaffoApiKey": {}, "WaffoPrivateKey": {}, "WaffoPublicCert": {},
	"WaffoSandboxPublicCert": {}, "WaffoSandboxApiKey": {}, "WaffoSandboxPrivateKey": {},
	"WaffoSandbox": {}, "WaffoMerchantId": {}, "WaffoCurrency": {}, "WaffoUnitPrice": {},
	"WaffoMinTopUp": {}, "WaffoNotifyUrl": {}, "WaffoReturnUrl": {}, "WaffoPayMethods": {},
	"WaffoPancakeMerchantID": {}, "WaffoPancakePrivateKey": {},
	"WaffoPancakeStoreID": {}, "WaffoPancakeProductID": {}, "WaffoPancakeReturnURL": {},
	"WaffoPancakeUnitPrice": {}, "WaffoPancakeMinTopUp": {},

	// SSRF, monitoring and performance forms.
	"fetch_setting.enable_ssrf_protection": {}, "fetch_setting.allow_private_ip": {},
	"fetch_setting.domain_filter_mode": {}, "fetch_setting.ip_filter_mode": {},
	"fetch_setting.domain_list": {}, "fetch_setting.ip_list": {},
	"fetch_setting.allowed_ports": {}, "fetch_setting.apply_ip_filter_for_domain": {},
	"ChannelDisableThreshold": {}, "QuotaRemindThreshold": {},
	"AutomaticDisableChannelEnabled": {}, "AutomaticEnableChannelEnabled": {},
	"AutomaticDisableKeywords": {}, "AutomaticDisableStatusCodes": {}, "AutomaticRetryStatusCodes": {},
	"monitor_setting.auto_test_channel_enabled": {}, "monitor_setting.auto_test_channel_minutes": {},
	"monitor_setting.channel_test_mode": {}, "monitor_setting.channel_test_concurrency": {},
	"performance_setting.disk_cache_enabled": {}, "performance_setting.disk_cache_threshold_mb": {},
	"performance_setting.disk_cache_max_size_mb": {}, "performance_setting.disk_cache_path": {},
	"performance_setting.monitor_enabled": {}, "performance_setting.monitor_cpu_threshold": {},
	"performance_setting.monitor_memory_threshold": {}, "performance_setting.monitor_disk_threshold": {},

	// Authentication and registration forms.
	"PasswordLoginEnabled": {}, "PasswordRegisterEnabled": {}, "EmailVerificationEnabled": {},
	"RegisterEnabled": {}, "EmailDomainRestrictionEnabled": {}, "EmailAliasRestrictionEnabled": {},
	"EmailDomainWhitelist": {}, "InvitationCodeEnabled": {}, "InvitationCodeOAuthRequired": {},
	"InvitationCodeUserGenerateEnabled": {}, "InvitationCodePolicy": {},
	"TurnstileCheckEnabled": {}, "TurnstileSiteKey": {}, "TurnstileSecretKey": {},
	"GitHubOAuthEnabled": {}, "GitHubClientId": {}, "GitHubClientSecret": {},
	"discord.enabled": {}, "discord.client_id": {}, "discord.client_secret": {},
	"oidc.enabled": {}, "oidc.display_name": {}, "oidc.client_id": {}, "oidc.client_secret": {},
	"oidc.well_known": {}, "oidc.authorization_endpoint": {}, "oidc.token_endpoint": {},
	"oidc.user_info_endpoint": {},
	"TelegramOAuthEnabled":    {}, "TelegramBotToken": {}, "TelegramBotName": {}, "TelegramClientId": {}, "TelegramClientSecret": {},
	"LinuxDOOAuthEnabled": {}, "LinuxDOClientId": {}, "LinuxDOClientSecret": {},
	"LinuxDOMinimumTrustLevel": {},
	"WeChatAuthEnabled":        {}, "WeChatServerAddress": {}, "WeChatServerToken": {},
	"WeChatAccountQRCodeImageURL": {},
	"passkey.enabled":             {}, "passkey.rp_display_name": {}, "passkey.rp_id": {},
	"passkey.origins": {}, "passkey.allow_insecure_origin": {}, "passkey.user_verification": {},
	"passkey.attachment_preference": {},

	// General, content, quota and model behavior forms.
	"DrawingEnabled": {}, "MjNotifyEnabled": {}, "MjAccountFilterEnabled": {},
	"MjForwardUrlEnabled": {}, "MjModeClearEnabled": {}, "MjActionCheckSuccessEnabled": {},
	"DataExportEnabled": {}, "DataExportDefaultTime": {}, "RetryTimes": {},
	"DefaultCollapseSidebar": {}, "DemoSiteEnabled": {}, "SelfUseModeEnabled": {},
	"CheckSensitiveEnabled": {}, "CheckSensitiveOnPromptEnabled": {}, "SensitiveWords": {},
	"QuotaForNewUser": {}, "PreConsumedQuota": {}, "QuotaForInviter": {}, "QuotaForInvitee": {},
	"TopUpLink": {}, "general_setting.docs_link": {},
	"quota_setting.enable_free_model_pre_consume": {}, "AffiliateEnabled": {},
	"Notice": {}, "SystemName": {}, "ServerAddress": {}, "GroupMonitoringURL": {},
	"Logo": {}, "Footer": {}, "About": {}, "HomePageContent": {},
	"legal.user_agreement": {}, "legal.privacy_policy": {},
	"QuotaPerUnit": {}, "USDExchangeRate": {}, "DisplayInCurrencyEnabled": {},
	"DisplayTokenStatEnabled": {}, "general_setting.quota_display_type": {},
	"general_setting.custom_currency_symbol": {}, "general_setting.custom_currency_exchange_rate": {},
	"checkin_setting.enabled": {}, "checkin_setting.min_quota": {}, "checkin_setting.max_quota": {},
	"global.pass_through_request_enabled": {}, "global.thinking_model_blacklist": {},
	"global.chat_completions_to_responses_policy": {},
	"general_setting.ping_interval_enabled":       {}, "general_setting.ping_interval_seconds": {},
	"gemini.safety_settings": {}, "gemini.version_settings": {}, "gemini.supported_imagine_models": {},
	"gemini.thinking_adapter_enabled": {}, "gemini.thinking_adapter_budget_tokens_percentage": {},
	"gemini.function_call_thought_signature_enabled": {}, "gemini.remove_function_response_id_enabled": {},
	"claude.model_headers_settings": {}, "claude.default_max_tokens": {},
	"claude.thinking_adapter_enabled": {}, "claude.thinking_adapter_budget_tokens_percentage": {},
	"grok.violation_deduction_enabled": {}, "grok.violation_deduction_amount": {},
	"ModelRequestRateLimitEnabled": {}, "ModelRequestRateLimitDurationMinutes": {},
	"ModelRequestRateLimitCount": {}, "ModelRequestRateLimitSuccessCount": {},
	"ModelRequestRateLimitGroup": {}, "ModelRPMConfig": {},

	// Email, worker, deployment, access, affinity, group monitoring and tickets.
	"SMTPServer": {}, "SMTPPort": {}, "SMTPAccount": {}, "SMTPFrom": {}, "SMTPToken": {},
	"SMTPArchiveBcc": {}, "SMTPSSLEnabled": {}, "SMTPStartTLSEnabled": {},
	"SMTPInsecureSkipVerify": {}, "SMTPForceAuthLogin": {},
	"WorkerUrl": {}, "WorkerValidKey": {}, "WorkerAllowHttpImageRequestEnabled": {},
	"model_deployment.ionet.enabled": {}, "model_deployment.ionet.api_key": {},
	"web_group_setting.enabled": {}, "web_group_setting.rules": {},
	"channel_affinity_setting.enabled": {}, "channel_affinity_setting.switch_on_success": {},
	"channel_affinity_setting.failover_on_channel_down": {}, "channel_affinity_setting.max_entries": {},
	"channel_affinity_setting.default_ttl_seconds": {}, "channel_affinity_setting.rules": {},
	"group_monitoring_setting.monitoring_groups": {}, "group_monitoring_setting.group_display_order": {},
	"group_monitoring_setting.availability_period_minutes":       {},
	"group_monitoring_setting.cache_hit_period_minutes":          {},
	"group_monitoring_setting.aggregation_interval_minutes":      {},
	"group_monitoring_setting.availability_exclude_models":       {},
	"group_monitoring_setting.cache_hit_exclude_models":          {},
	"group_monitoring_setting.availability_exclude_keywords":     {},
	"group_monitoring_setting.availability_exclude_status_codes": {},
	"group_monitoring_setting.cache_tokens_separate_groups":      {},
	"group_monitoring_setting.group_categories":                  {},
	"group_monitoring_setting.category_order":                    {},
	"TicketNotifyEnabled":                                        {}, "TicketAdminEmail": {}, "TicketAttachmentEnabled": {},
	"TicketAttachmentMaxSize": {}, "TicketAttachmentMaxCount": {}, "TicketAttachmentAllowedExts": {},
	"TicketAttachmentLocalPath": {}, "TicketAttachmentSignedURLTTL": {},
	"TicketReplyAttachmentEnabled": {}, "TicketReplyAttachmentMaxBytes": {},
	"TicketInvoiceMinTopupMoney": {},
}

func isBulkOptionAllowed(key string) bool {
	if _, ok := bulkOptionKeys[key]; ok {
		return true
	}
	if !strings.HasPrefix(key, constant.EmailTemplateOptionPrefix) {
		return false
	}
	templateKey, field, ok := strings.Cut(strings.TrimPrefix(key, constant.EmailTemplateOptionPrefix), ".")
	if !ok || (field != "subject" && field != "body") {
		return false
	}
	_, registered := constant.FindEmailTemplateSpec(templateKey)
	return registered
}

// UpdateOptionsBulk 原子保存一组相关设置，任一项无效或落库失败时整组不生效。
func UpdateOptionsBulk(c *gin.Context) {
	var request OptionsBulkUpdateRequest
	if err := common.DecodeJson(c.Request.Body, &request); err != nil || len(request.Options) == 0 || len(request.Options) > 100 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "无效的参数"})
		return
	}
	values := make(map[string]string, len(request.Options))
	for _, option := range request.Options {
		key := strings.TrimSpace(option.Key)
		if key == "" {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "设置键不能为空"})
			return
		}
		if !isBulkOptionAllowed(key) {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "批量接口不允许更新该设置"})
			return
		}
		if _, duplicated := values[key]; duplicated {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "批量设置键不能重复"})
			return
		}
		value, err := optionValueString(option.Value)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
			return
		}
		values[key] = value
	}
	if err := model.UpdateOptionsBulkValidated(values, func(current map[string]string) error {
		return validateOptionUpdateDependencies(current, values)
	}); err != nil {
		writeOptionSaveError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": ""})
}

func UpdateOption(c *gin.Context) {
	var option OptionUpdateRequest
	err := common.DecodeJson(c.Request.Body, &option)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "无效的参数",
		})
		return
	}
	value, err := optionValueString(option.Value)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}
	if isPaymentComplianceOptionKey(option.Key) {
		common.ApiErrorMsg(c, "合规确认字段不允许通过通用设置接口修改")
		return
	}
	err = model.UpdateOptionValidated(option.Key, value, func(current map[string]string) error {
		return validateOptionUpdateDependencies(current, map[string]string{option.Key: value})
	})
	if err != nil {
		writeOptionSaveError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
	})
}

// TestSMTPEmail sends a test email using current SMTP settings to verify configuration.
// Body: {"to": "recipient@example.com"}
func TestSMTPEmail(c *gin.Context) {
	var req struct {
		To string `json:"to"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "请求体无效: " + err.Error(),
		})
		return
	}
	to := strings.TrimSpace(req.To)
	if err := common.Validate.Var(to, "required,email"); err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "无效的收件邮箱",
		})
		return
	}
	smtpSettings := common.GetSMTPSettings()
	branding := common.GetBrandingSettings()
	if smtpSettings.Server == "" || smtpSettings.Account == "" {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "SMTP 未配置完整 (Server / Account 缺失)",
		})
		return
	}
	subject := fmt.Sprintf("[%s] SMTP 测试邮件", branding.SystemName)
	content := fmt.Sprintf(
		"<p>这是一封来自 <strong>%s</strong> 的 SMTP 测试邮件。</p>"+
			"<p>如果你看到这封邮件，说明 SMTP 配置工作正常。</p>"+
			"<ul>"+
			"<li>SMTP Server: %s:%d</li>"+
			"<li>SSL/TLS: %v</li>"+
			"<li>From: %s</li>"+
			"</ul>",
		branding.SystemName, smtpSettings.Server, smtpSettings.Port, smtpSettings.SSLEnabled, smtpSettings.From)
	if err := common.SendEmail(subject, to, content); err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "发送失败: " + err.Error(),
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "已发送，请到收件箱（含垃圾邮件夹）查看",
	})
}
