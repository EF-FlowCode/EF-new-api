package common

import (
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
)

func preserveBrandingSettings(t *testing.T) {
	t.Helper()
	original := GetBrandingSettings()
	t.Cleanup(func() {
		brandingSettingsMu.Lock()
		defer brandingSettingsMu.Unlock()
		publishBrandingSettings(original)
	})
}

func TestBrandingSettingsPartialUpdatePreservesOtherFields(t *testing.T) {
	preserveBrandingSettings(t)
	before := brandingSettingsVersion("before")
	require.NoError(t, UpdateBrandingSettings(brandingSettingsOptionValues(before)))

	require.NoError(t, ValidateBrandingSettings(map[string]string{"SystemName": ""}))
	require.NoError(t, UpdateBrandingSettings(map[string]string{"SystemName": "After"}))

	want := before
	want.SystemName = "After"
	require.Equal(t, want, GetBrandingSettings())
}

func TestBrandingSettingsConcurrentReadersObserveCompleteVersions(t *testing.T) {
	preserveBrandingSettings(t)
	wantA := brandingSettingsVersion("a")
	wantB := brandingSettingsVersion("b")
	versionA := brandingSettingsOptionValues(wantA)
	versionB := brandingSettingsOptionValues(wantB)
	require.NoError(t, UpdateBrandingSettings(versionA))

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
			if UpdateBrandingSettings(values) != nil {
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
				got := GetBrandingSettings()
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

func brandingSettingsVersion(version string) BrandingSettings {
	return BrandingSettings{
		SystemName:         "name-" + version,
		Footer:             "footer-" + version,
		Logo:               "logo-" + version,
		TopUpLink:          "topup-" + version,
		GroupMonitoringURL: "monitor-" + version,
	}
}

func brandingSettingsOptionValues(settings BrandingSettings) map[string]string {
	return map[string]string{
		"SystemName":         settings.SystemName,
		"Footer":             settings.Footer,
		"Logo":               settings.Logo,
		"TopUpLink":          settings.TopUpLink,
		"GroupMonitoringURL": settings.GroupMonitoringURL,
	}
}
