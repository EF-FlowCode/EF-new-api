package service

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"eflow.local/new-api/constant"
	host "eflow.local/new-api/relay/channel/task/jsplugin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"
)

type pluginRoundTripper func(*http.Request) (*http.Response, error)

func (f pluginRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func pluginTestAccount(t *testing.T) (string, *rsa.PrivateKey) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	raw, err := x509.MarshalPKCS8PrivateKey(key)
	require.NoError(t, err)
	account, err := json.MarshalIndent(map[string]string{"project_id": "test-project", "client_email": "test@example.test", "private_key": string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: raw}))}, "", "  ")
	require.NoError(t, err)
	return string(account), key
}

func TestTaskPluginOAuth(t *testing.T) {
	account, privateKey := pluginTestAccount(t)
	client := &http.Client{Transport: pluginRoundTripper(func(r *http.Request) (*http.Response, error) {
		require.Equal(t, "https://www.googleapis.com/oauth2/v4/token", r.URL.String())
		require.NoError(t, r.ParseForm())
		token, err := jwt.Parse(r.FormValue("assertion"), func(token *jwt.Token) (any, error) { return &privateKey.PublicKey, nil }, jwt.WithValidMethods([]string{"RS256"}), jwt.WithAudience("https://www.googleapis.com/oauth2/v4/token"))
		require.NoError(t, err)
		require.True(t, token.Valid)
		require.Equal(t, "test@example.test", token.Claims.(jwt.MapClaims)["iss"])
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"access_token":"local-token"}`))}, nil
	})}
	env, err := PrepareTaskPluginAuth(t.Context(), constant.ChannelTypeVertexAi, "https://vertex.example", account, `{"default":"us-central1"}`, host.Executor{Client: client, ValidateURL: func(string) error { return nil }})
	require.NoError(t, err)
	require.Equal(t, "Bearer local-token", env.AuthHeader)
	require.Equal(t, "test-project", env.Auth["projectId"])
	require.Equal(t, map[string]any{"default": "us-central1"}, env.UserSetting["vertexRegion"])
	for _, tc := range []struct {
		name, body string
		status     int
	}{
		{"empty", `{"access_token":" "}`, 200}, {"newline", `{"access_token":"secret\r\n"}`, 200}, {"bad_json", "secret", 200}, {"rejected", "secret", 403}, {"oversized", strings.Repeat("x", 65537), 200}, {"redirect", "", 302},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.Header().Set("Location", "/should-not-follow")
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.body)
			}))
			defer srv.Close()
			_, err := exchangeTaskPluginToken(t.Context(), srv.URL, "assertion", host.Executor{Client: srv.Client(), ValidateURL: func(string) error { return nil }})
			require.Error(t, err)
			require.NotContains(t, err.Error(), "secret")
			require.Equal(t, 1, calls)
		})
	}
}
