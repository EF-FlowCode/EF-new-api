package model

import (
	"errors"
	"fmt"
	"maps"
	"math"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"eflow.local/new-api/common"
	"eflow.local/new-api/constant"
	"eflow.local/new-api/pkg/knowledge"
	"eflow.local/new-api/setting"
	"eflow.local/new-api/setting/billing_setting"
	"eflow.local/new-api/setting/config"
	"eflow.local/new-api/setting/console_setting"
	"eflow.local/new-api/setting/operation_setting"
	"eflow.local/new-api/setting/performance_setting"
	"eflow.local/new-api/setting/ratio_setting"
	"eflow.local/new-api/setting/system_setting"
	"gorm.io/gorm"
)

type Option struct {
	Key   string `json:"key" gorm:"primaryKey"`
	Value string `json:"value"`
}

// optionMutationMutex keeps database commit order and in-process publication
// order aligned. It also serializes dependency validation with the write that
// follows it.
var optionMutationMutex sync.Mutex

// ErrOptionSavedNotPublished reports that an update was committed to the
// database but could not be applied to the running process.
var ErrOptionSavedNotPublished = errors.New("option saved but not applied to the running process")

func AllOption() ([]*Option, error) {
	var options []*Option
	var err error
	err = DB.Find(&options).Error
	return options, err
}

func InitOptionMap() error {
	authSettings := common.GetAuthSettings()
	userQuotaSettings := common.GetUserQuotaSettings()
	brandingSettings := common.GetBrandingSettings()
	featureSettings := common.GetFeatureSettings()
	smtpSettings := common.GetSMTPSettings()
	workerSettings := system_setting.GetWorkerSettings()
	quotaPerUnit := common.GetQuotaPerUnit()
	paymentNotificationSettings := common.GetPaymentNotificationSettings()
	ticketNotificationSettings := common.GetTicketNotificationSettings()
	ticketAttachmentSettings := setting.GetTicketAttachmentSettings()
	serverSettings := system_setting.GetServerSettings()
	stripeConfig := setting.GetStripeConfig()
	creemConfig := setting.GetCreemConfig()
	waffoConfig := setting.GetWaffoConfig()
	epayConfig := operation_setting.GetEpayConfig()
	waffoPancakeConfig := setting.GetWaffoPancakeConfig()
	legacyOAuthSettings := system_setting.GetLegacyOAuthSettings()
	modelRequestRateLimitSettings := setting.GetModelRequestRateLimitSettings()
	sensitiveSettings := setting.GetSensitiveSettings()
	autoGroupPolicy := setting.GetAutoGroupPolicy()
	midjourneySettings := setting.GetMidjourneySettings()
	channelFailurePolicy := operation_setting.GetChannelFailurePolicy()
	siteModeSettings := operation_setting.GetSiteModeSettings()
	waffoPayMethods := marshalOptionJSON(waffoConfig.PayMethods, "[]")
	epayPayMethods := marshalOptionJSON(epayConfig.PayMethods, "[]")

	common.OptionMapRWMutex.Lock()
	common.OptionMap = make(map[string]string)

	// 添加原有的系统配置
	common.OptionMap["FileUploadPermission"] = strconv.Itoa(common.RoleGuestUser)
	common.OptionMap["FileDownloadPermission"] = strconv.Itoa(common.RoleGuestUser)
	common.OptionMap["ImageUploadPermission"] = strconv.Itoa(common.RoleGuestUser)
	common.OptionMap["ImageDownloadPermission"] = strconv.Itoa(common.RoleGuestUser)
	common.OptionMap["PasswordLoginEnabled"] = strconv.FormatBool(authSettings.PasswordLoginEnabled)
	common.OptionMap["PasswordRegisterEnabled"] = strconv.FormatBool(authSettings.PasswordRegisterEnabled)
	common.OptionMap["EmailVerificationEnabled"] = strconv.FormatBool(authSettings.EmailVerificationEnabled)
	common.OptionMap["GitHubOAuthEnabled"] = strconv.FormatBool(legacyOAuthSettings.GitHubOAuthEnabled)
	common.OptionMap["LinuxDOOAuthEnabled"] = strconv.FormatBool(legacyOAuthSettings.LinuxDOOAuthEnabled)
	common.OptionMap["TelegramOAuthEnabled"] = strconv.FormatBool(legacyOAuthSettings.TelegramOAuthEnabled)
	common.OptionMap["WeChatAuthEnabled"] = strconv.FormatBool(legacyOAuthSettings.WeChatAuthEnabled)
	common.OptionMap["TurnstileCheckEnabled"] = strconv.FormatBool(authSettings.TurnstileCheckEnabled)
	common.OptionMap["RegisterEnabled"] = strconv.FormatBool(authSettings.RegistrationEnabled)
	common.OptionMap["InvitationCodeEnabled"] = strconv.FormatBool(authSettings.InvitationCodeEnabled)
	common.OptionMap["InvitationCodeOAuthRequired"] = strconv.FormatBool(authSettings.InvitationCodeOAuthRequired)
	common.OptionMap["InvitationCodeUserGenerateEnabled"] = strconv.FormatBool(authSettings.InvitationCodeUserGenerateEnabled)
	common.OptionMap["InvitationCodePolicy"] = setting.GetInvitationCodePolicyJSON()
	common.OptionMap["AutomaticDisableChannelEnabled"] = strconv.FormatBool(channelFailurePolicy.AutomaticDisableChannelEnabled)
	common.OptionMap["AutomaticEnableChannelEnabled"] = strconv.FormatBool(channelFailurePolicy.AutomaticEnableChannelEnabled)
	common.OptionMap["LogConsumeEnabled"] = strconv.FormatBool(featureSettings.LogConsumeEnabled)
	common.OptionMap["DisplayInCurrencyEnabled"] = strconv.FormatBool(featureSettings.DisplayInCurrencyEnabled)
	common.OptionMap["DisplayTokenStatEnabled"] = strconv.FormatBool(featureSettings.DisplayTokenStatEnabled)
	common.OptionMap["DrawingEnabled"] = strconv.FormatBool(featureSettings.DrawingEnabled)
	common.OptionMap["TaskEnabled"] = strconv.FormatBool(featureSettings.TaskEnabled)
	common.OptionMap["DataExportEnabled"] = strconv.FormatBool(featureSettings.DataExportEnabled)
	common.OptionMap["ChannelDisableThreshold"] = strconv.FormatFloat(channelFailurePolicy.ChannelDisableThreshold, 'f', -1, 64)
	common.OptionMap["EmailDomainRestrictionEnabled"] = strconv.FormatBool(authSettings.EmailDomainRestrictionEnabled)
	common.OptionMap["EmailAliasRestrictionEnabled"] = strconv.FormatBool(authSettings.EmailAliasRestrictionEnabled)
	common.OptionMap["EmailDomainWhitelist"] = strings.Join(authSettings.EmailDomainWhitelist, ",")
	common.OptionMap["SMTPServer"] = smtpSettings.Server
	common.OptionMap["SMTPFrom"] = smtpSettings.From
	common.OptionMap["SMTPPort"] = strconv.Itoa(smtpSettings.Port)
	common.OptionMap["SMTPAccount"] = smtpSettings.Account
	common.OptionMap["SMTPToken"] = smtpSettings.Token
	common.OptionMap["SMTPArchiveBcc"] = smtpSettings.ArchiveBcc
	common.OptionMap["SMTPSSLEnabled"] = strconv.FormatBool(smtpSettings.SSLEnabled)
	common.OptionMap["SMTPStartTLSEnabled"] = strconv.FormatBool(smtpSettings.StartTLSEnabled)
	common.OptionMap["SMTPInsecureSkipVerify"] = strconv.FormatBool(smtpSettings.InsecureSkipVerify)
	common.OptionMap["SMTPForceAuthLogin"] = strconv.FormatBool(smtpSettings.ForceAuthLogin)
	common.OptionMap["TicketNotifyEnabled"] = strconv.FormatBool(ticketNotificationSettings.Enabled)
	common.OptionMap["TicketAdminEmail"] = ticketNotificationSettings.AdminEmail
	common.OptionMap["TicketReplyAttachmentEnabled"] = strconv.FormatBool(ticketNotificationSettings.ReplyAttachmentEnabled)
	common.OptionMap["TicketReplyAttachmentMaxBytes"] = strconv.FormatInt(ticketNotificationSettings.ReplyAttachmentMaxBytes, 10)
	common.OptionMap["TicketInvoiceMinTopupMoney"] = strconv.FormatFloat(ticketNotificationSettings.InvoiceMinTopupMoney, 'f', -1, 64)
	common.OptionMap["TicketAttachmentEnabled"] = strconv.FormatBool(ticketAttachmentSettings.Enabled)
	common.OptionMap["TicketAttachmentMaxSize"] = strconv.FormatInt(ticketAttachmentSettings.MaxSize, 10)
	common.OptionMap["TicketAttachmentMaxCount"] = strconv.Itoa(ticketAttachmentSettings.MaxCount)
	common.OptionMap["TicketAttachmentAllowedExts"] = ticketAttachmentSettings.AllowedExts
	common.OptionMap["TicketAttachmentAllowedMimes"] = ticketAttachmentSettings.AllowedMimes
	common.OptionMap["TicketAttachmentStorage"] = ticketAttachmentSettings.Storage
	common.OptionMap["TicketAttachmentLocalPath"] = ticketAttachmentSettings.LocalPath
	common.OptionMap["TicketAttachmentSignedURLTTL"] = strconv.FormatInt(ticketAttachmentSettings.SignedURLTTL, 10)
	common.OptionMap["TicketAttachmentOSSEndpoint"] = ticketAttachmentSettings.OSSEndpoint
	common.OptionMap["TicketAttachmentOSSBucket"] = ticketAttachmentSettings.OSSBucket
	common.OptionMap["TicketAttachmentOSSRegion"] = ticketAttachmentSettings.OSSRegion
	common.OptionMap["TicketAttachmentOSSAccessKeyId"] = ticketAttachmentSettings.OSSAccessKeyID
	common.OptionMap["TicketAttachmentOSSAccessKeySecret"] = ticketAttachmentSettings.OSSAccessKeySecret
	common.OptionMap["TicketAttachmentOSSCustomDomain"] = ticketAttachmentSettings.OSSCustomDomain
	common.OptionMap["TicketAttachmentS3Endpoint"] = ticketAttachmentSettings.S3Endpoint
	common.OptionMap["TicketAttachmentS3Bucket"] = ticketAttachmentSettings.S3Bucket
	common.OptionMap["TicketAttachmentS3Region"] = ticketAttachmentSettings.S3Region
	common.OptionMap["TicketAttachmentS3AccessKeyId"] = ticketAttachmentSettings.S3AccessKeyID
	common.OptionMap["TicketAttachmentS3AccessKeySecret"] = ticketAttachmentSettings.S3AccessKeySecret
	common.OptionMap["TicketAttachmentS3CustomDomain"] = ticketAttachmentSettings.S3CustomDomain
	common.OptionMap["TicketAttachmentCOSEndpoint"] = ticketAttachmentSettings.COSEndpoint
	common.OptionMap["TicketAttachmentCOSBucket"] = ticketAttachmentSettings.COSBucket
	common.OptionMap["TicketAttachmentCOSRegion"] = ticketAttachmentSettings.COSRegion
	common.OptionMap["TicketAttachmentCOSSecretId"] = ticketAttachmentSettings.COSSecretID
	common.OptionMap["TicketAttachmentCOSSecretKey"] = ticketAttachmentSettings.COSSecretKey
	common.OptionMap["TicketAttachmentCOSCustomDomain"] = ticketAttachmentSettings.COSCustomDomain
	common.OptionMap["TicketAssignConfig"] = setting.TicketAssignConfig2JSONString()
	common.OptionMap["PaymentNotifyUserEnabled"] = strconv.FormatBool(paymentNotificationSettings.UserEnabled)
	common.OptionMap["PaymentNotifyAdminEnabled"] = strconv.FormatBool(paymentNotificationSettings.AdminEnabled)
	common.OptionMap["PaymentAdminEmail"] = paymentNotificationSettings.AdminEmail
	common.OptionMap["Notice"] = ""
	common.OptionMap["About"] = ""
	common.OptionMap["HomePageContent"] = ""
	common.OptionMap["Footer"] = brandingSettings.Footer
	common.OptionMap["SystemName"] = brandingSettings.SystemName
	common.OptionMap["Logo"] = brandingSettings.Logo
	common.OptionMap["ServerAddress"] = serverSettings.Address
	common.OptionMap["WorkerUrl"] = workerSettings.URL
	common.OptionMap["WorkerValidKey"] = workerSettings.ValidKey
	common.OptionMap["WorkerAllowHttpImageRequestEnabled"] = strconv.FormatBool(workerSettings.AllowHTTPImageRequest)
	common.OptionMap["PayAddress"] = epayConfig.PayAddress
	common.OptionMap["CustomCallbackAddress"] = epayConfig.CustomCallbackAddress
	common.OptionMap["EpayId"] = epayConfig.EpayID
	common.OptionMap["EpayKey"] = epayConfig.EpayKey
	common.OptionMap["Price"] = strconv.FormatFloat(epayConfig.Price, 'f', -1, 64)
	common.OptionMap["USDExchangeRate"] = strconv.FormatFloat(epayConfig.USDExchangeRate, 'f', -1, 64)
	common.OptionMap["MinTopUp"] = strconv.Itoa(epayConfig.MinTopUp)
	common.OptionMap["StripeMinTopUp"] = strconv.Itoa(stripeConfig.MinTopUp)
	common.OptionMap["StripeApiSecret"] = stripeConfig.APISecret
	common.OptionMap["StripeWebhookSecret"] = stripeConfig.WebhookSecret
	common.OptionMap["StripePriceId"] = stripeConfig.PriceID
	common.OptionMap["StripeUnitPrice"] = strconv.FormatFloat(stripeConfig.UnitPrice, 'f', -1, 64)
	common.OptionMap["StripePromotionCodesEnabled"] = strconv.FormatBool(stripeConfig.PromotionCodesEnabled)
	common.OptionMap["CreemApiKey"] = creemConfig.APIKey
	common.OptionMap["CreemProducts"] = creemConfig.ProductsJSON
	common.OptionMap["CreemTestMode"] = strconv.FormatBool(creemConfig.TestMode)
	common.OptionMap["CreemWebhookSecret"] = creemConfig.WebhookSecret
	common.OptionMap["WaffoEnabled"] = strconv.FormatBool(waffoConfig.Enabled)
	common.OptionMap["WaffoApiKey"] = waffoConfig.APIKey
	common.OptionMap["WaffoPrivateKey"] = waffoConfig.PrivateKey
	common.OptionMap["WaffoPublicCert"] = waffoConfig.PublicCert
	common.OptionMap["WaffoSandboxPublicCert"] = waffoConfig.SandboxPublicCert
	common.OptionMap["WaffoSandboxApiKey"] = waffoConfig.SandboxAPIKey
	common.OptionMap["WaffoSandboxPrivateKey"] = waffoConfig.SandboxPrivateKey
	common.OptionMap["WaffoSandbox"] = strconv.FormatBool(waffoConfig.Sandbox)
	common.OptionMap["WaffoMerchantId"] = waffoConfig.MerchantID
	common.OptionMap["WaffoNotifyUrl"] = waffoConfig.NotifyURL
	common.OptionMap["WaffoReturnUrl"] = waffoConfig.ReturnURL
	common.OptionMap["WaffoCurrency"] = waffoConfig.Currency
	common.OptionMap["WaffoUnitPrice"] = strconv.FormatFloat(waffoConfig.UnitPrice, 'f', -1, 64)
	common.OptionMap["WaffoMinTopUp"] = strconv.Itoa(waffoConfig.MinTopUp)
	common.OptionMap["WaffoPayMethods"] = waffoPayMethods
	common.OptionMap["WaffoPancakeMerchantID"] = waffoPancakeConfig.MerchantID
	common.OptionMap["WaffoPancakePrivateKey"] = waffoPancakeConfig.PrivateKey
	common.OptionMap["WaffoPancakeReturnURL"] = waffoPancakeConfig.ReturnURL
	common.OptionMap["WaffoPancakeUnitPrice"] = strconv.FormatFloat(waffoPancakeConfig.UnitPrice, 'f', -1, 64)
	common.OptionMap["WaffoPancakeMinTopUp"] = strconv.Itoa(waffoPancakeConfig.MinTopUp)
	common.OptionMap["WaffoPancakeStoreID"] = waffoPancakeConfig.StoreID
	common.OptionMap["WaffoPancakeProductID"] = waffoPancakeConfig.ProductID
	common.OptionMap["TopupGroupRatio"] = common.TopupGroupRatio2JSONString()
	common.OptionMap["Chats"] = setting.Chats2JsonString()
	common.OptionMap["AutoGroups"] = marshalOptionJSON(autoGroupPolicy.Groups, "[]")
	common.OptionMap["DefaultUseAutoGroup"] = strconv.FormatBool(autoGroupPolicy.DefaultUseAutoGroup)
	common.OptionMap["PayMethods"] = epayPayMethods
	common.OptionMap["GitHubClientId"] = legacyOAuthSettings.GitHubClientId
	common.OptionMap["GitHubClientSecret"] = legacyOAuthSettings.GitHubClientSecret
	common.OptionMap["LinuxDOClientId"] = legacyOAuthSettings.LinuxDOClientId
	common.OptionMap["LinuxDOClientSecret"] = legacyOAuthSettings.LinuxDOClientSecret
	common.OptionMap["LinuxDOMinimumTrustLevel"] = strconv.Itoa(legacyOAuthSettings.LinuxDOMinimumTrustLevel)
	common.OptionMap["TelegramBotToken"] = legacyOAuthSettings.TelegramBotToken
	common.OptionMap["TelegramBotName"] = legacyOAuthSettings.TelegramBotName
	common.OptionMap["TelegramClientId"] = legacyOAuthSettings.TelegramClientId
	common.OptionMap["TelegramClientSecret"] = legacyOAuthSettings.TelegramClientSecret
	common.OptionMap["WeChatServerAddress"] = legacyOAuthSettings.WeChatServerAddress
	common.OptionMap["WeChatServerToken"] = legacyOAuthSettings.WeChatServerToken
	common.OptionMap["WeChatAccountQRCodeImageURL"] = legacyOAuthSettings.WeChatAccountQRCodeImageURL
	common.OptionMap["TurnstileSiteKey"] = ""
	common.OptionMap["TurnstileSecretKey"] = ""
	common.OptionMap["QuotaForNewUser"] = strconv.Itoa(userQuotaSettings.QuotaForNewUser)
	common.OptionMap["QuotaForInviter"] = strconv.Itoa(userQuotaSettings.QuotaForInviter)
	common.OptionMap["QuotaForInvitee"] = strconv.Itoa(userQuotaSettings.QuotaForInvitee)
	common.OptionMap["AffiliateEnabled"] = strconv.FormatBool(userQuotaSettings.AffiliateEnabled)
	common.OptionMap["QuotaRemindThreshold"] = strconv.Itoa(userQuotaSettings.QuotaRemindThreshold)
	common.OptionMap["PreConsumedQuota"] = strconv.Itoa(userQuotaSettings.PreConsumedQuota)
	common.OptionMap["ModelRequestRateLimitCount"] = strconv.Itoa(modelRequestRateLimitSettings.TotalCount)
	common.OptionMap["ModelRequestRateLimitDurationMinutes"] = strconv.Itoa(modelRequestRateLimitSettings.DurationMinutes)
	common.OptionMap["ModelRequestRateLimitSuccessCount"] = strconv.Itoa(modelRequestRateLimitSettings.SuccessCount)
	common.OptionMap["ModelRequestRateLimitGroup"] = setting.ModelRequestRateLimitGroup2JSONString()
	common.OptionMap["ModelRPMConfig"] = setting.ModelRPMConfig2JSONString()
	common.OptionMap[PricingGroupEndpointOverridesOption] = "{}"
	common.OptionMap[ModelContextWindowsOption] = "{}"
	common.OptionMap["ModelRatio"] = ratio_setting.ModelRatio2JSONString()
	common.OptionMap["ModelPrice"] = ratio_setting.ModelPrice2JSONString()
	common.OptionMap["CacheRatio"] = ratio_setting.CacheRatio2JSONString()
	common.OptionMap["CreateCacheRatio"] = ratio_setting.CreateCacheRatio2JSONString()
	common.OptionMap["GroupRatio"] = ratio_setting.GroupRatio2JSONString()
	common.OptionMap["GroupGroupRatio"] = ratio_setting.GroupGroupRatio2JSONString()
	common.OptionMap["UserUsableGroups"] = setting.UserUsableGroups2JSONString()
	common.OptionMap["CompletionRatio"] = ratio_setting.CompletionRatio2JSONString()
	common.OptionMap["ImageRatio"] = ratio_setting.ImageRatio2JSONString()
	common.OptionMap["BatchInputRatio"] = ratio_setting.BatchInputRatio2JSONString()
	common.OptionMap["AudioRatio"] = ratio_setting.AudioRatio2JSONString()
	common.OptionMap["AudioCompletionRatio"] = ratio_setting.AudioCompletionRatio2JSONString()
	common.OptionMap["TopUpLink"] = brandingSettings.TopUpLink
	common.OptionMap["GroupMonitoringURL"] = brandingSettings.GroupMonitoringURL
	//common.OptionMap["ChatLink"] = common.ChatLink
	//common.OptionMap["ChatLink2"] = common.ChatLink2
	common.OptionMap["QuotaPerUnit"] = strconv.FormatFloat(quotaPerUnit, 'f', -1, 64)
	common.OptionMap["RetryTimes"] = strconv.Itoa(channelFailurePolicy.RetryTimes)
	common.OptionMap["DataExportInterval"] = strconv.Itoa(featureSettings.DataExportInterval)
	common.OptionMap["DataExportDefaultTime"] = featureSettings.DataExportDefaultTime
	common.OptionMap["DefaultCollapseSidebar"] = strconv.FormatBool(featureSettings.DefaultCollapseSidebar)
	common.OptionMap["MjNotifyEnabled"] = strconv.FormatBool(midjourneySettings.NotifyEnabled)
	common.OptionMap["MjAccountFilterEnabled"] = strconv.FormatBool(midjourneySettings.AccountFilterEnabled)
	common.OptionMap["MjModeClearEnabled"] = strconv.FormatBool(midjourneySettings.ModeClearEnabled)
	common.OptionMap["MjForwardUrlEnabled"] = strconv.FormatBool(midjourneySettings.ForwardURLEnabled)
	common.OptionMap["MjActionCheckSuccessEnabled"] = strconv.FormatBool(midjourneySettings.ActionCheckSuccessEnabled)
	common.OptionMap["CheckSensitiveEnabled"] = strconv.FormatBool(sensitiveSettings.CheckEnabled)
	common.OptionMap["DemoSiteEnabled"] = strconv.FormatBool(siteModeSettings.DemoSiteEnabled)
	common.OptionMap["SelfUseModeEnabled"] = strconv.FormatBool(siteModeSettings.SelfUseModeEnabled)
	common.OptionMap["ModelRequestRateLimitEnabled"] = strconv.FormatBool(modelRequestRateLimitSettings.Enabled)
	common.OptionMap["CheckSensitiveOnPromptEnabled"] = strconv.FormatBool(sensitiveSettings.CheckOnPromptEnabled)
	common.OptionMap["StopOnSensitiveEnabled"] = strconv.FormatBool(sensitiveSettings.StopOnSensitive)
	common.OptionMap["SensitiveWords"] = sensitiveSettings.WordsString()
	common.OptionMap["StreamCacheQueueLength"] = strconv.Itoa(sensitiveSettings.StreamCacheQueueLength)
	common.OptionMap["AutomaticDisableKeywords"] = channelFailurePolicy.AutomaticDisableKeywordsString()
	common.OptionMap["AutomaticDisableStatusCodes"] = channelFailurePolicy.AutomaticDisableStatusCodesString()
	common.OptionMap["AutomaticRetryStatusCodes"] = channelFailurePolicy.AutomaticRetryStatusCodesString()
	common.OptionMap["ExposeRatioEnabled"] = strconv.FormatBool(ratio_setting.IsExposeRatioEnabled())

	// 自动添加所有注册的模型配置
	modelConfigs := config.GlobalConfig.ExportAllConfigs()
	maps.Copy(common.OptionMap, modelConfigs)

	common.OptionMapRWMutex.Unlock()
	if shouldRunLegacyKnowledgePriceMigration() {
		if err := migrateLegacyKnowledgePricesFromDatabase(); err != nil {
			return err
		}
		if err := migrateModelPricingFromDatabase(); err != nil {
			return err
		}
	}
	if err := loadOptionsFromDatabase(); err != nil {
		common.SysLog("failed to load options from database: " + err.Error())
		return err
	}
	return nil
}

func marshalOptionJSON(value any, fallback string) string {
	encoded, err := common.Marshal(value)
	if err != nil {
		return fallback
	}
	return string(encoded)
}

func loadOptionsFromDatabase() error {
	optionMutationMutex.Lock()
	defer optionMutationMutex.Unlock()
	return loadOptionsFromDatabaseLocked()
}

func loadOptionsFromDatabaseLocked() error {
	options, err := AllOption()
	if err != nil {
		return err
	}
	values := make(map[string]string, len(options))
	for _, option := range options {
		values[option.Key] = option.Value
	}
	normalized, err := normalizeOptionValues(values)
	if err != nil {
		return err
	}
	if _, legacy := normalized["billing_setting.knowledge_prices"]; legacy {
		return fmt.Errorf("legacy knowledge pricing requires a migration role")
	}
	if _, migrated := normalized[ModelPricingOption]; !migrated {
		return fmt.Errorf("model pricing requires a migration role")
	}
	for key := range modelPricingFields {
		if _, legacy := normalized[key]; legacy {
			return fmt.Errorf("mixed model pricing storage: %s", key)
		}
	}
	if err = validateOptionValues(normalized); err != nil {
		return err
	}
	return publishOptionValues(normalized)
}

// migrateLegacyKnowledgePrices converts the removed knowledge-only pricing
// option into the regular model pricing maps exactly once. Runtime code never
// reads the legacy key; this is only a startup data migration for existing
// installations.
func shouldRunLegacyKnowledgePriceMigration() bool {
	return common.DeploymentRole == "migrate" || (common.DeploymentRole == "" && common.IsMasterNode)
}

func migrateLegacyKnowledgePricesFromDatabase() error {
	options, err := AllOption()
	if err != nil {
		return err
	}
	values := make(map[string]string, len(options))
	for _, option := range options {
		values[option.Key] = option.Value
	}
	normalized, err := normalizeOptionValues(values)
	if err != nil {
		return err
	}
	return migrateLegacyKnowledgePricesValues(normalized)
}

func migrateLegacyKnowledgePricesValues(values map[string]string) error {
	raw, ok := values["billing_setting.knowledge_prices"]
	if !ok || strings.TrimSpace(raw) == "" {
		return nil
	}
	var legacy map[string]knowledge.Prices
	if err := common.Unmarshal([]byte(raw), &legacy); err != nil {
		return fmt.Errorf("invalid legacy knowledge pricing: %w", err)
	}
	modelRatios := map[string]float64{}
	imageRatios := map[string]float64{}
	batchRatios := map[string]float64{}
	modes := map[string]string{}
	expressions := map[string]string{}
	for key, target := range map[string]any{
		"ModelRatio":                   &modelRatios,
		"ImageRatio":                   &imageRatios,
		"BatchInputRatio":              &batchRatios,
		"billing_setting.billing_mode": &modes,
		"billing_setting.billing_expr": &expressions,
	} {
		if current, exists := values[key]; exists && strings.TrimSpace(current) != "" {
			if err := common.Unmarshal([]byte(current), target); err != nil {
				return fmt.Errorf("invalid %s while migrating knowledge pricing: %w", key, err)
			}
		}
	}
	for name, price := range legacy {
		if price.Input == nil {
			continue
		}
		modelRatios[name] = *price.Input / 2
		if price.Image != nil && *price.Input > 0 {
			imageRatios[name] = *price.Image / *price.Input
		}
		if price.BatchInput != nil {
			batchRatios[name] = *price.BatchInput / 2
		}
		modes[name] = billing_setting.BillingModeTieredExpr
		expressions[name] = price.Expression()
	}
	marshal := func(value any) (string, error) {
		encoded, err := common.Marshal(value)
		return string(encoded), err
	}
	for key, value := range map[string]any{
		"ModelRatio":                   modelRatios,
		"ImageRatio":                   imageRatios,
		"BatchInputRatio":              batchRatios,
		"billing_setting.billing_mode": modes,
		"billing_setting.billing_expr": expressions,
	} {
		encoded, err := marshal(value)
		if err != nil {
			return err
		}
		values[key] = encoded
	}
	delete(values, "billing_setting.knowledge_prices")
	return DB.Transaction(func(tx *gorm.DB) error {
		if err := saveOptionValuesWithTx(tx, values); err != nil {
			return err
		}
		return tx.Where("`key` = ?", "billing_setting.knowledge_prices").Delete(&Option{}).Error
	})
}

// ReloadOptions refreshes the same validated snapshot used by periodic sync.
func ReloadOptions() error { return loadOptionsFromDatabase() }

func SyncOptions(frequency int) {
	for {
		time.Sleep(time.Duration(frequency) * time.Second)
		common.SysLog("syncing options from database")
		if err := loadOptionsFromDatabase(); err != nil {
			common.SysLog("failed to sync options from database: " + err.Error())
		}
	}
}

func UpdateOption(key string, value string) error {
	return UpdateOptionValidated(key, value, nil)
}

// UpdateOptionValidated validates an option against the latest locked
// database/runtime snapshot before committing it. The validator must not call
// another option mutation function.
func UpdateOptionValidated(key string, value string, validator func(current map[string]string) error) error {
	return updateOptionsValidated(map[string]string{key: value}, validator)
}

// UpdateOptionsBulk persists multiple key/value pairs in a single database
// transaction, then dispatches them through updateOptionMap in one pass. If
// any DB write fails the whole transaction rolls back and no in-memory state
// is touched — safe for callers that must commit a set of related options
// atomically (e.g. payment gateway binding).
func UpdateOptionsBulk(values map[string]string) error {
	return UpdateOptionsBulkValidated(values, nil)
}

// UpdateOptionsBulkValidated validates and persists a related option set while
// holding the same mutation boundary used by single-option writes.
func UpdateOptionsBulkValidated(values map[string]string, validator func(current map[string]string) error) error {
	if len(values) == 0 {
		return nil
	}
	return updateOptionsValidated(values, validator)
}

func updateOptionsValidated(values map[string]string, validator func(current map[string]string) error) error {
	if _, exists := values[TaskRulesFormatMigrationOption]; exists {
		return fmt.Errorf("task format migration provenance is read-only")
	}
	if _, exists := values[TaskPricingMigrationOption]; exists {
		return fmt.Errorf("task migration provenance is read-only")
	}
	if _, exists := values[ModelPricingAmountMigrationOption]; exists {
		return fmt.Errorf("pricing amount migration provenance is read-only")
	}
	if _, exists := values[LegacyAccessTokenRetireAtKey]; exists {
		return ErrLegacyRetireAtReadOnly
	}
	if _, exists := values[ChannelTypeRenumberMigrationOption]; exists {
		return fmt.Errorf("channel type renumber provenance is read-only")
	}
	optionMutationMutex.Lock()
	defer optionMutationMutex.Unlock()

	normalized, err := normalizeOptionValues(values)
	if err != nil {
		return err
	}
	if err = validateOptionValues(normalized); err != nil {
		return err
	}
	err = DB.Transaction(func(tx *gorm.DB) error {
		if validator != nil {
			current, err := optionValuesForUpdate(tx)
			if err != nil {
				return err
			}
			if err := validator(current); err != nil {
				return err
			}
		}
		consolidated, err := consolidateModelPricingUpdate(tx, normalized)
		if err != nil {
			return err
		}
		normalized = consolidated
		if err := saveOptionValuesWithTx(tx, normalized); err != nil {
			return err
		}
		// Self-use mode changes task rule selection (every model gains a
		// followed default price, so turning it off can leave a model needing
		// an explicit task rule); check enabled bindings against the value
		// just written in this transaction.
		if _, selfUse := normalized["SelfUseModeEnabled"]; selfUse {
			if err := validateCurrentTaskPricing(tx, nil); err != nil {
				return err
			}
		}
		if _, pricing := normalized[ModelPricingOption]; pricing {
			keys := make([]string, 0, len(modelPricingFields))
			for key := range modelPricingFields {
				keys = append(keys, key)
			}
			return tx.Where("`key` IN ?", keys).Delete(&Option{}).Error
		}
		return nil
	})
	if err != nil {
		return err
	}
	if err = publishOptionValues(normalized); err != nil {
		// The database already holds the new values; republish the full committed
		// snapshot so this process does not keep a partially published state
		// until the next periodic sync.
		if reloadErr := loadOptionsFromDatabaseLocked(); reloadErr != nil {
			err = errors.Join(err, fmt.Errorf("reload committed options: %w", reloadErr))
		}
		return fmt.Errorf("%w: %w", ErrOptionSavedNotPublished, err)
	}
	return nil
}

func normalizeOptionValues(values map[string]string) (map[string]string, error) {
	normalized := make(map[string]string, len(values))
	for key, value := range values {
		if isBooleanOptionKey(key) {
			parsed, err := strconv.ParseBool(value)
			if err != nil {
				return nil, fmt.Errorf("invalid option %s: invalid boolean: %w", key, err)
			}
			value = strconv.FormatBool(parsed)
		}
		normalized[key] = value
	}
	return normalized, nil
}

func optionValuesForUpdate(tx *gorm.DB) (map[string]string, error) {
	common.OptionMapRWMutex.RLock()
	values := make(map[string]string, len(common.OptionMap))
	maps.Copy(values, common.OptionMap)
	common.OptionMapRWMutex.RUnlock()

	var persisted []Option
	if err := lockForUpdate(tx).Find(&persisted).Error; err != nil {
		return nil, err
	}
	for _, option := range persisted {
		values[option.Key] = option.Value
	}
	return values, nil
}

func validateOptionValues(values map[string]string) error {
	for key, value := range values {
		if err := validateConfigOption(key, value); err != nil {
			return fmt.Errorf("invalid option %s: %w", key, err)
		}
	}
	if err := validatePaymentOptionValues(values); err != nil {
		return err
	}
	if err := common.ValidatePaymentNotificationOptionValues(values); err != nil {
		return fmt.Errorf("invalid payment notification options: %w", err)
	}
	if err := common.ValidateTicketNotificationOptionValues(values); err != nil {
		return fmt.Errorf("invalid ticket notification options: %w", err)
	}
	if err := setting.ValidateTicketAttachmentOptionValues(values); err != nil {
		return fmt.Errorf("invalid ticket attachment options: %w", err)
	}
	if err := system_setting.ValidateServerOptionValues(values); err != nil {
		return fmt.Errorf("invalid server options: %w", err)
	}
	if err := common.ValidateBrandingSettings(values); err != nil {
		return fmt.Errorf("invalid branding options: %w", err)
	}
	if err := common.ValidateFeatureSettings(values); err != nil {
		return fmt.Errorf("invalid feature options: %w", err)
	}
	if err := setting.ValidateMidjourneySettings(values); err != nil {
		return fmt.Errorf("invalid Midjourney options: %w", err)
	}
	if err := operation_setting.ValidateSiteModeSettings(values); err != nil {
		return fmt.Errorf("invalid site mode options: %w", err)
	}
	return nil
}

func validatePaymentOptionValues(values map[string]string) error {
	if err := setting.ValidateStripeOptionValues(values); err != nil {
		return fmt.Errorf("invalid Stripe options: %w", err)
	}
	if err := setting.ValidateCreemOptionValues(values); err != nil {
		return fmt.Errorf("invalid Creem options: %w", err)
	}
	if err := setting.ValidateWaffoOptionValues(values); err != nil {
		return fmt.Errorf("invalid Waffo options: %w", err)
	}
	if err := operation_setting.ValidateEpayOptionValues(values); err != nil {
		return fmt.Errorf("invalid Epay options: %w", err)
	}
	return nil
}

func saveOptionValuesWithTx(tx *gorm.DB, values map[string]string) error {
	for _, key := range sortedOptionKeys(values) {
		value := values[key]
		option := Option{Key: key}
		if err := tx.FirstOrCreate(&option, Option{Key: key}).Error; err != nil {
			return err
		}
		option.Value = value
		if err := tx.Save(&option).Error; err != nil {
			return err
		}
	}
	return nil
}

func sortedOptionKeys(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func publishOptionValues(values map[string]string) error {
	var err error
	values, err = expandModelPricing(values)
	if err != nil {
		return err
	}
	if err := validateOptionValues(values); err != nil {
		return err
	}

	configValues := make(map[string]string)
	configNames := make(map[string]struct{})
	configKeys := make(map[string]struct{})
	for _, key := range sortedOptionKeys(values) {
		value := values[key]
		parts := strings.SplitN(key, ".", 2)
		if len(parts) == 2 && config.GlobalConfig.Has(parts[0]) {
			configValues[key] = value
			configNames[parts[0]] = struct{}{}
			configKeys[key] = struct{}{}
		}
	}
	if len(configValues) > 0 {
		if err := config.GlobalConfig.LoadFromDB(configValues); err != nil {
			return err
		}
		for name := range configNames {
			postProcessConfigUpdate(name)
		}
	}
	if err := setting.UpdateStripeConfig(values); err != nil {
		return err
	}
	if err := setting.UpdateCreemConfig(values); err != nil {
		return err
	}
	if err := setting.UpdateWaffoConfig(values); err != nil {
		return err
	}
	if err := operation_setting.UpdateEpayConfig(values); err != nil {
		return err
	}
	if hasWaffoPancakeOption(values) {
		if err := setting.UpdateWaffoPancakeConfig(values); err != nil {
			return err
		}
	}
	if hasLegacyOAuthOption(values) {
		if err := system_setting.UpdateLegacyOAuthSettings(values); err != nil {
			return err
		}
	}
	if hasSMTPOption(values) {
		if err := common.UpdateSMTPSettings(values); err != nil {
			return err
		}
	}
	if hasWorkerOption(values) {
		if err := system_setting.UpdateWorkerSettings(values); err != nil {
			return err
		}
	}
	if hasPaymentNotificationOption(values) {
		if err := common.UpdatePaymentNotificationSettings(values); err != nil {
			return err
		}
	}
	if hasTicketNotificationOption(values) {
		if err := common.UpdateTicketNotificationSettings(values); err != nil {
			return err
		}
	}
	if hasTicketAttachmentOption(values) {
		if err := setting.UpdateTicketAttachmentSettings(values); err != nil {
			return err
		}
	}
	if hasServerOption(values) {
		if err := system_setting.UpdateServerSettings(values); err != nil {
			return err
		}
	}
	if hasModelRequestRateLimitOption(values) {
		if err := setting.UpdateModelRequestRateLimitSettings(values); err != nil {
			return err
		}
	}
	if hasAuthOption(values) {
		if err := common.UpdateAuthSettings(values); err != nil {
			return err
		}
	}
	if hasUserQuotaOption(values) {
		if err := common.UpdateUserQuotaSettings(values); err != nil {
			return err
		}
	}
	if hasOptionKey(values, common.IsBrandingOptionKey) {
		if err := common.UpdateBrandingSettings(values); err != nil {
			return err
		}
	}
	if hasOptionKey(values, common.IsFeatureOptionKey) {
		if err := syncLegacyCurrencyDisplayOption(values); err != nil {
			return err
		}
		if err := common.UpdateFeatureSettings(values); err != nil {
			return err
		}
	}
	if hasOptionKey(values, setting.IsMidjourneyOptionKey) {
		if err := setting.UpdateMidjourneySettings(values); err != nil {
			return err
		}
	}
	if hasOptionKey(values, operation_setting.IsSiteModeOptionKey) {
		if err := operation_setting.UpdateSiteModeSettings(values); err != nil {
			return err
		}
	}
	if hasSensitiveSettingsOption(values) {
		if err := setting.UpdateSensitiveSettings(values); err != nil {
			return err
		}
	}
	if hasOptionKey(values, setting.IsAutoGroupOptionKey) {
		if err := setting.UpdateAutoGroupPolicy(values); err != nil {
			return err
		}
	}
	if hasOptionKey(values, operation_setting.IsChannelFailurePolicyOptionKey) {
		if err := operation_setting.UpdateChannelFailurePolicy(values); err != nil {
			return err
		}
	}

	common.OptionMapRWMutex.Lock()
	for _, key := range sortedOptionKeys(values) {
		value := values[key]
		common.OptionMap[key] = value
	}
	common.OptionMapRWMutex.Unlock()

	for _, key := range sortedOptionKeys(values) {
		value := values[key]
		if _, handled := configKeys[key]; handled || isPaymentOptionKey(key) || isWaffoPancakeOptionKey(key) || isLegacyOAuthOptionKey(key) || isSMTPOptionKey(key) || isWorkerOptionKey(key) || isPaymentNotificationOptionKey(key) || isTicketNotificationOptionKey(key) || isTicketAttachmentOptionKey(key) || isServerOptionKey(key) || isModelRequestRateLimitOptionKey(key) || isAuthOptionKey(key) || isUserQuotaOptionKey(key) || common.IsBrandingOptionKey(key) || common.IsFeatureOptionKey(key) || setting.IsMidjourneyOptionKey(key) || operation_setting.IsSiteModeOptionKey(key) || isSensitiveSettingsOptionKey(key) || setting.IsAutoGroupOptionKey(key) || operation_setting.IsChannelFailurePolicyOptionKey(key) {
			continue
		}
		if err := updateOptionMap(key, value); err != nil {
			return err
		}
	}
	return nil
}

func syncLegacyCurrencyDisplayOption(values map[string]string) error {
	value, ok := values["DisplayInCurrencyEnabled"]
	if !ok {
		return nil
	}
	enabled, err := strconv.ParseBool(value)
	if err != nil {
		return err
	}
	displayType := operation_setting.QuotaDisplayTypeTokens
	if enabled {
		displayType = operation_setting.QuotaDisplayTypeUSD
	}
	_, err = config.GlobalConfig.Update("general_setting", map[string]string{"quota_display_type": displayType})
	return err
}

func isPaymentOptionKey(key string) bool {
	switch key {
	case "PayAddress", "CustomCallbackAddress", "EpayId", "EpayKey", "Price", "MinTopUp", "USDExchangeRate", "PayMethods",
		"StripeApiSecret", "StripeWebhookSecret", "StripePriceId", "StripeUnitPrice", "StripeMinTopUp", "StripePromotionCodesEnabled",
		"CreemApiKey", "CreemProducts", "CreemTestMode", "CreemWebhookSecret",
		"WaffoEnabled", "WaffoApiKey", "WaffoPrivateKey", "WaffoPublicCert", "WaffoSandboxPublicCert", "WaffoSandboxApiKey",
		"WaffoSandboxPrivateKey", "WaffoSandbox", "WaffoMerchantId", "WaffoNotifyUrl", "WaffoReturnUrl", "WaffoCurrency",
		"WaffoUnitPrice", "WaffoMinTopUp", "WaffoPayMethods":
		return true
	default:
		return false
	}
}

func hasWaffoPancakeOption(values map[string]string) bool {
	for key := range values {
		if isWaffoPancakeOptionKey(key) {
			return true
		}
	}
	return false
}

func isWaffoPancakeOptionKey(key string) bool {
	switch key {
	case "WaffoPancakeMerchantID", "WaffoPancakePrivateKey", "WaffoPancakeReturnURL",
		"WaffoPancakeUnitPrice", "WaffoPancakeMinTopUp", "WaffoPancakeStoreID", "WaffoPancakeProductID":
		return true
	default:
		return false
	}
}

func hasLegacyOAuthOption(values map[string]string) bool {
	for key := range values {
		if isLegacyOAuthOptionKey(key) {
			return true
		}
	}
	return false
}

func isLegacyOAuthOptionKey(key string) bool {
	switch key {
	case "GitHubOAuthEnabled", "GitHubClientId", "GitHubClientSecret",
		"LinuxDOOAuthEnabled", "LinuxDOClientId", "LinuxDOClientSecret", "LinuxDOMinimumTrustLevel",
		"TelegramOAuthEnabled", "TelegramBotToken", "TelegramBotName", "TelegramClientId", "TelegramClientSecret",
		"WeChatAuthEnabled", "WeChatServerAddress", "WeChatServerToken", "WeChatAccountQRCodeImageURL":
		return true
	default:
		return false
	}
}

func hasSMTPOption(values map[string]string) bool {
	for key := range values {
		if isSMTPOptionKey(key) {
			return true
		}
	}
	return false
}

func isSMTPOptionKey(key string) bool {
	switch key {
	case "SMTPServer", "SMTPPort", "SMTPSSLEnabled", "SMTPStartTLSEnabled",
		"SMTPInsecureSkipVerify", "SMTPForceAuthLogin", "SMTPAccount", "SMTPFrom",
		"SMTPToken", "SMTPArchiveBcc":
		return true
	default:
		return false
	}
}

func hasWorkerOption(values map[string]string) bool {
	for key := range values {
		if isWorkerOptionKey(key) {
			return true
		}
	}
	return false
}

func isWorkerOptionKey(key string) bool {
	switch key {
	case "WorkerUrl", "WorkerValidKey", "WorkerAllowHttpImageRequestEnabled":
		return true
	default:
		return false
	}
}

func hasPaymentNotificationOption(values map[string]string) bool {
	return hasOptionKey(values, isPaymentNotificationOptionKey)
}

func isPaymentNotificationOptionKey(key string) bool {
	switch key {
	case "PaymentNotifyUserEnabled", "PaymentNotifyAdminEnabled", "PaymentAdminEmail":
		return true
	default:
		return false
	}
}

func hasTicketNotificationOption(values map[string]string) bool {
	return hasOptionKey(values, isTicketNotificationOptionKey)
}

func isTicketNotificationOptionKey(key string) bool {
	switch key {
	case "TicketNotifyEnabled", "TicketAdminEmail", "TicketReplyAttachmentEnabled",
		"TicketReplyAttachmentMaxBytes", "TicketInvoiceMinTopupMoney":
		return true
	default:
		return false
	}
}

func hasTicketAttachmentOption(values map[string]string) bool {
	return hasOptionKey(values, isTicketAttachmentOptionKey)
}

func isTicketAttachmentOptionKey(key string) bool {
	return strings.HasPrefix(key, "TicketAttachment")
}

func hasServerOption(values map[string]string) bool {
	return hasOptionKey(values, isServerOptionKey)
}

func isServerOptionKey(key string) bool {
	return key == "ServerAddress"
}

func hasModelRequestRateLimitOption(values map[string]string) bool {
	return hasOptionKey(values, isModelRequestRateLimitOptionKey)
}

func isModelRequestRateLimitOptionKey(key string) bool {
	switch key {
	case "ModelRequestRateLimitEnabled", "ModelRequestRateLimitDurationMinutes",
		"ModelRequestRateLimitCount", "ModelRequestRateLimitSuccessCount", "ModelRequestRateLimitGroup":
		return true
	default:
		return false
	}
}

func hasAuthOption(values map[string]string) bool {
	return hasOptionKey(values, isAuthOptionKey)
}

func isAuthOptionKey(key string) bool {
	switch key {
	case "PasswordLoginEnabled", "PasswordRegisterEnabled", "EmailVerificationEnabled", "RegisterEnabled",
		"TurnstileCheckEnabled", "TurnstileSiteKey", "TurnstileSecretKey",
		"InvitationCodeEnabled", "InvitationCodeOAuthRequired", "InvitationCodeUserGenerateEnabled",
		"EmailDomainRestrictionEnabled", "EmailAliasRestrictionEnabled", "EmailDomainWhitelist":
		return true
	default:
		return false
	}
}

func hasUserQuotaOption(values map[string]string) bool {
	return hasOptionKey(values, isUserQuotaOptionKey)
}

func isUserQuotaOptionKey(key string) bool {
	switch key {
	case "QuotaForNewUser", "QuotaForInviter", "QuotaForInvitee", "QuotaRemindThreshold", "PreConsumedQuota", "AffiliateEnabled":
		return true
	default:
		return false
	}
}

func hasSensitiveSettingsOption(values map[string]string) bool {
	return hasOptionKey(values, isSensitiveSettingsOptionKey)
}

func isSensitiveSettingsOptionKey(key string) bool {
	switch key {
	case "CheckSensitiveEnabled", "CheckSensitiveOnPromptEnabled", "StopOnSensitiveEnabled", "SensitiveWords", "StreamCacheQueueLength":
		return true
	default:
		return false
	}
}

func hasOptionKey(values map[string]string, matches func(string) bool) bool {
	for key := range values {
		if matches(key) {
			return true
		}
	}
	return false
}

func updateOptionMap(key string, value string) (err error) {
	if isPaymentOptionKey(key) || isLegacyOAuthOptionKey(key) || isSMTPOptionKey(key) || isWorkerOptionKey(key) ||
		isPaymentNotificationOptionKey(key) || isTicketNotificationOptionKey(key) || isTicketAttachmentOptionKey(key) || isServerOptionKey(key) || isModelRequestRateLimitOptionKey(key) ||
		isAuthOptionKey(key) || isUserQuotaOptionKey(key) || common.IsBrandingOptionKey(key) || common.IsFeatureOptionKey(key) || setting.IsMidjourneyOptionKey(key) || operation_setting.IsSiteModeOptionKey(key) ||
		isSensitiveSettingsOptionKey(key) || setting.IsAutoGroupOptionKey(key) || operation_setting.IsChannelFailurePolicyOptionKey(key) {
		return nil
	}

	handled, err := handleConfigUpdate(key, value)
	if err != nil {
		return err
	}
	common.OptionMapRWMutex.Lock()
	defer func() {
		common.OptionMapRWMutex.Unlock()
		// Pricing refreshes read OptionMap while holding updatePricingLock.
		// Invalidate only after releasing the option lock to avoid lock inversion.
		if err == nil && !handled && isPricingOptionKey(key) {
			InvalidatePricingCache()
			ratio_setting.InvalidateExposedDataCache()
		}
	}()
	common.OptionMap[key] = value

	if handled {
		return nil // 已由配置系统处理
	}

	// 处理传统配置项...
	if strings.HasSuffix(key, "Enabled") || key == "DefaultCollapseSidebar" || key == "DefaultUseAutoGroup" || key == "SMTPForceAuthLogin" || key == "SMTPInsecureSkipVerify" || key == "InvitationCodeOAuthRequired" {
		boolValue := value == "true"
		switch key {
		case "ExposeRatioEnabled":
			ratio_setting.SetExposeRatioEnabled(boolValue)
		}
	}
	switch key {
	case "TicketAssignConfig":
		err = setting.UpdateTicketAssignConfigFromJSON(value)
	case "Chats":
		err = setting.UpdateChatsByJsonString(value)
	case "TopupGroupRatio":
		err = common.UpdateTopupGroupRatioByJSONString(value)
	case "ModelRPMConfig":
		err = setting.UpdateModelRPMConfigByJSONString(value)
	case "ModelRatio":
		err = ratio_setting.UpdateModelRatioByJSONString(value)
	case "BatchInputRatio":
		err = ratio_setting.UpdateBatchInputRatioByJSONString(value)
	case "GroupRatio":
		err = ratio_setting.UpdateGroupRatioByJSONString(value)
	case "GroupGroupRatio":
		err = ratio_setting.UpdateGroupGroupRatioByJSONString(value)
	case "UserUsableGroups":
		err = setting.UpdateUserUsableGroupsByJSONString(value)
	case "CompletionRatio":
		err = ratio_setting.UpdateCompletionRatioByJSONString(value)
	case "ModelPrice":
		err = ratio_setting.UpdateModelPriceByJSONString(value)
	case "CacheRatio":
		err = ratio_setting.UpdateCacheRatioByJSONString(value)
	case "CreateCacheRatio":
		err = ratio_setting.UpdateCreateCacheRatioByJSONString(value)
	case "ImageRatio":
		err = ratio_setting.UpdateImageRatioByJSONString(value)
	case "AudioRatio":
		err = ratio_setting.UpdateAudioRatioByJSONString(value)
	case "AudioCompletionRatio":
		err = ratio_setting.UpdateAudioCompletionRatioByJSONString(value)
	case "InvitationCodePolicy":
		err = setting.UpdateInvitationCodePolicy(value)
	//case "ChatLink":
	//	common.ChatLink = value
	//case "ChatLink2":
	//	common.ChatLink2 = value
	case "QuotaPerUnit":
		var quotaPerUnit float64
		quotaPerUnit, err = strconv.ParseFloat(value, 64)
		if err == nil {
			err = common.SetQuotaPerUnit(quotaPerUnit)
		}
	}
	return err
}

func validateConfigOption(key, value string) error {
	if key == ModelPricingOption {
		_, err := unpackModelPricing(value)
		return err
	}
	if err := constant.ValidateEmailTemplateOption(key, value); err != nil {
		return err
	}
	if isBooleanOptionKey(key) {
		if _, err := strconv.ParseBool(value); err != nil {
			return fmt.Errorf("invalid boolean for %s: %w", key, err)
		}
	}
	switch key {
	case "ModelRatio", "CompletionRatio", "ModelPrice", "CacheRatio", "CreateCacheRatio", "ImageRatio", "BatchInputRatio", "AudioRatio", "AudioCompletionRatio":
		var parsed map[string]float64
		if err := common.Unmarshal([]byte(value), &parsed); err != nil {
			return fmt.Errorf("invalid pricing JSON: %w", err)
		}
		for modelName, ratio := range parsed {
			if ratio < 0 {
				return fmt.Errorf("pricing value for %s cannot be negative", modelName)
			}
		}
	case "GroupRatio":
		if err := ratio_setting.CheckGroupRatio(value); err != nil {
			return err
		}
	case "GroupGroupRatio":
		var parsed map[string]map[string]float64
		if err := common.Unmarshal([]byte(value), &parsed); err != nil {
			return err
		}
		for tier, ratios := range parsed {
			for group, ratio := range ratios {
				if ratio < 0 {
					return fmt.Errorf("group ratio for %s/%s cannot be negative", tier, group)
				}
			}
		}
	case "UserUsableGroups":
		var parsed map[string]string
		if err := common.Unmarshal([]byte(value), &parsed); err != nil {
			return err
		}
	case "AutoGroups":
		var parsed []string
		if err := common.Unmarshal([]byte(value), &parsed); err != nil {
			return err
		}
	case "TopupGroupRatio":
		var parsed map[string]float64
		if err := common.Unmarshal([]byte(value), &parsed); err != nil {
			return err
		}
		for group, ratio := range parsed {
			if ratio < 0 {
				return fmt.Errorf("top-up ratio for %s cannot be negative", group)
			}
		}
	case "PayMethods":
		var parsed []map[string]string
		if err := common.Unmarshal([]byte(value), &parsed); err != nil {
			return fmt.Errorf("invalid payment methods: %w", err)
		}
	case "Chats":
		if err := setting.ValidateChatsJsonString(value); err != nil {
			return err
		}
	case "TicketAssignConfig":
		if err := setting.ValidateTicketAssignConfigJSON(value); err != nil {
			return err
		}
	case "CreemProducts", "WaffoPayMethods":
		var parsed []any
		if err := common.Unmarshal([]byte(value), &parsed); err != nil {
			return fmt.Errorf("invalid JSON array: %w", err)
		}
	case "payment_setting.amount_options":
		var parsed []int
		if err := common.Unmarshal([]byte(value), &parsed); err != nil {
			return err
		}
		for _, amount := range parsed {
			if amount <= 0 {
				return fmt.Errorf("top-up amount must be positive: %d", amount)
			}
		}
	case "payment_setting.amount_discount":
		var parsed map[int]float64
		if err := common.Unmarshal([]byte(value), &parsed); err != nil {
			return err
		}
		for amount, discount := range parsed {
			if amount <= 0 || discount < 0 {
				return fmt.Errorf("invalid top-up discount for amount %d", amount)
			}
		}
	case "QuotaPerUnit":
		if err := validatePositiveFloatOption(key, value); err != nil {
			return err
		}
	case "Price", "StripeUnitPrice", "WaffoUnitPrice", "WaffoPancakeUnitPrice", "ChannelDisableThreshold", "USDExchangeRate", "TicketInvoiceMinTopupMoney":
		if err := validateNonNegativeFloatOption(key, value); err != nil {
			return err
		}
	case "MinTopUp", "StripeMinTopUp", "WaffoMinTopUp", "WaffoPancakeMinTopUp", "QuotaRemindThreshold", "QuotaForNewUser", "PreConsumedQuota", "QuotaForInviter", "QuotaForInvitee", "ModelRequestRateLimitDurationMinutes", "ModelRequestRateLimitCount", "StreamCacheQueueLength":
		if err := validateIntegerRange(key, value, 0, 0); err != nil {
			return err
		}
	case "ModelRequestRateLimitSuccessCount":
		if err := validateIntegerRange(key, value, 1, 100_000_000); err != nil {
			return err
		}
	case "RetryTimes":
		if err := validateIntegerRange(key, value, 0, 10); err != nil {
			return err
		}
	case "SMTPPort":
		if err := validateIntegerRange(key, value, 1, 65_535); err != nil {
			return err
		}
	case "TicketAttachmentMaxSize", "TicketAttachmentMaxCount", "TicketAttachmentSignedURLTTL", "TicketReplyAttachmentMaxBytes":
		if err := validateIntegerRange(key, value, 1, 0); err != nil {
			return err
		}
	case "LinuxDOMinimumTrustLevel":
		if err := validateIntegerRange(key, value, 0, 4); err != nil {
			return err
		}
	case "AutomaticDisableStatusCodes", "AutomaticRetryStatusCodes":
		if _, err := operation_setting.ParseHTTPStatusCodeRanges(value); err != nil {
			return err
		}
	case "monitor_setting.auto_test_channel_minutes":
		minutes, err := strconv.ParseFloat(value, 64)
		if err != nil || math.IsNaN(minutes) || math.IsInf(minutes, 0) || minutes <= 0 {
			return fmt.Errorf("channel test interval must be positive")
		}
	case "monitor_setting.channel_test_mode":
		switch value {
		case operation_setting.ChannelTestModeScheduledAll, operation_setting.ChannelTestModeAutoBanOnly, operation_setting.ChannelTestModePassiveRecovery:
		default:
			return fmt.Errorf("invalid channel test mode %q", value)
		}
	case "performance_setting.disk_cache_threshold_mb":
		if err := validateIntegerRange(key, value, 1, 0); err != nil {
			return err
		}
	case "performance_setting.disk_cache_max_size_mb":
		if err := validateIntegerRange(key, value, 100, 0); err != nil {
			return err
		}
	case "performance_setting.monitor_cpu_threshold", "performance_setting.monitor_memory_threshold", "performance_setting.monitor_disk_threshold":
		if err := validateIntegerRange(key, value, 0, 100); err != nil {
			return err
		}
	case "fetch_setting.allowed_ports":
		var ports []string
		if err := common.Unmarshal([]byte(value), &ports); err != nil {
			return err
		}
		if _, err := common.NewSSRFProtectionFromFetchSetting(false, false, false, nil, nil, ports, true); err != nil {
			return err
		}
	case "InvitationCodePolicy":
		if err := setting.ValidateInvitationCodePolicy(value); err != nil {
			return fmt.Errorf("invalid invitation code policy: %w", err)
		}
	case "ModelRequestRateLimitGroup":
		if err := setting.CheckModelRequestRateLimitGroup(value); err != nil {
			return err
		}
	case "ModelRPMConfig":
		if err := setting.CheckModelRPMConfig(value); err != nil {
			return err
		}
	case "console_setting.api_info":
		return console_setting.ValidateConsoleSettings(value, "ApiInfo")
	case "console_setting.announcements":
		return console_setting.ValidateConsoleSettings(value, "Announcements")
	case "console_setting.faq":
		return console_setting.ValidateConsoleSettings(value, "FAQ")
	case "console_setting.uptime_kuma_groups":
		return console_setting.ValidateConsoleSettings(value, "UptimeKumaGroups")
	case "passkey.rp_id":
		if err := validatePasskeyRPIDChange(value); err != nil {
			return err
		}
	case "passkey.user_verification":
		switch value {
		case "required", "preferred", "discouraged":
		default:
			return fmt.Errorf("invalid passkey user verification %q", value)
		}
	case "passkey.attachment_preference":
		switch value {
		case "", "platform", "cross-platform":
		default:
			return fmt.Errorf("invalid passkey attachment preference %q", value)
		}
	case "DataExportDefaultTime":
		switch value {
		case "hour", "day", "week":
		default:
			return fmt.Errorf("invalid data export default time %q", value)
		}
	case PricingGroupEndpointOverridesOption:
		if err := ValidatePricingGroupEndpointOverrides(value); err != nil {
			return err
		}
	case ModelContextWindowsOption:
		if err := ValidateModelContextWindows(value); err != nil {
			return err
		}
	case "billing_setting.billing_mode":
		var modes map[string]string
		if err := common.Unmarshal([]byte(value), &modes); err != nil {
			return err
		}
		for modelName, mode := range modes {
			if !billing_setting.IsKnownBillingMode(mode) {
				return fmt.Errorf("unknown billing mode %q for %s", mode, modelName)
			}
		}
	case billing_setting.TaskPriorityOption:
		if value != "0" && value != "1" {
			return fmt.Errorf("invalid task priority")
		}
		return nil
	case billing_setting.TaskExpressionsOption:
		return billing_setting.ValidateTaskExpressions(value)
	case "billing_setting.billing_expr":
		var expressions map[string]string
		if err := common.Unmarshal([]byte(value), &expressions); err != nil {
			return err
		}
		for modelName, expression := range expressions {
			// An empty expression is only valid for non-expression modes. That
			// depends on billing_mode, possibly from the same batch, so it is
			// enforced on the merged values by validateExpressionModesHaveExpressions.
			if strings.TrimSpace(expression) == "" {
				continue
			}
			if err := billing_setting.SmokeTestExpr(expression); err != nil {
				return fmt.Errorf("invalid billing expression for %s: %w", modelName, err)
			}
		}
	case operation_setting.ChannelTestConcurrencyOptionKey:
		if err := operation_setting.ValidateChannelTestConcurrency(value); err != nil {
			return err
		}
	}
	parts := strings.SplitN(key, ".", 2)
	if len(parts) != 2 {
		return nil
	}
	_, err := config.GlobalConfig.Validate(parts[0], map[string]string{parts[1]: value})
	return err
}

func isBooleanOptionKey(key string) bool {
	if strings.HasSuffix(key, "Enabled") {
		return true
	}
	switch key {
	case "DefaultCollapseSidebar", "DefaultUseAutoGroup", "SMTPForceAuthLogin", "SMTPInsecureSkipVerify", "InvitationCodeOAuthRequired", "CreemTestMode", "WaffoSandbox", "WaffoPancakeSandbox":
		return true
	}
	parts := strings.SplitN(key, ".", 2)
	return len(parts) == 2 && config.GlobalConfig.IsBooleanField(parts[0], parts[1])
}

func validateNonNegativeFloatOption(key, value string) error {
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil || math.IsNaN(parsed) || math.IsInf(parsed, 0) || parsed < 0 {
		return fmt.Errorf("%s must be a non-negative number", key)
	}
	return nil
}

func validatePositiveFloatOption(key, value string) error {
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil || math.IsNaN(parsed) || math.IsInf(parsed, 0) || parsed <= 0 {
		return fmt.Errorf("%s must be a positive finite number", key)
	}
	return nil
}

func validateIntegerRange(key, value string, minimum, maximum int) error {
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < minimum || (maximum > 0 && parsed > maximum) {
		if maximum > 0 {
			return fmt.Errorf("%s must be between %d and %d", key, minimum, maximum)
		}
		return fmt.Errorf("%s must be at least %d", key, minimum)
	}
	return nil
}

func isPricingOptionKey(key string) bool {
	switch key {
	case "ModelRatio", "CompletionRatio", "ModelPrice", "CacheRatio", "CreateCacheRatio", "ImageRatio", "BatchInputRatio", "AudioRatio", "AudioCompletionRatio", "GroupRatio", "GroupGroupRatio", PricingGroupEndpointOverridesOption, ModelContextWindowsOption:
		return true
	default:
		return strings.HasPrefix(key, "billing_setting.")
	}
}

// handleConfigUpdate 处理分层配置更新，返回是否已处理
func handleConfigUpdate(key, value string) (bool, error) {
	parts := strings.SplitN(key, ".", 2)
	if len(parts) != 2 {
		return false, nil // 不是分层配置
	}

	configName := parts[0]
	configKey := parts[1]

	// 获取配置对象
	configMap := map[string]string{
		configKey: value,
	}
	handled, err := config.GlobalConfig.Update(configName, configMap)
	if err != nil || !handled {
		return handled, err
	}

	postProcessConfigUpdate(configName)

	return true, nil // 已处理
}

func postProcessConfigUpdate(configName string) {
	switch configName {
	case "performance_setting":
		performance_setting.UpdateAndSync()
	case "tool_price_setting":
		operation_setting.RebuildToolPriceIndex()
	case "billing_setting":
		InvalidatePricingCache()
		ratio_setting.InvalidateExposedDataCache()
	}
}

// validatePasskeyRPIDChange refuses to drop a passkey domain that credentials
// are still registered under: those Passkeys could never be used again.
func validatePasskeyRPIDChange(value string) error {
	if DB == nil || !DB.Migrator().HasColumn(&PasskeyCredential{}, "rp_id") {
		return nil
	}
	next := map[string]bool{}
	for _, part := range strings.Split(value, ",") {
		if part = strings.ToLower(strings.TrimSpace(part)); part != "" {
			next[part] = true
		}
	}
	if len(next) == 0 {
		return nil
	}
	var used []string
	if err := DB.Model(&PasskeyCredential{}).Where("rp_id IS NOT NULL AND rp_id <> ''").Distinct("rp_id").Pluck("rp_id", &used).Error; err != nil {
		return err
	}
	var removed []string
	for _, rpID := range used {
		if !next[strings.ToLower(rpID)] {
			removed = append(removed, rpID)
		}
	}
	if len(removed) == 0 {
		return nil
	}
	count, err := CountPasskeysByRPID(removed)
	if err != nil {
		return err
	}
	return fmt.Errorf("仍有 %d 个 Passkey 绑定在域名 %s，移除后这些 Passkey 将无法使用；请保留该域名，或先让相关用户重新注册", count, strings.Join(removed, ", "))
}
