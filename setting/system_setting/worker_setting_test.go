package system_setting

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func preserveWorkerSettings(t *testing.T) {
	t.Helper()
	original := GetWorkerSettings()
	t.Cleanup(func() {
		workerSettingsMu.Lock()
		defer workerSettingsMu.Unlock()
		publishWorkerSettings(original)
	})
}

func TestUpdateWorkerSettingsRejectsInvalidBatchWithoutPublishing(t *testing.T) {
	preserveWorkerSettings(t)
	require.NoError(t, UpdateWorkerSettings(map[string]string{
		"WorkerUrl":                          "https://worker.original.example",
		"WorkerValidKey":                     "original-key",
		"WorkerAllowHttpImageRequestEnabled": "false",
	}))
	before := GetWorkerSettings()

	err := UpdateWorkerSettings(map[string]string{
		"WorkerUrl":                          "https://worker.unpublished.example",
		"WorkerAllowHttpImageRequestEnabled": "not-a-bool",
	})

	require.ErrorContains(t, err, "WorkerAllowHttpImageRequestEnabled")
	assert.Equal(t, before, GetWorkerSettings())
}

func TestUpdateWorkerSettingsIgnoresUnknownKeys(t *testing.T) {
	preserveWorkerSettings(t)
	before := GetWorkerSettings()

	require.NoError(t, UpdateWorkerSettings(map[string]string{"UnrelatedOption": "ignored"}))
	assert.Equal(t, before, GetWorkerSettings())
}

func TestWorkerSettingsConcurrentReadersObserveCompleteVersions(t *testing.T) {
	preserveWorkerSettings(t)
	versionA := map[string]string{
		"WorkerUrl":                          "https://worker-a.example",
		"WorkerValidKey":                     "key-a",
		"WorkerAllowHttpImageRequestEnabled": "true",
	}
	versionB := map[string]string{
		"WorkerUrl":                          "https://worker-b.example",
		"WorkerValidKey":                     "key-b",
		"WorkerAllowHttpImageRequestEnabled": "false",
	}
	require.NoError(t, UpdateWorkerSettings(versionA))
	wantA := GetWorkerSettings()
	require.NoError(t, UpdateWorkerSettings(versionB))
	wantB := GetWorkerSettings()
	require.NoError(t, UpdateWorkerSettings(versionA))

	invalid := make(chan WorkerSettings, 1)
	var writers sync.WaitGroup
	for _, values := range []map[string]string{versionA, versionB} {
		writers.Add(1)
		go func() {
			defer writers.Done()
			for range 2000 {
				if err := UpdateWorkerSettings(values); err != nil {
					return
				}
			}
		}()
	}

	for range 10000 {
		got := GetWorkerSettings()
		if got != wantA && got != wantB {
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
		t.Fatalf("reader observed mixed worker settings: %#v", got)
	default:
	}
}
