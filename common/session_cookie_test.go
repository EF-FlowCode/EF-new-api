package common

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestInitSessionCookieSettings(t *testing.T) {
	tests := []struct {
		name        string
		secure      string
		trustedURLs string
		wantSecure  bool
		wantURLs    []string
		wantErr     string
	}{
		{name: "defaults to insecure"},
		{
			name:        "accepts HTTPS trusted URLs",
			secure:      "true",
			trustedURLs: "https://api.example.com, https://admin.example.com",
			wantSecure:  true,
			wantURLs:    []string{"https://api.example.com", "https://admin.example.com"},
		},
		{name: "requires trusted URL", secure: "true", wantErr: "requires SESSION_COOKIE_TRUSTED_URL"},
		{name: "rejects trusted URL without secure cookie", trustedURLs: "https://api.example.com", wantErr: "requires SESSION_COOKIE_SECURE=true"},
		{name: "rejects HTTP trusted URL", secure: "true", trustedURLs: "http://api.example.com", wantErr: "only https URLs"},
		{name: "rejects invalid boolean", secure: "yes", wantErr: "must be true or false"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("SESSION_COOKIE_SECURE", tt.secure)
			t.Setenv("SESSION_COOKIE_TRUSTED_URL", tt.trustedURLs)

			err := InitSessionCookieSettings()
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.wantSecure, SessionCookieSecure)
			require.Equal(t, tt.wantURLs, SessionCookieTrustedURLs)
		})
	}
}
