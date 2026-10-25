package system_setting

import (
	"fmt"
	"runtime"
	"strconv"
	"sync"
	"testing"
)

func TestUpdateLegacyOAuthSettingsPublishesWholeVersion(t *testing.T) {
	original := GetLegacyOAuthSettings()
	t.Cleanup(func() {
		if err := UpdateLegacyOAuthSettings(legacyOAuthOptionValues(original)); err != nil {
			t.Fatalf("restore legacy OAuth settings: %v", err)
		}
	})

	versionA := legacyOAuthTestSettings("a", true, 1)
	versionB := legacyOAuthTestSettings("b", false, 4)
	if err := UpdateLegacyOAuthSettings(legacyOAuthOptionValues(versionA)); err != nil {
		t.Fatalf("publish initial settings: %v", err)
	}

	const iterations = 2_000
	start := make(chan struct{})
	errCh := make(chan error, 1)
	report := func(err error) {
		select {
		case errCh <- err:
		default:
		}
	}

	var workers sync.WaitGroup
	workers.Add(1)
	go func() {
		defer workers.Done()
		<-start
		for i := range iterations {
			next := versionA
			if i%2 == 0 {
				next = versionB
			}
			if err := UpdateLegacyOAuthSettings(legacyOAuthOptionValues(next)); err != nil {
				report(err)
				return
			}
			runtime.Gosched()
		}
	}()
	for range 4 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			for range iterations {
				got := GetLegacyOAuthSettings()
				if got != versionA && got != versionB {
					report(fmt.Errorf("observed torn legacy OAuth settings: %+v", got))
					return
				}
				runtime.Gosched()
			}
		}()
	}

	close(start)
	workers.Wait()
	select {
	case err := <-errCh:
		t.Fatal(err)
	default:
	}
}

func TestUpdateLegacyOAuthSettingsInvalidValueKeepsSnapshot(t *testing.T) {
	original := GetLegacyOAuthSettings()
	t.Cleanup(func() {
		if err := UpdateLegacyOAuthSettings(legacyOAuthOptionValues(original)); err != nil {
			t.Fatalf("restore legacy OAuth settings: %v", err)
		}
	})

	baseline := legacyOAuthTestSettings("baseline", true, 2)
	if err := UpdateLegacyOAuthSettings(legacyOAuthOptionValues(baseline)); err != nil {
		t.Fatalf("publish baseline: %v", err)
	}

	for _, testCase := range []struct {
		name   string
		values map[string]string
	}{
		{name: "invalid boolean", values: map[string]string{"GitHubClientId": "changed", "GitHubOAuthEnabled": "sometimes"}},
		{name: "invalid trust level", values: map[string]string{"LinuxDOClientId": "changed", "LinuxDOMinimumTrustLevel": "5"}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if err := UpdateLegacyOAuthSettings(testCase.values); err == nil {
				t.Fatal("expected invalid update to fail")
			}
			if got := GetLegacyOAuthSettings(); got != baseline {
				t.Fatalf("failed update changed snapshot: got %+v, want %+v", got, baseline)
			}
		})
	}
}

func legacyOAuthTestSettings(version string, enabled bool, trustLevel int) LegacyOAuthSettings {
	return LegacyOAuthSettings{
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

func legacyOAuthOptionValues(settings LegacyOAuthSettings) map[string]string {
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
