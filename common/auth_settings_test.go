package common

import (
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func preserveAuthSettings(t *testing.T) {
	t.Helper()
	original := GetAuthSettings()
	t.Cleanup(func() {
		authSettingsMu.Lock()
		defer authSettingsMu.Unlock()
		publishAuthSettings(original)
	})
}

func TestUpdateAuthSettingsRejectsInvalidBatchWithoutPublishing(t *testing.T) {
	preserveAuthSettings(t)
	require.NoError(t, UpdateAuthSettings(authSettingsOptionValues(authSettingsVersion("before", true))))
	before := GetAuthSettings()

	err := UpdateAuthSettings(map[string]string{
		"EmailDomainWhitelist": "unpublished.example",
		"RegisterEnabled":      "not-a-boolean",
	})

	require.ErrorContains(t, err, "RegisterEnabled")
	assert.Equal(t, before, GetAuthSettings())
}

func TestGetAuthSettingsDoesNotExposePublishedWhitelist(t *testing.T) {
	preserveAuthSettings(t)
	require.NoError(t, UpdateAuthSettings(map[string]string{"EmailDomainWhitelist": "one.example,two.example"}))

	returned := GetAuthSettings()
	returned.EmailDomainWhitelist[0] = "mutated.example"

	assert.Equal(t, []string{"one.example", "two.example"}, GetAuthSettings().EmailDomainWhitelist)
}

func TestAuthSettingsConcurrentReadersObserveCompleteVersions(t *testing.T) {
	preserveAuthSettings(t)
	versionA := authSettingsOptionValues(authSettingsVersion("a", true))
	versionB := authSettingsOptionValues(authSettingsVersion("b", false))
	require.NoError(t, UpdateAuthSettings(versionA))
	wantA := GetAuthSettings()
	require.NoError(t, UpdateAuthSettings(versionB))
	wantB := GetAuthSettings()
	require.NoError(t, UpdateAuthSettings(versionA))

	invalid := make(chan AuthSettings, 1)
	var writers sync.WaitGroup
	for _, values := range []map[string]string{versionA, versionB} {
		writers.Add(1)
		go func() {
			defer writers.Done()
			for range 1000 {
				if err := UpdateAuthSettings(values); err != nil {
					return
				}
			}
		}()
	}

	for range 5000 {
		got := GetAuthSettings()
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
		t.Fatalf("reader observed mixed auth settings: %#v", got)
	default:
	}
}

func authSettingsVersion(version string, enabled bool) AuthSettings {
	return AuthSettings{
		PasswordLoginEnabled:              enabled,
		PasswordRegisterEnabled:           !enabled,
		EmailVerificationEnabled:          enabled,
		RegistrationEnabled:               !enabled,
		TurnstileCheckEnabled:             enabled,
		TurnstileSiteKey:                  "site-" + version,
		TurnstileSecretKey:                "secret-" + version,
		InvitationCodeEnabled:             !enabled,
		InvitationCodeOAuthRequired:       enabled,
		InvitationCodeUserGenerateEnabled: !enabled,
		EmailDomainRestrictionEnabled:     enabled,
		EmailAliasRestrictionEnabled:      !enabled,
		EmailDomainWhitelist:              []string{version + ".example", "mail-" + version + ".example"},
	}
}

func authSettingsOptionValues(settings AuthSettings) map[string]string {
	return map[string]string{
		"PasswordLoginEnabled":              strconv.FormatBool(settings.PasswordLoginEnabled),
		"PasswordRegisterEnabled":           strconv.FormatBool(settings.PasswordRegisterEnabled),
		"EmailVerificationEnabled":          strconv.FormatBool(settings.EmailVerificationEnabled),
		"RegisterEnabled":                   strconv.FormatBool(settings.RegistrationEnabled),
		"TurnstileCheckEnabled":             strconv.FormatBool(settings.TurnstileCheckEnabled),
		"TurnstileSiteKey":                  settings.TurnstileSiteKey,
		"TurnstileSecretKey":                settings.TurnstileSecretKey,
		"InvitationCodeEnabled":             strconv.FormatBool(settings.InvitationCodeEnabled),
		"InvitationCodeOAuthRequired":       strconv.FormatBool(settings.InvitationCodeOAuthRequired),
		"InvitationCodeUserGenerateEnabled": strconv.FormatBool(settings.InvitationCodeUserGenerateEnabled),
		"EmailDomainRestrictionEnabled":     strconv.FormatBool(settings.EmailDomainRestrictionEnabled),
		"EmailAliasRestrictionEnabled":      strconv.FormatBool(settings.EmailAliasRestrictionEnabled),
		"EmailDomainWhitelist":              strings.Join(settings.EmailDomainWhitelist, ","),
	}
}
