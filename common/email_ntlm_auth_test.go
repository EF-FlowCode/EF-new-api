package common

import (
	"net/smtp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAutoSMTPAuthPreservesPlainAuthTLSGuard(t *testing.T) {
	_, _, err := AutoSMTPAuth("sender@example.com", "secret", "smtp.example.com", false).Start(&smtp.ServerInfo{
		Name: "smtp.example.com",
		TLS:  false,
		Auth: []string{"PLAIN"},
	})

	require.ErrorContains(t, err, "unencrypted connection")
}

func TestAutoSMTPAuthPrefersAdvertisedPlainOverLogin(t *testing.T) {
	mechanism, initialResponse, err := AutoSMTPAuth("sender@example.com", "secret", "smtp.example.com", false).Start(&smtp.ServerInfo{
		Name: "smtp.example.com",
		TLS:  true,
		Auth: []string{"PLAIN", "LOGIN"},
	})

	require.NoError(t, err)
	require.Equal(t, "PLAIN", mechanism)
	require.Equal(t, []byte("\x00sender@example.com\x00secret"), initialResponse)
}

func TestAutoSMTPAuthSupportsNTLMOnlyServer(t *testing.T) {
	mechanism, initialResponse, err := AutoSMTPAuth("sender@example.com", "secret", "exchange.example.com", false).Start(&smtp.ServerInfo{
		Name: "exchange.example.com",
		TLS:  true,
		Auth: []string{"NTLM"},
	})

	require.NoError(t, err)
	require.Equal(t, "NTLM", mechanism)
	require.NotEmpty(t, initialResponse)
}

func TestShouldAuthenticateSMTPRequiresCompleteCredentials(t *testing.T) {
	settings := SMTPSettings{Account: "sender@example.com"}
	require.False(t, shouldAuthenticateSMTP(settings))

	settings.Token = "secret"
	require.True(t, shouldAuthenticateSMTP(settings))
}

func TestAutoSMTPAuthCapturesServerAndForceLogin(t *testing.T) {
	auth := AutoSMTPAuth("sender@example.com", "secret", "smtp.original.example", true)
	captured, ok := auth.(*smtpAutoAuth)
	require.True(t, ok)
	assert.Equal(t, "smtp.original.example", captured.server)
	assert.True(t, captured.forceAuthLogin)

	mechanism, _, err := auth.Start(&smtp.ServerInfo{
		Name: "smtp.original.example",
		TLS:  true,
		Auth: []string{"PLAIN"},
	})
	require.NoError(t, err)
	assert.Equal(t, "LOGIN", mechanism)
}
