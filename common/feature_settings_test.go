package common

import (
	"strconv"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
)

func preserveFeatureSettings(t *testing.T) {
	t.Helper()
	original := GetFeatureSettings()
	t.Cleanup(func() {
		featureSettingsMu.Lock()
		defer featureSettingsMu.Unlock()
		publishFeatureSettings(original)
	})
}

func TestFeatureSettingsRejectsInvalidBatchWithoutPublishing(t *testing.T) {
	preserveFeatureSettings(t)
	require.NoError(t, UpdateFeatureSettings(featureSettingsOptionValues(featureSettingsVersion(true))))
	before := GetFeatureSettings()

	err := UpdateFeatureSettings(map[string]string{
		"DrawingEnabled":     "false",
		"DataExportInterval": "not-an-integer",
	})

	require.ErrorContains(t, err, "DataExportInterval")
	require.Equal(t, before, GetFeatureSettings())
}

func TestFeatureSettingsConcurrentReadersObserveCompleteVersions(t *testing.T) {
	preserveFeatureSettings(t)
	wantA := featureSettingsVersion(true)
	wantB := featureSettingsVersion(false)
	versionA := featureSettingsOptionValues(wantA)
	versionB := featureSettingsOptionValues(wantB)
	require.NoError(t, UpdateFeatureSettings(versionA))

	var invalid atomic.Bool
	var wg sync.WaitGroup
	start := make(chan struct{})
	wg.Add(5)
	go func() {
		defer wg.Done()
		<-start
		for i := range 1000 {
			values := versionA
			if i%2 == 0 {
				values = versionB
			}
			if UpdateFeatureSettings(values) != nil {
				invalid.Store(true)
				return
			}
		}
	}()
	for range 4 {
		go func() {
			defer wg.Done()
			<-start
			for range 2000 {
				got := GetFeatureSettings()
				if got != wantA && got != wantB {
					invalid.Store(true)
					return
				}
			}
		}()
	}
	close(start)
	wg.Wait()
	require.False(t, invalid.Load())
}

func featureSettingsVersion(enabled bool) FeatureSettings {
	defaultTime := "week"
	interval := 17
	if !enabled {
		defaultTime = "day"
		interval = 29
	}
	return FeatureSettings{
		LogConsumeEnabled:        enabled,
		DisplayInCurrencyEnabled: !enabled,
		DisplayTokenStatEnabled:  enabled,
		DrawingEnabled:           !enabled,
		TaskEnabled:              enabled,
		DataExportEnabled:        !enabled,
		DataExportInterval:       interval,
		DataExportDefaultTime:    defaultTime,
		DefaultCollapseSidebar:   enabled,
	}
}

func featureSettingsOptionValues(settings FeatureSettings) map[string]string {
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
