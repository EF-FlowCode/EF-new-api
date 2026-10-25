package system_setting

import (
	"fmt"
	"strconv"
	"sync"
	"sync/atomic"
)

// WorkerSettings is one immutable runtime version of the outbound worker
// configuration. Retaining this value keeps every redirect hop on one worker
// endpoint with one credential and transport policy.
type WorkerSettings struct {
	URL                   string
	ValidKey              string
	AllowHTTPImageRequest bool
}

var (
	workerSettings   atomic.Pointer[WorkerSettings]
	workerSettingsMu sync.Mutex
)

func init() {
	publishWorkerSettings(WorkerSettings{})
}

// GetWorkerSettings returns one immutable runtime configuration version.
func GetWorkerSettings() WorkerSettings {
	current := workerSettings.Load()
	if current == nil {
		return WorkerSettings{}
	}
	return *current
}

// UpdateWorkerSettings parses every supplied worker option before atomically
// publishing one complete replacement. Unknown option keys are ignored.
func UpdateWorkerSettings(values map[string]string) error {
	workerSettingsMu.Lock()
	defer workerSettingsMu.Unlock()

	next := GetWorkerSettings()
	if value, ok := values["WorkerUrl"]; ok {
		next.URL = value
	}
	if value, ok := values["WorkerValidKey"]; ok {
		next.ValidKey = value
	}
	if value, ok := values["WorkerAllowHttpImageRequestEnabled"]; ok {
		parsed, err := strconv.ParseBool(value)
		if err != nil {
			return fmt.Errorf("WorkerAllowHttpImageRequestEnabled must be a boolean: %w", err)
		}
		next.AllowHTTPImageRequest = parsed
	}

	publishWorkerSettings(next)
	return nil
}

func (settings WorkerSettings) Enabled() bool {
	return settings.URL != ""
}

func publishWorkerSettings(next WorkerSettings) {
	workerSettings.Store(&next)
}
