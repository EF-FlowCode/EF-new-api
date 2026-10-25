package setting

import (
	"fmt"
	"strconv"
	"sync"
	"sync/atomic"
)

// MidjourneySettings is one immutable runtime version of Midjourney request
// filtering, notification, and URL rewriting behavior.
type MidjourneySettings struct {
	NotifyEnabled             bool
	AccountFilterEnabled      bool
	ModeClearEnabled          bool
	ForwardURLEnabled         bool
	ActionCheckSuccessEnabled bool
}

var (
	midjourneySettings   atomic.Pointer[MidjourneySettings]
	midjourneySettingsMu sync.Mutex
)

func init() {
	publishMidjourneySettings(MidjourneySettings{
		ForwardURLEnabled:         true,
		ActionCheckSuccessEnabled: true,
	})
}

func GetMidjourneySettings() MidjourneySettings {
	current := midjourneySettings.Load()
	if current == nil {
		return MidjourneySettings{}
	}
	return *current
}

func IsMidjourneyOptionKey(key string) bool {
	switch key {
	case "MjNotifyEnabled", "MjAccountFilterEnabled", "MjModeClearEnabled",
		"MjForwardUrlEnabled", "MjActionCheckSuccessEnabled":
		return true
	default:
		return false
	}
}

func ValidateMidjourneySettings(values map[string]string) error {
	_, _, err := mergeMidjourneySettings(GetMidjourneySettings(), values)
	return err
}

func UpdateMidjourneySettings(values map[string]string) error {
	midjourneySettingsMu.Lock()
	defer midjourneySettingsMu.Unlock()

	next, touched, err := mergeMidjourneySettings(GetMidjourneySettings(), values)
	if err != nil {
		return err
	}
	if touched {
		publishMidjourneySettings(next)
	}
	return nil
}

func mergeMidjourneySettings(next MidjourneySettings, values map[string]string) (MidjourneySettings, bool, error) {
	touched := false
	options := []struct {
		key    string
		target *bool
	}{
		{key: "MjNotifyEnabled", target: &next.NotifyEnabled},
		{key: "MjAccountFilterEnabled", target: &next.AccountFilterEnabled},
		{key: "MjModeClearEnabled", target: &next.ModeClearEnabled},
		{key: "MjForwardUrlEnabled", target: &next.ForwardURLEnabled},
		{key: "MjActionCheckSuccessEnabled", target: &next.ActionCheckSuccessEnabled},
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

func publishMidjourneySettings(next MidjourneySettings) {
	midjourneySettings.Store(&next)
}
