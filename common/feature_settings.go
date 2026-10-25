package common

import (
	"fmt"
	"strconv"
	"sync"
	"sync/atomic"
)

// FeatureSettings is one immutable runtime version of general feature gates
// and data-export behavior.
type FeatureSettings struct {
	LogConsumeEnabled        bool
	DisplayInCurrencyEnabled bool
	DisplayTokenStatEnabled  bool
	DrawingEnabled           bool
	TaskEnabled              bool
	DataExportEnabled        bool
	DataExportInterval       int
	DataExportDefaultTime    string
	DefaultCollapseSidebar   bool
}

var (
	featureSettings   atomic.Pointer[FeatureSettings]
	featureSettingsMu sync.Mutex
)

func init() {
	publishFeatureSettings(FeatureSettings{
		LogConsumeEnabled:        true,
		DisplayInCurrencyEnabled: true,
		DisplayTokenStatEnabled:  true,
		DrawingEnabled:           true,
		TaskEnabled:              true,
		DataExportEnabled:        true,
		DataExportInterval:       5,
		DataExportDefaultTime:    "hour",
	})
}

func GetFeatureSettings() FeatureSettings {
	current := featureSettings.Load()
	if current == nil {
		return FeatureSettings{}
	}
	return *current
}

func IsFeatureOptionKey(key string) bool {
	switch key {
	case "LogConsumeEnabled", "DisplayInCurrencyEnabled", "DisplayTokenStatEnabled",
		"DrawingEnabled", "TaskEnabled", "DataExportEnabled", "DataExportInterval",
		"DataExportDefaultTime", "DefaultCollapseSidebar":
		return true
	default:
		return false
	}
}

func ValidateFeatureSettings(values map[string]string) error {
	_, _, err := mergeFeatureSettings(GetFeatureSettings(), values)
	return err
}

func UpdateFeatureSettings(values map[string]string) error {
	featureSettingsMu.Lock()
	defer featureSettingsMu.Unlock()

	next, touched, err := mergeFeatureSettings(GetFeatureSettings(), values)
	if err != nil {
		return err
	}
	if touched {
		publishFeatureSettings(next)
	}
	return nil
}

func mergeFeatureSettings(next FeatureSettings, values map[string]string) (FeatureSettings, bool, error) {
	touched := false
	boolOptions := []struct {
		key    string
		target *bool
	}{
		{key: "LogConsumeEnabled", target: &next.LogConsumeEnabled},
		{key: "DisplayInCurrencyEnabled", target: &next.DisplayInCurrencyEnabled},
		{key: "DisplayTokenStatEnabled", target: &next.DisplayTokenStatEnabled},
		{key: "DrawingEnabled", target: &next.DrawingEnabled},
		{key: "TaskEnabled", target: &next.TaskEnabled},
		{key: "DataExportEnabled", target: &next.DataExportEnabled},
		{key: "DefaultCollapseSidebar", target: &next.DefaultCollapseSidebar},
	}
	for _, option := range boolOptions {
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
	if value, ok := values["DataExportInterval"]; ok {
		parsed, err := strconv.Atoi(value)
		if err != nil || parsed < 0 {
			return next, false, fmt.Errorf("DataExportInterval must be a non-negative integer")
		}
		next.DataExportInterval = parsed
		touched = true
	}
	if value, ok := values["DataExportDefaultTime"]; ok {
		switch value {
		case "hour", "day", "week":
			next.DataExportDefaultTime = value
			touched = true
		default:
			return next, false, fmt.Errorf("invalid data export default time %q", value)
		}
	}
	return next, touched, nil
}

func publishFeatureSettings(next FeatureSettings) {
	featureSettings.Store(&next)
}
