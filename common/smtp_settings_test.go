package common

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func preserveSMTPSettings(t *testing.T) {
	t.Helper()
	original := GetSMTPSettings()
	t.Cleanup(func() {
		smtpSettingsMu.Lock()
		defer smtpSettingsMu.Unlock()
		publishSMTPSettings(original)
	})
}

func TestUpdateSMTPSettingsRejectsInvalidBatchWithoutPublishing(t *testing.T) {
	preserveSMTPSettings(t)
	require.NoError(t, UpdateSMTPSettings(map[string]string{
		"SMTPServer":     "smtp.original.example",
		"SMTPPort":       "587",
		"SMTPSSLEnabled": "false",
		"SMTPAccount":    "sender@original.example",
	}))
	before := GetSMTPSettings()

	err := UpdateSMTPSettings(map[string]string{
		"SMTPServer": "smtp.unpublished.example",
		"SMTPPort":   "65536",
	})

	require.ErrorContains(t, err, "SMTPPort")
	assert.Equal(t, before, GetSMTPSettings())
}

func TestUpdateSMTPSettingsIgnoresUnknownKeys(t *testing.T) {
	preserveSMTPSettings(t)
	before := GetSMTPSettings()

	require.NoError(t, UpdateSMTPSettings(map[string]string{"UnrelatedOption": "ignored"}))
	assert.Equal(t, before, GetSMTPSettings())
}

func TestSMTPSettingsConcurrentReadersObserveCompleteVersions(t *testing.T) {
	preserveSMTPSettings(t)
	versionA := map[string]string{
		"SMTPServer":             "smtp.a.example",
		"SMTPPort":               "465",
		"SMTPSSLEnabled":         "true",
		"SMTPStartTLSEnabled":    "false",
		"SMTPInsecureSkipVerify": "false",
		"SMTPForceAuthLogin":     "true",
		"SMTPAccount":            "sender@a.example",
		"SMTPFrom":               "from@a.example",
		"SMTPToken":              "token-a",
		"SMTPArchiveBcc":         "archive@a.example",
	}
	versionB := map[string]string{
		"SMTPServer":             "smtp.b.example",
		"SMTPPort":               "2525",
		"SMTPSSLEnabled":         "false",
		"SMTPStartTLSEnabled":    "true",
		"SMTPInsecureSkipVerify": "true",
		"SMTPForceAuthLogin":     "false",
		"SMTPAccount":            "sender@b.example",
		"SMTPFrom":               "from@b.example",
		"SMTPToken":              "token-b",
		"SMTPArchiveBcc":         "archive@b.example",
	}
	require.NoError(t, UpdateSMTPSettings(versionA))
	wantA := GetSMTPSettings()
	require.NoError(t, UpdateSMTPSettings(versionB))
	wantB := GetSMTPSettings()
	require.NoError(t, UpdateSMTPSettings(versionA))

	invalid := make(chan SMTPSettings, 1)
	var writers sync.WaitGroup
	for _, values := range []map[string]string{versionA, versionB} {
		writers.Add(1)
		go func() {
			defer writers.Done()
			for range 2000 {
				if err := UpdateSMTPSettings(values); err != nil {
					return
				}
			}
		}()
	}

	for range 10000 {
		got := GetSMTPSettings()
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
		t.Fatalf("reader observed mixed SMTP settings: %#v", got)
	default:
	}
}

func TestSendEmailDoesNotMutateSettingsForFallbackFrom(t *testing.T) {
	preserveSMTPSettings(t)
	require.NoError(t, UpdateSMTPSettings(map[string]string{
		"SMTPServer":  "127.0.0.1",
		"SMTPPort":    "1",
		"SMTPAccount": "sender@example.com",
		"SMTPFrom":    "",
	}))
	before := GetSMTPSettings()

	_ = SendEmail("subject", "receiver@example.com", "content")

	assert.Equal(t, before, GetSMTPSettings())
}
