package middleware

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"eflow.local/new-api/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestTurnstileCheckPinsAuthSettingsForDownstreamRegistrationAndVerification(t *testing.T) {
	original := common.GetAuthSettings()
	t.Cleanup(func() {
		require.NoError(t, common.UpdateAuthSettings(authSettingsValues(original)))
	})

	before := original
	before.RegistrationEnabled = false
	before.PasswordRegisterEnabled = true
	before.TurnstileCheckEnabled = false
	before.EmailDomainRestrictionEnabled = true
	before.EmailDomainWhitelist = []string{"before.example"}

	after := before
	after.RegistrationEnabled = true
	after.TurnstileCheckEnabled = true
	after.TurnstileSecretKey = "after-secret"
	after.EmailDomainRestrictionEnabled = false
	after.EmailDomainWhitelist = []string{"after.example"}

	require.NoError(t, common.UpdateAuthSettings(authSettingsValues(before)))

	var downstream common.AuthSettings
	router := gin.New()
	router.GET("/", TurnstileCheck(), func(c *gin.Context) {
		require.NoError(t, common.UpdateAuthSettings(authSettingsValues(after)))
		downstream = common.GetRequestAuthSettings(c)
		c.Status(http.StatusNoContent)
	})

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	router.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusNoContent, recorder.Code)
	require.Equal(t, before, downstream)
	require.False(t, downstream.RegistrationEnabled, "registration must not switch to the post-Turnstile version")
	require.True(t, downstream.EmailDomainRestrictionEnabled, "verification must retain the pre-Turnstile email policy")
}

func authSettingsValues(settings common.AuthSettings) map[string]string {
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
