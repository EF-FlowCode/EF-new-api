package operation_setting

import (
	"strings"

	"eflow.local/new-api/setting/config"
)

// WebGroupRegionRule controls presentation-only group availability for one
// Cloudflare country code. It is intentionally separate from API authorization.
type WebGroupRegionRule struct {
	DisplayName   string   `json:"display_name"`
	AllowedGroups []string `json:"allowed_groups"`
	PopupEnabled  bool     `json:"popup_enabled"`
	PopupTitle    string   `json:"popup_title"`
	PopupContent  string   `json:"popup_content"`
	PopupNotice   string   `json:"popup_notice"`
	PopupButton   string   `json:"popup_button"`
	Version       string   `json:"version"`
}

type WebGroupSetting struct {
	Enabled bool                          `json:"enabled"`
	Rules   map[string]WebGroupRegionRule `json:"rules"`
}

var webGroupSetting = WebGroupSetting{
	Enabled: false,
	Rules:   map[string]WebGroupRegionRule{},
}

func init() {
	config.GlobalConfig.Register("web_group_setting", &webGroupSetting)
}

func GetWebGroupSetting() WebGroupSetting {
	return config.MustSnapshot[WebGroupSetting]("web_group_setting")
}

func normalizeRegion(region string) string {
	return strings.ToUpper(strings.TrimSpace(region))
}

// GetRegionRule returns an exact country rule, or the optional wildcard rule.
func (s WebGroupSetting) GetRegionRule(region string) (WebGroupRegionRule, bool) {
	if !s.Enabled || len(s.Rules) == 0 {
		return WebGroupRegionRule{}, false
	}
	if rule, ok := s.Rules[normalizeRegion(region)]; ok {
		return rule, true
	}
	for key, rule := range s.Rules {
		if normalizeRegion(key) == normalizeRegion(region) {
			return rule, true
		}
	}
	rule, ok := s.Rules["*"]
	if !ok {
		for key, candidate := range s.Rules {
			if strings.TrimSpace(key) == "*" {
				return candidate, true
			}
		}
	}
	return rule, ok
}

func (s WebGroupSetting) IsGroupAvailable(region, group string) bool {
	rule, restricted := s.GetRegionRule(region)
	if !restricted {
		return true
	}
	for _, allowed := range rule.AllowedGroups {
		if strings.TrimSpace(allowed) == group {
			return true
		}
	}
	return false
}
