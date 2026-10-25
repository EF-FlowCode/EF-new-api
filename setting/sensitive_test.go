package setting

import (
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func preserveSensitiveSettings(t *testing.T) {
	t.Helper()
	original := GetSensitiveSettings()
	t.Cleanup(func() {
		sensitiveSettingsMu.Lock()
		defer sensitiveSettingsMu.Unlock()
		publishSensitiveSettings(original)
	})
}

func TestUpdateSensitiveSettingsRejectsInvalidBatchWithoutPublishing(t *testing.T) {
	preserveSensitiveSettings(t)
	require.NoError(t, UpdateSensitiveSettings(sensitiveSettingsOptionValues(sensitiveSettingsVersion("before", true))))
	before := GetSensitiveSettings()

	err := UpdateSensitiveSettings(map[string]string{
		"SensitiveWords":              "unpublished",
		"CheckSensitivePromptEnabled": "not-a-boolean",
		"CheckSensitiveEnabled":       "not-a-boolean",
	})

	require.ErrorContains(t, err, "CheckSensitiveEnabled")
	assert.Equal(t, before, GetSensitiveSettings())
}

func TestGetSensitiveSettingsDoesNotExposePublishedWords(t *testing.T) {
	preserveSensitiveSettings(t)
	require.NoError(t, UpdateSensitiveSettings(map[string]string{"SensitiveWords": "one\ntwo"}))

	returned := GetSensitiveSettings()
	returned.Words[0] = "mutated"

	assert.Equal(t, []string{"one", "two"}, GetSensitiveSettings().Words)
}

func TestSensitiveSettingsConcurrentReadersObserveCompleteVersions(t *testing.T) {
	preserveSensitiveSettings(t)
	versionA := sensitiveSettingsOptionValues(sensitiveSettingsVersion("a", true))
	versionB := sensitiveSettingsOptionValues(sensitiveSettingsVersion("b", false))
	require.NoError(t, UpdateSensitiveSettings(versionA))
	wantA := GetSensitiveSettings()
	require.NoError(t, UpdateSensitiveSettings(versionB))
	wantB := GetSensitiveSettings()
	require.NoError(t, UpdateSensitiveSettings(versionA))

	invalid := make(chan SensitiveSettings, 1)
	var writers sync.WaitGroup
	for _, values := range []map[string]string{versionA, versionB} {
		writers.Add(1)
		go func() {
			defer writers.Done()
			for range 1000 {
				if err := UpdateSensitiveSettings(values); err != nil {
					return
				}
			}
		}()
	}

	for range 5000 {
		got := GetSensitiveSettings()
		if !reflect.DeepEqual(got, wantA) && !reflect.DeepEqual(got, wantB) {
			select {
			case invalid <- got:
			default:
			}
			break
		}
	}
	writers.Wait()
	select {
	case got := <-invalid:
		t.Fatalf("reader observed mixed sensitive settings: %#v", got)
	default:
	}
}

func sensitiveSettingsVersion(version string, enabled bool) SensitiveSettings {
	return SensitiveSettings{
		CheckEnabled:           enabled,
		CheckOnPromptEnabled:   !enabled,
		StopOnSensitive:        enabled,
		StreamCacheQueueLength: len(version) + 10,
		Words:                  []string{"word-" + version, "other-" + version},
	}
}

func sensitiveSettingsOptionValues(settings SensitiveSettings) map[string]string {
	return map[string]string{
		"CheckSensitiveEnabled":         strconv.FormatBool(settings.CheckEnabled),
		"CheckSensitiveOnPromptEnabled": strconv.FormatBool(settings.CheckOnPromptEnabled),
		"StopOnSensitiveEnabled":        strconv.FormatBool(settings.StopOnSensitive),
		"StreamCacheQueueLength":        strconv.Itoa(settings.StreamCacheQueueLength),
		"SensitiveWords":                strings.Join(settings.Words, "\n"),
	}
}
