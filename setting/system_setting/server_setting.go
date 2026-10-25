package system_setting

import (
	"sync"
	"sync/atomic"
)

// ServerSettings is one immutable runtime version of the public server
// configuration.
type ServerSettings struct {
	Address string
}

var (
	serverSettings   atomic.Pointer[ServerSettings]
	serverSettingsMu sync.Mutex
)

func init() {
	publishServerSettings(ServerSettings{Address: "http://localhost:3000"})
}

func GetServerSettings() ServerSettings {
	current := serverSettings.Load()
	if current == nil {
		return ServerSettings{Address: "http://localhost:3000"}
	}
	return *current
}

func GetServerAddress() string {
	return GetServerSettings().Address
}

// ValidateServerOptionValues is intentionally permissive to retain the
// historical ServerAddress behavior while providing a side-effect-free
// validation hook for option batches.
func ValidateServerOptionValues(values map[string]string) error {
	_ = values
	return nil
}

func UpdateServerSettings(values map[string]string) error {
	serverSettingsMu.Lock()
	defer serverSettingsMu.Unlock()

	next := GetServerSettings()
	if value, ok := values["ServerAddress"]; ok {
		next.Address = value
	}
	publishServerSettings(next)
	return nil
}

func publishServerSettings(next ServerSettings) {
	serverSettings.Store(&next)
}
