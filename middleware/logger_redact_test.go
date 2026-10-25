package middleware

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRedactLogPathMasksSensitiveQueryValues(t *testing.T) {
	cases := map[string]string{
		"/api/oauth/github?code=abc123&state=xyz":         "/api/oauth/github?code=***&state=***",
		"/v1beta/models/gemini:generateContent?key=AIzaX": "/v1beta/models/gemini:generateContent?key=***",
		"/api/verification?email=a%40b.com&turnstile=t":   "/api/verification?email=***&turnstile=***",
		"/api/log/self?p=1&page_size=10":                  "/api/log/self?p=1&page_size=10",
		"/api/oauth/telegram/login?id=1&HASH=deadbeef":    "/api/oauth/telegram/login?id=1&HASH=***",
		"/api/status": "/api/status",
		"/api/x?code": "/api/x?code",
	}
	for in, want := range cases {
		require.Equal(t, want, redactLogPath(in), in)
	}
}
