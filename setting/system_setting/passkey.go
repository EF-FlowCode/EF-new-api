package system_setting

import (
	"net/url"
	"slices"
	"strings"

	"eflow.local/new-api/common"
	"eflow.local/new-api/setting/config"
)

type PasskeySettings struct {
	Enabled              bool   `json:"enabled"`
	RPDisplayName        string `json:"rp_display_name"`
	RPID                 string `json:"rp_id"`
	Origins              string `json:"origins"`
	AllowInsecureOrigin  bool   `json:"allow_insecure_origin"`
	UserVerification     string `json:"user_verification"`
	AttachmentPreference string `json:"attachment_preference"`
}

var defaultPasskeySettings = PasskeySettings{
	Enabled:              false,
	RPDisplayName:        common.GetBrandingSettings().SystemName,
	RPID:                 "",
	Origins:              "",
	AllowInsecureOrigin:  false,
	UserVerification:     "preferred",
	AttachmentPreference: "",
}

func init() {
	config.GlobalConfig.Register("passkey", &defaultPasskeySettings)
}

func GetPasskeySettings() *PasskeySettings {
	setting := config.MustSnapshot[PasskeySettings]("passkey")
	serverAddress := GetServerAddress()
	if setting.RPID == "" && serverAddress != "" {
		// 从ServerAddress提取域名作为RPID
		// ServerAddress可能是 "https://newapi.pro" 这种格式
		serverAddr := strings.TrimSpace(serverAddress)
		if parsed, err := url.Parse(serverAddr); err == nil && parsed.Host != "" {
			setting.RPID = parsed.Host
		} else {
			setting.RPID = serverAddr
		}
	}
	if setting.Origins == "" || setting.Origins == "[]" {
		setting.Origins = serverAddress
	}
	return &setting
}

// RPIDs returns the allowed relying party IDs, primary first. eflow: rp_id may
// list several comma-separated domains (upstream 385d2dfd1 intent); the RP ID
// of a request is the allowed one that matches its host.
func (s *PasskeySettings) RPIDs() []string {
	var ids []string
	for _, part := range strings.Split(s.RPID, ",") {
		part = strings.ToLower(strings.TrimSpace(part))
		if part != "" && !slices.Contains(ids, part) {
			ids = append(ids, part)
		}
	}
	return ids
}
