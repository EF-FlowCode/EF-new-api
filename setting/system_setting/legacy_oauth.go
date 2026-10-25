package system_setting

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
)

// LegacyOAuthSettings groups the un-namespaced OAuth options that predate the
// config manager. Callers should retain one returned value for the full request.
type LegacyOAuthSettings struct {
	GitHubOAuthEnabled       bool
	GitHubClientId           string
	GitHubClientSecret       string
	LinuxDOOAuthEnabled      bool
	LinuxDOClientId          string
	LinuxDOClientSecret      string
	LinuxDOMinimumTrustLevel int
	TelegramOAuthEnabled     bool
	TelegramBotToken         string
	TelegramBotName          string
	// TelegramClientId/Secret configure Telegram's OpenID Connect login
	// (eflow, upstream 3e84ec0ab). With them set the legacy widget is retired.
	TelegramClientId            string
	TelegramClientSecret        string
	WeChatAuthEnabled           bool
	WeChatServerAddress         string
	WeChatServerToken           string
	WeChatAccountQRCodeImageURL string
}

var (
	legacyOAuthSettings   atomic.Pointer[LegacyOAuthSettings]
	legacyOAuthSettingsMu sync.Mutex
)

func init() {
	publishLegacyOAuthSettings(LegacyOAuthSettings{})
}

// GetLegacyOAuthSettings returns one immutable runtime version.
func GetLegacyOAuthSettings() LegacyOAuthSettings {
	current := legacyOAuthSettings.Load()
	if current == nil {
		return LegacyOAuthSettings{}
	}
	return *current
}

// UpdateLegacyOAuthSettings merges supplied legacy option keys and publishes
// one complete replacement. Unknown keys are ignored.
func UpdateLegacyOAuthSettings(values map[string]string) error {
	legacyOAuthSettingsMu.Lock()
	defer legacyOAuthSettingsMu.Unlock()

	next := GetLegacyOAuthSettings()
	touched := false
	for key, value := range values {
		switch key {
		case "GitHubOAuthEnabled":
			parsed, err := parseLegacyOAuthBool(key, value)
			if err != nil {
				return err
			}
			next.GitHubOAuthEnabled = parsed
			touched = true
		case "GitHubClientId":
			next.GitHubClientId = value
			touched = true
		case "GitHubClientSecret":
			next.GitHubClientSecret = value
			touched = true
		case "LinuxDOOAuthEnabled":
			parsed, err := parseLegacyOAuthBool(key, value)
			if err != nil {
				return err
			}
			next.LinuxDOOAuthEnabled = parsed
			touched = true
		case "LinuxDOClientId":
			next.LinuxDOClientId = value
			touched = true
		case "LinuxDOClientSecret":
			next.LinuxDOClientSecret = value
			touched = true
		case "LinuxDOMinimumTrustLevel":
			parsed, err := strconv.Atoi(value)
			if err != nil || parsed < 0 || parsed > 4 {
				return fmt.Errorf("invalid %s %q", key, value)
			}
			next.LinuxDOMinimumTrustLevel = parsed
			touched = true
		case "TelegramOAuthEnabled":
			parsed, err := parseLegacyOAuthBool(key, value)
			if err != nil {
				return err
			}
			next.TelegramOAuthEnabled = parsed
			touched = true
		case "TelegramBotToken":
			next.TelegramBotToken = value
			touched = true
		case "TelegramBotName":
			next.TelegramBotName = value
			touched = true
		case "TelegramClientId":
			next.TelegramClientId = value
			touched = true
		case "TelegramClientSecret":
			next.TelegramClientSecret = value
			touched = true
		case "WeChatAuthEnabled":
			parsed, err := parseLegacyOAuthBool(key, value)
			if err != nil {
				return err
			}
			next.WeChatAuthEnabled = parsed
			touched = true
		case "WeChatServerAddress":
			next.WeChatServerAddress = value
			touched = true
		case "WeChatServerToken":
			next.WeChatServerToken = value
			touched = true
		case "WeChatAccountQRCodeImageURL":
			next.WeChatAccountQRCodeImageURL = value
			touched = true
		}
	}
	if touched {
		publishLegacyOAuthSettings(next)
	}
	return nil
}

func parseLegacyOAuthBool(key, value string) (bool, error) {
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return false, fmt.Errorf("invalid %s %q: %w", key, value, err)
	}
	return parsed, nil
}

func publishLegacyOAuthSettings(next LegacyOAuthSettings) {
	legacyOAuthSettings.Store(&next)
}

// TelegramOIDCEnabled reports whether Telegram sign-in uses OpenID Connect.
func (s LegacyOAuthSettings) TelegramOIDCEnabled() bool {
	return s.TelegramOAuthEnabled && strings.TrimSpace(s.TelegramClientId) != "" && strings.TrimSpace(s.TelegramClientSecret) != ""
}
