package operation_setting

import (
	"strconv"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
)

func preserveSiteModeSettings(t *testing.T) {
	t.Helper()
	original := GetSiteModeSettings()
	t.Cleanup(func() {
		siteModeSettingsMu.Lock()
		defer siteModeSettingsMu.Unlock()
		publishSiteModeSettings(original)
	})
}

func TestSiteModeSettingsRejectsInvalidBatchWithoutPublishing(t *testing.T) {
	preserveSiteModeSettings(t)
	require.NoError(t, UpdateSiteModeSettings(siteModeSettingsOptionValues(SiteModeSettings{
		DemoSiteEnabled:    true,
		SelfUseModeEnabled: false,
	})))
	before := GetSiteModeSettings()

	err := UpdateSiteModeSettings(map[string]string{
		"DemoSiteEnabled":    "false",
		"SelfUseModeEnabled": "not-a-boolean",
	})

	require.ErrorContains(t, err, "SelfUseModeEnabled")
	require.Equal(t, before, GetSiteModeSettings())
}

func TestSiteModeSettingsConcurrentReadersObserveCompleteVersions(t *testing.T) {
	preserveSiteModeSettings(t)
	wantA := SiteModeSettings{DemoSiteEnabled: true, SelfUseModeEnabled: false}
	wantB := SiteModeSettings{DemoSiteEnabled: false, SelfUseModeEnabled: true}
	versionA := siteModeSettingsOptionValues(wantA)
	versionB := siteModeSettingsOptionValues(wantB)
	require.NoError(t, UpdateSiteModeSettings(versionA))

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
			if UpdateSiteModeSettings(values) != nil {
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
				got := GetSiteModeSettings()
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

func siteModeSettingsOptionValues(settings SiteModeSettings) map[string]string {
	return map[string]string{
		"DemoSiteEnabled":    strconv.FormatBool(settings.DemoSiteEnabled),
		"SelfUseModeEnabled": strconv.FormatBool(settings.SelfUseModeEnabled),
	}
}
