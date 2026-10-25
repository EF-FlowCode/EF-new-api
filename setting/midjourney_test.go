package setting

import (
	"strconv"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
)

func preserveMidjourneySettings(t *testing.T) {
	t.Helper()
	original := GetMidjourneySettings()
	t.Cleanup(func() {
		midjourneySettingsMu.Lock()
		defer midjourneySettingsMu.Unlock()
		publishMidjourneySettings(original)
	})
}

func TestMidjourneySettingsRejectsInvalidBatchWithoutPublishing(t *testing.T) {
	preserveMidjourneySettings(t)
	require.NoError(t, UpdateMidjourneySettings(midjourneySettingsOptionValues(midjourneySettingsVersion(true))))
	before := GetMidjourneySettings()

	err := UpdateMidjourneySettings(map[string]string{
		"MjNotifyEnabled":             "false",
		"MjActionCheckSuccessEnabled": "not-a-boolean",
	})

	require.ErrorContains(t, err, "MjActionCheckSuccessEnabled")
	require.Equal(t, before, GetMidjourneySettings())
}

func TestMidjourneySettingsConcurrentReadersObserveCompleteVersions(t *testing.T) {
	preserveMidjourneySettings(t)
	wantA := midjourneySettingsVersion(true)
	wantB := midjourneySettingsVersion(false)
	versionA := midjourneySettingsOptionValues(wantA)
	versionB := midjourneySettingsOptionValues(wantB)
	require.NoError(t, UpdateMidjourneySettings(versionA))

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
			if UpdateMidjourneySettings(values) != nil {
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
				got := GetMidjourneySettings()
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

func midjourneySettingsVersion(enabled bool) MidjourneySettings {
	return MidjourneySettings{
		NotifyEnabled:             enabled,
		AccountFilterEnabled:      !enabled,
		ModeClearEnabled:          enabled,
		ForwardURLEnabled:         !enabled,
		ActionCheckSuccessEnabled: enabled,
	}
}

func midjourneySettingsOptionValues(settings MidjourneySettings) map[string]string {
	return map[string]string{
		"MjNotifyEnabled":             strconv.FormatBool(settings.NotifyEnabled),
		"MjAccountFilterEnabled":      strconv.FormatBool(settings.AccountFilterEnabled),
		"MjModeClearEnabled":          strconv.FormatBool(settings.ModeClearEnabled),
		"MjForwardUrlEnabled":         strconv.FormatBool(settings.ForwardURLEnabled),
		"MjActionCheckSuccessEnabled": strconv.FormatBool(settings.ActionCheckSuccessEnabled),
	}
}
