package common

import (
	"sync"
	"sync/atomic"
)

// BrandingSettings is one immutable runtime version of public-facing brand
// content. A response or notification should retain one returned value so a
// bulk option publish cannot mix fields from different versions.
type BrandingSettings struct {
	SystemName         string
	Footer             string
	Logo               string
	TopUpLink          string
	GroupMonitoringURL string
}

var (
	brandingSettings   atomic.Pointer[BrandingSettings]
	brandingSettingsMu sync.Mutex
)

func init() {
	publishBrandingSettings(BrandingSettings{SystemName: "New API"})
}

func GetBrandingSettings() BrandingSettings {
	current := brandingSettings.Load()
	if current == nil {
		return BrandingSettings{}
	}
	return *current
}

func IsBrandingOptionKey(key string) bool {
	switch key {
	case "SystemName", "Footer", "Logo", "TopUpLink", "GroupMonitoringURL":
		return true
	default:
		return false
	}
}

// ValidateBrandingSettings checks a prospective partial update without
// changing the published version. Branding values intentionally accept empty
// and arbitrary strings to preserve the existing option contract.
func ValidateBrandingSettings(values map[string]string) error {
	_, _ = mergeBrandingSettings(GetBrandingSettings(), values)
	return nil
}

func UpdateBrandingSettings(values map[string]string) error {
	brandingSettingsMu.Lock()
	defer brandingSettingsMu.Unlock()

	next, touched := mergeBrandingSettings(GetBrandingSettings(), values)
	if touched {
		publishBrandingSettings(next)
	}
	return nil
}

func mergeBrandingSettings(next BrandingSettings, values map[string]string) (BrandingSettings, bool) {
	touched := false
	for key, value := range values {
		switch key {
		case "SystemName":
			next.SystemName = value
			touched = true
		case "Footer":
			next.Footer = value
			touched = true
		case "Logo":
			next.Logo = value
			touched = true
		case "TopUpLink":
			next.TopUpLink = value
			touched = true
		case "GroupMonitoringURL":
			next.GroupMonitoringURL = value
			touched = true
		}
	}
	return next, touched
}

func publishBrandingSettings(next BrandingSettings) {
	brandingSettings.Store(&next)
}
