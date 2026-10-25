package operation_setting

import (
	"fmt"
	"strconv"
	"sync"
	"sync/atomic"
)

// SiteModeSettings is one immutable runtime version of site-wide operating
// modes used by public status and model availability decisions.
type SiteModeSettings struct {
	DemoSiteEnabled    bool
	SelfUseModeEnabled bool
}

var (
	siteModeSettings   atomic.Pointer[SiteModeSettings]
	siteModeSettingsMu sync.Mutex
)

func init() {
	publishSiteModeSettings(SiteModeSettings{})
}

func GetSiteModeSettings() SiteModeSettings {
	current := siteModeSettings.Load()
	if current == nil {
		return SiteModeSettings{}
	}
	return *current
}

func IsSiteModeOptionKey(key string) bool {
	return key == "DemoSiteEnabled" || key == "SelfUseModeEnabled"
}

func ValidateSiteModeSettings(values map[string]string) error {
	_, _, err := mergeSiteModeSettings(GetSiteModeSettings(), values)
	return err
}

func UpdateSiteModeSettings(values map[string]string) error {
	siteModeSettingsMu.Lock()
	defer siteModeSettingsMu.Unlock()

	next, touched, err := mergeSiteModeSettings(GetSiteModeSettings(), values)
	if err != nil {
		return err
	}
	if touched {
		publishSiteModeSettings(next)
	}
	return nil
}

func mergeSiteModeSettings(next SiteModeSettings, values map[string]string) (SiteModeSettings, bool, error) {
	touched := false
	options := []struct {
		key    string
		target *bool
	}{
		{key: "DemoSiteEnabled", target: &next.DemoSiteEnabled},
		{key: "SelfUseModeEnabled", target: &next.SelfUseModeEnabled},
	}
	for _, option := range options {
		value, ok := values[option.key]
		if !ok {
			continue
		}
		parsed, err := strconv.ParseBool(value)
		if err != nil {
			return next, false, fmt.Errorf("%s must be a boolean: %w", option.key, err)
		}
		*option.target = parsed
		touched = true
	}
	return next, touched, nil
}

func publishSiteModeSettings(next SiteModeSettings) {
	siteModeSettings.Store(&next)
}
