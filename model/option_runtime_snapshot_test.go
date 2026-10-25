package model

import (
	"maps"
	"strconv"
	"strings"
	"testing"

	"eflow.local/new-api/common"
	"eflow.local/new-api/setting"
	"eflow.local/new-api/setting/config"
	"eflow.local/new-api/setting/operation_setting"
	"github.com/stretchr/testify/require"
)

func TestUpdateOptionsBulkPublishesRuntimePolicySnapshots(t *testing.T) {
	useModelIntegrationTestDB(t, &Option{})
	preserveOptionMap(t)
	originalAuth := common.GetAuthSettings()
	originalQuota := common.GetUserQuotaSettings()
	originalBranding := common.GetBrandingSettings()
	originalFeatures := common.GetFeatureSettings()
	originalSensitive := setting.GetSensitiveSettings()
	originalMidjourney := setting.GetMidjourneySettings()
	originalSiteMode := operation_setting.GetSiteModeSettings()
	originalDisplayType := operation_setting.GetGeneralSetting().QuotaDisplayType
	t.Cleanup(func() {
		require.NoError(t, common.UpdateAuthSettings(modelAuthOptionValues(originalAuth)))
		require.NoError(t, common.UpdateUserQuotaSettings(modelUserQuotaOptionValues(originalQuota)))
		require.NoError(t, common.UpdateBrandingSettings(modelBrandingOptionValues(originalBranding)))
		require.NoError(t, common.UpdateFeatureSettings(modelFeatureOptionValues(originalFeatures)))
		require.NoError(t, setting.UpdateSensitiveSettings(modelSensitiveOptionValues(originalSensitive)))
		require.NoError(t, setting.UpdateMidjourneySettings(modelMidjourneyOptionValues(originalMidjourney)))
		require.NoError(t, operation_setting.UpdateSiteModeSettings(modelSiteModeOptionValues(originalSiteMode)))
		_, err := config.GlobalConfig.Update("general_setting", map[string]string{"quota_display_type": originalDisplayType})
		require.NoError(t, err)
	})

	wantAuth := common.AuthSettings{
		PasswordLoginEnabled:              false,
		PasswordRegisterEnabled:           true,
		EmailVerificationEnabled:          true,
		RegistrationEnabled:               true,
		TurnstileCheckEnabled:             true,
		TurnstileSiteKey:                  "site-published",
		TurnstileSecretKey:                "secret-published",
		InvitationCodeEnabled:             true,
		InvitationCodeOAuthRequired:       true,
		InvitationCodeUserGenerateEnabled: false,
		EmailDomainRestrictionEnabled:     true,
		EmailAliasRestrictionEnabled:      true,
		EmailDomainWhitelist:              []string{"one.example", "two.example"},
	}
	wantQuota := common.UserQuotaSettings{
		QuotaForNewUser:      101,
		QuotaForInviter:      202,
		QuotaForInvitee:      303,
		QuotaRemindThreshold: 404,
		PreConsumedQuota:     505,
		AffiliateEnabled:     false,
	}
	wantBranding := common.BrandingSettings{
		SystemName:         "Published API",
		Footer:             "published footer",
		Logo:               "https://assets.example/logo.png",
		TopUpLink:          "https://billing.example",
		GroupMonitoringURL: "https://monitor.example",
	}
	wantFeatures := common.FeatureSettings{
		LogConsumeEnabled:        false,
		DisplayInCurrencyEnabled: false,
		DisplayTokenStatEnabled:  false,
		DrawingEnabled:           false,
		TaskEnabled:              true,
		DataExportEnabled:        false,
		DataExportInterval:       37,
		DataExportDefaultTime:    "week",
		DefaultCollapseSidebar:   true,
	}
	wantSensitive := setting.SensitiveSettings{
		CheckEnabled:           true,
		CheckOnPromptEnabled:   true,
		StopOnSensitive:        false,
		StreamCacheQueueLength: 17,
		Words:                  []string{"alpha", "beta"},
	}
	wantMidjourney := setting.MidjourneySettings{
		NotifyEnabled:             true,
		AccountFilterEnabled:      true,
		ModeClearEnabled:          true,
		ForwardURLEnabled:         false,
		ActionCheckSuccessEnabled: false,
	}
	wantSiteMode := operation_setting.SiteModeSettings{
		DemoSiteEnabled:    true,
		SelfUseModeEnabled: true,
	}
	values := modelAuthOptionValues(wantAuth)
	maps.Copy(values, modelUserQuotaOptionValues(wantQuota))
	maps.Copy(values, modelBrandingOptionValues(wantBranding))
	maps.Copy(values, modelFeatureOptionValues(wantFeatures))
	maps.Copy(values, modelSensitiveOptionValues(wantSensitive))
	maps.Copy(values, modelMidjourneyOptionValues(wantMidjourney))
	maps.Copy(values, modelSiteModeOptionValues(wantSiteMode))

	require.NoError(t, UpdateOptionsBulk(values))
	require.Equal(t, wantAuth, common.GetAuthSettings())
	require.Equal(t, wantQuota, common.GetUserQuotaSettings())
	require.Equal(t, wantBranding, common.GetBrandingSettings())
	require.Equal(t, wantFeatures, common.GetFeatureSettings())
	require.Equal(t, wantSensitive, setting.GetSensitiveSettings())
	require.Equal(t, wantMidjourney, setting.GetMidjourneySettings())
	require.Equal(t, wantSiteMode, operation_setting.GetSiteModeSettings())
	require.Equal(t, operation_setting.QuotaDisplayTypeTokens, operation_setting.GetGeneralSetting().QuotaDisplayType)

	common.OptionMapRWMutex.RLock()
	defer common.OptionMapRWMutex.RUnlock()
	for key, want := range values {
		require.Equal(t, want, common.OptionMap[key], key)
	}
}

func modelBrandingOptionValues(settings common.BrandingSettings) map[string]string {
	return map[string]string{
		"SystemName":         settings.SystemName,
		"Footer":             settings.Footer,
		"Logo":               settings.Logo,
		"TopUpLink":          settings.TopUpLink,
		"GroupMonitoringURL": settings.GroupMonitoringURL,
	}
}

func modelFeatureOptionValues(settings common.FeatureSettings) map[string]string {
	return map[string]string{
		"LogConsumeEnabled":        strconv.FormatBool(settings.LogConsumeEnabled),
		"DisplayInCurrencyEnabled": strconv.FormatBool(settings.DisplayInCurrencyEnabled),
		"DisplayTokenStatEnabled":  strconv.FormatBool(settings.DisplayTokenStatEnabled),
		"DrawingEnabled":           strconv.FormatBool(settings.DrawingEnabled),
		"TaskEnabled":              strconv.FormatBool(settings.TaskEnabled),
		"DataExportEnabled":        strconv.FormatBool(settings.DataExportEnabled),
		"DataExportInterval":       strconv.Itoa(settings.DataExportInterval),
		"DataExportDefaultTime":    settings.DataExportDefaultTime,
		"DefaultCollapseSidebar":   strconv.FormatBool(settings.DefaultCollapseSidebar),
	}
}

func modelMidjourneyOptionValues(settings setting.MidjourneySettings) map[string]string {
	return map[string]string{
		"MjNotifyEnabled":             strconv.FormatBool(settings.NotifyEnabled),
		"MjAccountFilterEnabled":      strconv.FormatBool(settings.AccountFilterEnabled),
		"MjModeClearEnabled":          strconv.FormatBool(settings.ModeClearEnabled),
		"MjForwardUrlEnabled":         strconv.FormatBool(settings.ForwardURLEnabled),
		"MjActionCheckSuccessEnabled": strconv.FormatBool(settings.ActionCheckSuccessEnabled),
	}
}

func modelSiteModeOptionValues(settings operation_setting.SiteModeSettings) map[string]string {
	return map[string]string{
		"DemoSiteEnabled":    strconv.FormatBool(settings.DemoSiteEnabled),
		"SelfUseModeEnabled": strconv.FormatBool(settings.SelfUseModeEnabled),
	}
}

func modelAuthOptionValues(settings common.AuthSettings) map[string]string {
	return map[string]string{
		"PasswordLoginEnabled":              strconv.FormatBool(settings.PasswordLoginEnabled),
		"PasswordRegisterEnabled":           strconv.FormatBool(settings.PasswordRegisterEnabled),
		"EmailVerificationEnabled":          strconv.FormatBool(settings.EmailVerificationEnabled),
		"RegisterEnabled":                   strconv.FormatBool(settings.RegistrationEnabled),
		"TurnstileCheckEnabled":             strconv.FormatBool(settings.TurnstileCheckEnabled),
		"TurnstileSiteKey":                  settings.TurnstileSiteKey,
		"TurnstileSecretKey":                settings.TurnstileSecretKey,
		"InvitationCodeEnabled":             strconv.FormatBool(settings.InvitationCodeEnabled),
		"InvitationCodeOAuthRequired":       strconv.FormatBool(settings.InvitationCodeOAuthRequired),
		"InvitationCodeUserGenerateEnabled": strconv.FormatBool(settings.InvitationCodeUserGenerateEnabled),
		"EmailDomainRestrictionEnabled":     strconv.FormatBool(settings.EmailDomainRestrictionEnabled),
		"EmailAliasRestrictionEnabled":      strconv.FormatBool(settings.EmailAliasRestrictionEnabled),
		"EmailDomainWhitelist":              strings.Join(settings.EmailDomainWhitelist, ","),
	}
}

func modelUserQuotaOptionValues(settings common.UserQuotaSettings) map[string]string {
	return map[string]string{
		"QuotaForNewUser":      strconv.Itoa(settings.QuotaForNewUser),
		"QuotaForInviter":      strconv.Itoa(settings.QuotaForInviter),
		"QuotaForInvitee":      strconv.Itoa(settings.QuotaForInvitee),
		"QuotaRemindThreshold": strconv.Itoa(settings.QuotaRemindThreshold),
		"PreConsumedQuota":     strconv.Itoa(settings.PreConsumedQuota),
		"AffiliateEnabled":     strconv.FormatBool(settings.AffiliateEnabled),
	}
}

func modelSensitiveOptionValues(settings setting.SensitiveSettings) map[string]string {
	return map[string]string{
		"CheckSensitiveEnabled":         strconv.FormatBool(settings.CheckEnabled),
		"CheckSensitiveOnPromptEnabled": strconv.FormatBool(settings.CheckOnPromptEnabled),
		"StopOnSensitiveEnabled":        strconv.FormatBool(settings.StopOnSensitive),
		"StreamCacheQueueLength":        strconv.Itoa(settings.StreamCacheQueueLength),
		"SensitiveWords":                strings.Join(settings.Words, "\n"),
	}
}
