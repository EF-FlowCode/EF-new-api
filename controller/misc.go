package controller

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"eflow.local/new-api/common"
	"eflow.local/new-api/constant"
	"eflow.local/new-api/i18n"
	"eflow.local/new-api/logger"
	"eflow.local/new-api/middleware"
	"eflow.local/new-api/model"
	"eflow.local/new-api/oauth"
	"eflow.local/new-api/service"
	"eflow.local/new-api/setting"
	"eflow.local/new-api/setting/console_setting"
	"eflow.local/new-api/setting/operation_setting"
	"eflow.local/new-api/setting/system_setting"

	"github.com/gin-gonic/gin"
)

func TestStatus(c *gin.Context) {
	err := model.PingDB()
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"success": false,
			"message": "数据库连接失败",
		})
		return
	}
	// 获取HTTP统计信息
	httpStats := middleware.GetStats()
	c.JSON(http.StatusOK, gin.H{
		"success":    true,
		"message":    "Server is running",
		"http_stats": httpStats,
	})
	return
}

func GetStatus(c *gin.Context) {

	authSettings := common.GetAuthSettings()
	userQuotaSettings := common.GetUserQuotaSettings()
	brandingSettings := common.GetBrandingSettings()
	featureSettings := common.GetFeatureSettings()
	autoGroupPolicy := setting.GetAutoGroupPolicy()
	midjourneySettings := setting.GetMidjourneySettings()
	cs := console_setting.GetConsoleSetting()
	groupMonitoringSetting := operation_setting.GetGroupMonitoringSetting()
	generalSetting := operation_setting.GetGeneralSetting()
	siteModeSettings := operation_setting.GetSiteModeSettings()
	epayConfig := operation_setting.GetEpayConfig()
	stripeConfig := setting.GetStripeConfig()
	legacyOAuthSettings := system_setting.GetLegacyOAuthSettings()
	discordSettings := system_setting.GetDiscordSettings()
	oidcSettings := system_setting.GetOIDCSettings()
	serverSettings := system_setting.GetServerSettings()
	attachmentSettings := setting.GetTicketAttachmentSettings()
	common.OptionMapRWMutex.RLock()
	defer common.OptionMapRWMutex.RUnlock()

	passkeySetting := system_setting.GetPasskeySettings()
	legalSetting := system_setting.GetLegalSettings()

	data := gin.H{
		"version":                     common.Version,
		"start_time":                  common.StartTime,
		"redis_enabled":               common.RedisEnabled,
		"email_verification":          authSettings.EmailVerificationEnabled,
		"github_oauth":                legacyOAuthSettings.GitHubOAuthEnabled,
		"github_client_id":            legacyOAuthSettings.GitHubClientId,
		"discord_oauth":               discordSettings.Enabled,
		"discord_client_id":           discordSettings.ClientId,
		"linuxdo_oauth":               legacyOAuthSettings.LinuxDOOAuthEnabled,
		"linuxdo_client_id":           legacyOAuthSettings.LinuxDOClientId,
		"linuxdo_minimum_trust_level": legacyOAuthSettings.LinuxDOMinimumTrustLevel,
		"telegram_oauth":              legacyOAuthSettings.TelegramOAuthEnabled,
		"telegram_bot_name":           legacyOAuthSettings.TelegramBotName,
		"telegram_oidc":               legacyOAuthSettings.TelegramOIDCEnabled(),
		"system_name":                 brandingSettings.SystemName,
		"logo":                        brandingSettings.Logo,
		"footer_html":                 brandingSettings.Footer,
		"wechat_qrcode":               legacyOAuthSettings.WeChatAccountQRCodeImageURL,
		"wechat_login":                legacyOAuthSettings.WeChatAuthEnabled,
		"server_address":              serverSettings.Address,
		"turnstile_check":             authSettings.TurnstileCheckEnabled,
		"turnstile_site_key":          authSettings.TurnstileSiteKey,
		"group_monitoring_url":        brandingSettings.GroupMonitoringURL,
		"group_monitoring_available":  len(groupMonitoringSetting.MonitoringGroups) > 0,
		"affiliate_enabled":           userQuotaSettings.AffiliateEnabled,
		"docs_link":                   generalSetting.DocsLink,
		"quota_per_unit":              common.GetQuotaPerUnit(),
		// 兼容旧前端：保留 display_in_currency，同时提供新的 quota_display_type
		"display_in_currency":           generalSetting.QuotaDisplayType != operation_setting.QuotaDisplayTypeTokens,
		"quota_display_type":            generalSetting.QuotaDisplayType,
		"custom_currency_symbol":        generalSetting.CustomCurrencySymbol,
		"custom_currency_exchange_rate": generalSetting.CustomCurrencyExchangeRate,
		"enable_batch_update":           common.BatchUpdateEnabled,
		"enable_drawing":                featureSettings.DrawingEnabled,
		"enable_task":                   featureSettings.TaskEnabled,
		"enable_data_export":            featureSettings.DataExportEnabled,
		"data_export_default_time":      featureSettings.DataExportDefaultTime,
		"default_collapse_sidebar":      featureSettings.DefaultCollapseSidebar,
		"mj_notify_enabled":             midjourneySettings.NotifyEnabled,
		"chats":                         setting.GetChats(),
		"demo_site_enabled":             siteModeSettings.DemoSiteEnabled,
		"self_use_mode_enabled":         siteModeSettings.SelfUseModeEnabled,
		"register_enabled":              authSettings.RegistrationEnabled,
		"password_login_enabled":        authSettings.PasswordLoginEnabled,
		"password_register_enabled":     authSettings.PasswordRegisterEnabled,
		"default_use_auto_group":        autoGroupPolicy.DefaultUseAutoGroup,

		"usd_exchange_rate": epayConfig.USDExchangeRate,
		"price":             epayConfig.Price,
		"stripe_unit_price": stripeConfig.UnitPrice,

		// 面板启用开关
		"api_info_enabled":      cs.ApiInfoEnabled,
		"uptime_kuma_enabled":   cs.UptimeKumaEnabled,
		"announcements_enabled": cs.AnnouncementsEnabled,
		"faq_enabled":           cs.FAQEnabled,

		// 模块管理配置
		"HeaderNavModules":    common.OptionMap["HeaderNavModules"],
		"SidebarModulesAdmin": common.OptionMap["SidebarModulesAdmin"],

		"oidc_enabled":                          oidcSettings.Enabled,
		"oidc_client_id":                        oidcSettings.ClientId,
		"oidc_authorization_endpoint":           oidcSettings.AuthorizationEndpoint,
		"oidc_display_name":                     oidcSettings.GetEffectiveDisplayName(),
		"passkey_login":                         passkeySetting.Enabled,
		"passkey_display_name":                  passkeySetting.RPDisplayName,
		"passkey_rp_id":                         passkeySetting.RPID,
		"passkey_allow_insecure":                passkeySetting.AllowInsecureOrigin,
		"passkey_user_verification":             passkeySetting.UserVerification,
		"passkey_attachment":                    passkeySetting.AttachmentPreference,
		"setup":                                 constant.IsSetupComplete(),
		"user_agreement_enabled":                legalSetting.UserAgreement != "",
		"privacy_policy_enabled":                legalSetting.PrivacyPolicy != "",
		"checkin_enabled":                       operation_setting.GetCheckinSetting().Enabled,
		"invitation_code_enabled":               authSettings.InvitationCodeEnabled,
		"invitation_code_oauth_required":        authSettings.InvitationCodeOAuthRequired,
		"invitation_code_user_generate_enabled": authSettings.InvitationCodeUserGenerateEnabled,

		// 工单附件配置下发给前端，避免选文件后才发现不支持。
		"ticket_attachment_enabled":      attachmentSettings.Enabled,
		"ticket_attachment_max_size":     attachmentSettings.MaxSize,
		"ticket_attachment_max_count":    attachmentSettings.MaxCount,
		"ticket_attachment_allowed_exts": attachmentSettings.AllowedExts,
	}

	// 根据启用状态注入可选内容
	if cs.ApiInfoEnabled {
		data["api_info"] = console_setting.GetApiInfo()
	}
	if cs.AnnouncementsEnabled {
		data["announcements"] = console_setting.GetAnnouncements()
	}
	if cs.FAQEnabled {
		data["faq"] = console_setting.GetFAQ()
	}

	// Add enabled custom OAuth providers
	customProviders := oauth.GetEnabledCustomProviders()
	if len(customProviders) > 0 {
		type CustomOAuthInfo struct {
			Id                    int    `json:"id"`
			Name                  string `json:"name"`
			Slug                  string `json:"slug"`
			Icon                  string `json:"icon"`
			ClientId              string `json:"client_id"`
			AuthorizationEndpoint string `json:"authorization_endpoint"`
			Scopes                string `json:"scopes"`
		}
		providersInfo := make([]CustomOAuthInfo, 0, len(customProviders))
		for _, p := range customProviders {
			config := p.GetConfig()
			providersInfo = append(providersInfo, CustomOAuthInfo{
				Id:                    config.Id,
				Name:                  config.Name,
				Slug:                  config.Slug,
				Icon:                  config.Icon,
				ClientId:              config.ClientId,
				AuthorizationEndpoint: config.AuthorizationEndpoint,
				Scopes:                config.Scopes,
			})
		}
		data["custom_oauth_providers"] = providersInfo
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    data,
	})
	return
}

func GetNotice(c *gin.Context) {
	common.OptionMapRWMutex.RLock()
	defer common.OptionMapRWMutex.RUnlock()
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    common.OptionMap["Notice"],
	})
	return
}

func GetAbout(c *gin.Context) {
	common.OptionMapRWMutex.RLock()
	defer common.OptionMapRWMutex.RUnlock()
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    common.OptionMap["About"],
	})
	return
}

func GetUserAgreement(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    system_setting.GetLegalSettings().UserAgreement,
	})
	return
}

func GetPrivacyPolicy(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    system_setting.GetLegalSettings().PrivacyPolicy,
	})
	return
}

func GetMidjourney(c *gin.Context) {
	common.OptionMapRWMutex.RLock()
	defer common.OptionMapRWMutex.RUnlock()
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    common.OptionMap["Midjourney"],
	})
	return
}

func GetHomePageContent(c *gin.Context) {
	common.OptionMapRWMutex.RLock()
	defer common.OptionMapRWMutex.RUnlock()
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    common.OptionMap["HomePageContent"],
	})
	return
}

func SendEmailVerification(c *gin.Context) {
	authSettings := common.GetRequestAuthSettings(c)
	brandingSettings := common.GetBrandingSettings()
	email := model.NormalizeEmail(c.Query("email"))
	if err := common.Validate.Var(email, "required,email"); err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	parts := strings.Split(email, "@")
	if len(parts) != 2 {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "无效的邮箱地址",
		})
		return
	}
	localPart := parts[0]
	domainPart := parts[1]
	if authSettings.EmailDomainRestrictionEnabled {
		allowed := slices.Contains(authSettings.EmailDomainWhitelist, domainPart)
		if !allowed {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": "The administrator has enabled the email domain name whitelist, and your email address is not allowed due to special symbols or it's not in the whitelist.",
			})
			return
		}
	}
	if authSettings.EmailAliasRestrictionEnabled {
		containsSpecialSymbols := strings.Contains(localPart, "+") || strings.Contains(localPart, ".")
		if containsSpecialSymbols {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": "管理员已启用邮箱地址别名限制，您的邮箱地址由于包含特殊符号而被拒绝。",
			})
			return
		}
	}

	if model.IsEmailAlreadyTaken(email) {
		common.ApiErrorI18n(c, i18n.MsgUserEmailAlreadyTaken)
		return
	}
	purpose := common.EmailVerificationPurpose
	if userID := c.GetInt("id"); userID > 0 {
		purpose = common.EmailBindVerificationPurpose(userID)
	}
	code, err := common.GetOrCreateVerificationCodeWithKey(email, purpose, 6)
	if err != nil {
		verificationStoreError(c)
		return
	}
	subject := fmt.Sprintf("%s邮箱验证邮件", brandingSettings.SystemName)
	content := fmt.Sprintf("<p>您好，你正在进行%s邮箱验证。</p>"+
		"<p>您的验证码为: <strong>%s</strong></p>"+
		"<p>验证码 %d 分钟内有效，如果不是本人操作，请忽略。</p>", brandingSettings.SystemName, code, common.VerificationValidMinutes)
	err = common.SendEmail(subject, email, content)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
	})
	return
}

func SendPasswordResetEmail(c *gin.Context) {
	brandingSettings := common.GetBrandingSettings()
	email := model.NormalizeEmail(c.Query("email"))
	if err := common.Validate.Var(email, "required,email"); err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	// Store before the account lookup so dependency errors do not reveal account existence.
	code := common.GenerateVerificationCode(0)
	if err := common.RegisterVerificationCodeWithKey(email, code, common.PasswordResetPurpose); err != nil {
		verificationStoreError(c)
		return
	}
	if _, err := model.GetUniqueUserByEmail(email); err == nil {
		link := fmt.Sprintf("%s/user/reset?email=%s&token=%s", system_setting.GetServerAddress(), url.QueryEscape(email), url.QueryEscape(code))
		subject, content := service.RenderPasswordResetEmail(brandingSettings.SystemName, link, common.VerificationValidMinutes)
		err := common.SendEmail(subject, email, content)
		if err != nil {
			logger.LogError(c.Request.Context(), fmt.Sprintf("failed to send password reset email to %s: %s", email, err.Error()))
		}
	} else if err != nil && !errors.Is(err, model.ErrEmailNotFound) {
		logger.LogWarn(c.Request.Context(), fmt.Sprintf("skip password reset email for %s: %s", email, err.Error()))
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
	})
}

type PasswordResetRequest struct {
	Email string `json:"email"`
	Token string `json:"token"`
}

func ResetPassword(c *gin.Context) {
	var req PasswordResetRequest
	err := json.NewDecoder(c.Request.Body).Decode(&req)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	req.Email = model.NormalizeEmail(req.Email)
	if req.Email == "" || req.Token == "" {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	valid, err := common.ConsumeVerificationCodeWithKey(req.Email, req.Token, common.PasswordResetPurpose)
	if err != nil {
		verificationStoreError(c)
		return
	}
	if !valid {
		common.ApiErrorI18n(c, i18n.MsgUserPasswordResetLinkInvalid)
		return
	}
	password := common.GenerateVerificationCode(12)
	err = model.ResetUserPasswordByEmail(req.Email, password)
	if err != nil {
		if errors.Is(err, model.ErrEmailNotFound) || errors.Is(err, model.ErrEmailAmbiguous) {
			common.ApiErrorI18n(c, i18n.MsgUserPasswordResetLinkInvalid)
			return
		}
		common.ApiError(c, err)
		return
	}
	// eflow: 重置密码通常意味着怀疑账号失守，作废该用户全部登录会话并刷新用户缓存。
	if user, err := model.GetUniqueUserByEmail(req.Email); err == nil {
		revokeUserSessions(c, user.Id, false)
		if err := model.InvalidateUserCache(user.Id); err != nil {
			common.SysLog(fmt.Sprintf("failed to invalidate user cache for user %d: %s", user.Id, err.Error()))
		}
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    password,
	})
	return
}

func verificationStoreError(c *gin.Context) {
	c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "message": common.ErrVerificationStoreUnavailable.Error()})
}
