package oauth

import (
	"strconv"
	"testing"

	"eflow.local/new-api/setting/system_setting"
)

func TestGetProviderCapturesLegacyOAuthSnapshotForRequest(t *testing.T) {
	original := system_setting.GetLegacyOAuthSettings()
	t.Cleanup(func() {
		if err := system_setting.UpdateLegacyOAuthSettings(oauthSnapshotOptionValues(original)); err != nil {
			t.Fatalf("restore legacy OAuth settings: %v", err)
		}
	})

	versionA := oauthSnapshotSettings("a", true, 1)
	versionB := oauthSnapshotSettings("b", false, 4)
	if err := system_setting.UpdateLegacyOAuthSettings(oauthSnapshotOptionValues(versionA)); err != nil {
		t.Fatalf("publish version A: %v", err)
	}

	githubA, ok := GetProvider("github").(*GitHubProvider)
	if !ok {
		t.Fatal("github provider is not registered")
	}
	linuxDOA, ok := GetProvider("linuxdo").(*LinuxDOProvider)
	if !ok {
		t.Fatal("linuxdo provider is not registered")
	}
	if err := system_setting.UpdateLegacyOAuthSettings(oauthSnapshotOptionValues(versionB)); err != nil {
		t.Fatalf("publish version B: %v", err)
	}

	if got := githubA.legacyOAuthSettings(); got != versionA {
		t.Fatalf("existing GitHub request changed versions: got %+v, want %+v", got, versionA)
	}
	if got := linuxDOA.legacyOAuthSettings(); got != versionA {
		t.Fatalf("existing LinuxDO request changed versions: got %+v, want %+v", got, versionA)
	}
	if !githubA.IsEnabled() || !linuxDOA.IsEnabled() {
		t.Fatal("existing providers did not retain version A enabled state")
	}

	githubB := GetProvider("github").(*GitHubProvider)
	linuxDOB := GetProvider("linuxdo").(*LinuxDOProvider)
	if got := githubB.legacyOAuthSettings(); got != versionB {
		t.Fatalf("new GitHub request did not capture version B: got %+v, want %+v", got, versionB)
	}
	if got := linuxDOB.legacyOAuthSettings(); got != versionB {
		t.Fatalf("new LinuxDO request did not capture version B: got %+v, want %+v", got, versionB)
	}
	if githubB.IsEnabled() || linuxDOB.IsEnabled() {
		t.Fatal("new providers did not use version B enabled state")
	}
}

func oauthSnapshotSettings(version string, enabled bool, trustLevel int) system_setting.LegacyOAuthSettings {
	return system_setting.LegacyOAuthSettings{
		GitHubOAuthEnabled:          enabled,
		GitHubClientId:              "github-id-" + version,
		GitHubClientSecret:          "github-secret-" + version,
		LinuxDOOAuthEnabled:         enabled,
		LinuxDOClientId:             "linuxdo-id-" + version,
		LinuxDOClientSecret:         "linuxdo-secret-" + version,
		LinuxDOMinimumTrustLevel:    trustLevel,
		TelegramOAuthEnabled:        enabled,
		TelegramBotToken:            "telegram-token-" + version,
		TelegramBotName:             "telegram-name-" + version,
		WeChatAuthEnabled:           enabled,
		WeChatServerAddress:         "https://wechat-" + version + ".example.com",
		WeChatServerToken:           "wechat-token-" + version,
		WeChatAccountQRCodeImageURL: "https://wechat-" + version + ".example.com/qr.png",
	}
}

func oauthSnapshotOptionValues(settings system_setting.LegacyOAuthSettings) map[string]string {
	return map[string]string{
		"GitHubOAuthEnabled":          strconv.FormatBool(settings.GitHubOAuthEnabled),
		"GitHubClientId":              settings.GitHubClientId,
		"GitHubClientSecret":          settings.GitHubClientSecret,
		"LinuxDOOAuthEnabled":         strconv.FormatBool(settings.LinuxDOOAuthEnabled),
		"LinuxDOClientId":             settings.LinuxDOClientId,
		"LinuxDOClientSecret":         settings.LinuxDOClientSecret,
		"LinuxDOMinimumTrustLevel":    strconv.Itoa(settings.LinuxDOMinimumTrustLevel),
		"TelegramOAuthEnabled":        strconv.FormatBool(settings.TelegramOAuthEnabled),
		"TelegramBotToken":            settings.TelegramBotToken,
		"TelegramBotName":             settings.TelegramBotName,
		"WeChatAuthEnabled":           strconv.FormatBool(settings.WeChatAuthEnabled),
		"WeChatServerAddress":         settings.WeChatServerAddress,
		"WeChatServerToken":           settings.WeChatServerToken,
		"WeChatAccountQRCodeImageURL": settings.WeChatAccountQRCodeImageURL,
	}
}
