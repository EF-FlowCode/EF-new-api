package common

import (
	"fmt"
	"strconv"
	"sync"
	"sync/atomic"
)

// SMTPSettings is one immutable runtime version of the SMTP configuration.
// Callers that use multiple fields must retain one value for the whole
// operation so a concurrent option update cannot mix configuration versions.
type SMTPSettings struct {
	Server             string
	Port               int
	SSLEnabled         bool
	StartTLSEnabled    bool
	InsecureSkipVerify bool
	ForceAuthLogin     bool
	Account            string
	From               string
	Token              string
	ArchiveBcc         string
}

var (
	smtpSettings   atomic.Pointer[SMTPSettings]
	smtpSettingsMu sync.Mutex
)

func init() {
	publishSMTPSettings(SMTPSettings{Port: 587})
}

// GetSMTPSettings returns one immutable runtime configuration version.
func GetSMTPSettings() SMTPSettings {
	current := smtpSettings.Load()
	if current == nil {
		return SMTPSettings{Port: 587}
	}
	return *current
}

// UpdateSMTPSettings parses every supplied SMTP option before atomically
// publishing one complete replacement. Unknown option keys are ignored.
func UpdateSMTPSettings(values map[string]string) error {
	smtpSettingsMu.Lock()
	defer smtpSettingsMu.Unlock()

	next := GetSMTPSettings()
	if value, ok := values["SMTPServer"]; ok {
		next.Server = value
	}
	if value, ok := values["SMTPPort"]; ok {
		port, err := strconv.Atoi(value)
		if err != nil || port < 1 || port > 65535 {
			return fmt.Errorf("SMTPPort must be between 1 and 65535")
		}
		next.Port = port
	}
	if value, ok := values["SMTPSSLEnabled"]; ok {
		parsed, err := strconv.ParseBool(value)
		if err != nil {
			return fmt.Errorf("SMTPSSLEnabled must be a boolean: %w", err)
		}
		next.SSLEnabled = parsed
	}
	if value, ok := values["SMTPStartTLSEnabled"]; ok {
		parsed, err := strconv.ParseBool(value)
		if err != nil {
			return fmt.Errorf("SMTPStartTLSEnabled must be a boolean: %w", err)
		}
		next.StartTLSEnabled = parsed
	}
	if value, ok := values["SMTPInsecureSkipVerify"]; ok {
		parsed, err := strconv.ParseBool(value)
		if err != nil {
			return fmt.Errorf("SMTPInsecureSkipVerify must be a boolean: %w", err)
		}
		next.InsecureSkipVerify = parsed
	}
	if value, ok := values["SMTPForceAuthLogin"]; ok {
		parsed, err := strconv.ParseBool(value)
		if err != nil {
			return fmt.Errorf("SMTPForceAuthLogin must be a boolean: %w", err)
		}
		next.ForceAuthLogin = parsed
	}
	if value, ok := values["SMTPAccount"]; ok {
		next.Account = value
	}
	if value, ok := values["SMTPFrom"]; ok {
		next.From = value
	}
	if value, ok := values["SMTPToken"]; ok {
		next.Token = value
	}
	if value, ok := values["SMTPArchiveBcc"]; ok {
		next.ArchiveBcc = value
	}

	publishSMTPSettings(next)
	return nil
}

func publishSMTPSettings(next SMTPSettings) {
	smtpSettings.Store(&next)
}
