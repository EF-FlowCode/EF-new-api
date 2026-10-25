package common

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"eflow.local/new-api/constant"
	"github.com/gin-gonic/gin"
)

// AuthSettings is one immutable runtime version of the user authentication
// and registration gates. Request paths should retain one returned value for
// the whole operation so a bulk option publish cannot mix old and new fields.
type AuthSettings struct {
	PasswordLoginEnabled              bool
	PasswordRegisterEnabled           bool
	EmailVerificationEnabled          bool
	RegistrationEnabled               bool
	TurnstileCheckEnabled             bool
	TurnstileSiteKey                  string
	TurnstileSecretKey                string
	InvitationCodeEnabled             bool
	InvitationCodeOAuthRequired       bool
	InvitationCodeUserGenerateEnabled bool
	EmailDomainRestrictionEnabled     bool
	EmailAliasRestrictionEnabled      bool
	EmailDomainWhitelist              []string
}

var (
	authSettings   atomic.Pointer[AuthSettings]
	authSettingsMu sync.Mutex
)

func init() {
	publishAuthSettings(AuthSettings{
		PasswordLoginEnabled:    true,
		PasswordRegisterEnabled: true,
		RegistrationEnabled:     true,
		EmailDomainWhitelist: []string{
			"gmail.com",
			"163.com",
			"126.com",
			"qq.com",
			"outlook.com",
			"hotmail.com",
			"icloud.com",
			"yahoo.com",
			"foxmail.com",
		},
	})
}

// GetAuthSettings returns an isolated copy of one published runtime version.
func GetAuthSettings() AuthSettings {
	current := authSettings.Load()
	if current == nil {
		return AuthSettings{}
	}
	return cloneAuthSettings(*current)
}

// SetRequestAuthSettings pins one authentication policy version to a request.
func SetRequestAuthSettings(c *gin.Context, settings AuthSettings) {
	if c == nil {
		return
	}
	SetContextKey(c, constant.ContextKeyAuthSettings, cloneAuthSettings(settings))
}

// GetRequestAuthSettings returns the version pinned by authentication
// middleware, or the current runtime version for direct handler invocations.
func GetRequestAuthSettings(c *gin.Context) AuthSettings {
	if c != nil {
		if settings, ok := GetContextKeyType[AuthSettings](c, constant.ContextKeyAuthSettings); ok {
			return cloneAuthSettings(settings)
		}
	}
	return GetAuthSettings()
}

// UpdateAuthSettings merges supplied legacy option keys and atomically
// publishes one complete replacement. Unknown keys are ignored.
func UpdateAuthSettings(values map[string]string) error {
	authSettingsMu.Lock()
	defer authSettingsMu.Unlock()

	next := GetAuthSettings()
	boolOptions := []struct {
		key    string
		target *bool
	}{
		{key: "PasswordLoginEnabled", target: &next.PasswordLoginEnabled},
		{key: "PasswordRegisterEnabled", target: &next.PasswordRegisterEnabled},
		{key: "EmailVerificationEnabled", target: &next.EmailVerificationEnabled},
		{key: "RegisterEnabled", target: &next.RegistrationEnabled},
		{key: "TurnstileCheckEnabled", target: &next.TurnstileCheckEnabled},
		{key: "InvitationCodeEnabled", target: &next.InvitationCodeEnabled},
		{key: "InvitationCodeOAuthRequired", target: &next.InvitationCodeOAuthRequired},
		{key: "InvitationCodeUserGenerateEnabled", target: &next.InvitationCodeUserGenerateEnabled},
		{key: "EmailDomainRestrictionEnabled", target: &next.EmailDomainRestrictionEnabled},
		{key: "EmailAliasRestrictionEnabled", target: &next.EmailAliasRestrictionEnabled},
	}
	for _, option := range boolOptions {
		value, ok := values[option.key]
		if !ok {
			continue
		}
		parsed, err := strconv.ParseBool(value)
		if err != nil {
			return fmt.Errorf("%s must be a boolean: %w", option.key, err)
		}
		*option.target = parsed
	}
	if value, ok := values["TurnstileSiteKey"]; ok {
		next.TurnstileSiteKey = value
	}
	if value, ok := values["TurnstileSecretKey"]; ok {
		next.TurnstileSecretKey = value
	}
	if value, ok := values["EmailDomainWhitelist"]; ok {
		next.EmailDomainWhitelist = strings.Split(value, ",")
	}

	publishAuthSettings(next)
	return nil
}

func cloneAuthSettings(settings AuthSettings) AuthSettings {
	settings.EmailDomainWhitelist = append([]string(nil), settings.EmailDomainWhitelist...)
	return settings
}

func publishAuthSettings(next AuthSettings) {
	owned := cloneAuthSettings(next)
	authSettings.Store(&owned)
}
