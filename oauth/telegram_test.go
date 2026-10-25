package oauth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"eflow.local/new-api/setting/system_setting"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"
)

func TestTelegramOpenIDConnectFlow(t *testing.T) {
	gin.SetMode(gin.TestMode)
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	var issuer string
	var gotVerifier string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/jwks.json":
			_ = json.NewEncoder(w).Encode(map[string]any{"keys": []map[string]string{{
				"kid": "k1", "kty": "RSA",
				"n": base64.RawURLEncoding.EncodeToString(key.N.Bytes()),
				"e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes()),
			}}})
		case "/token":
			require.NoError(t, r.ParseForm())
			user, pass, ok := r.BasicAuth()
			require.True(t, ok)
			require.Equal(t, "client-1", user)
			require.Equal(t, "secret-1", pass)
			gotVerifier = r.PostForm.Get("code_verifier")
			token := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{
				"iss": issuer, "aud": "client-1", "sub": "s-1", "id": 123456789,
				"name": "Tg User", "preferred_username": "tguser",
				"exp": time.Now().Add(time.Minute).Unix(), "iat": time.Now().Unix(),
			})
			token.Header["kid"] = "k1"
			signed, err := token.SignedString(key)
			require.NoError(t, err)
			_ = json.NewEncoder(w).Encode(map[string]string{"access_token": "a", "token_type": "Bearer", "id_token": signed})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	issuer = server.URL

	previous := system_setting.GetLegacyOAuthSettings()
	require.NoError(t, system_setting.UpdateLegacyOAuthSettings(map[string]string{
		"TelegramOAuthEnabled": "true", "TelegramClientId": "client-1", "TelegramClientSecret": "secret-1",
	}))
	t.Cleanup(func() {
		enabled := "false"
		if previous.TelegramOAuthEnabled {
			enabled = "true"
		}
		_ = system_setting.UpdateLegacyOAuthSettings(map[string]string{
			"TelegramOAuthEnabled": enabled, "TelegramClientId": previous.TelegramClientId, "TelegramClientSecret": previous.TelegramClientSecret,
		})
	})

	flow, err := NewTelegramOAuthFlow()
	require.NoError(t, err)
	authURL, err := url.Parse(flow.AuthorizationURL("state-1"))
	require.NoError(t, err)
	challenge := sha256.Sum256([]byte(flow.CodeVerifier))
	require.Equal(t, base64.RawURLEncoding.EncodeToString(challenge[:]), authURL.Query().Get("code_challenge"))
	require.Equal(t, "S256", authURL.Query().Get("code_challenge_method"))
	require.Equal(t, "state-1", authURL.Query().Get("state"))

	provider := NewTelegramProvider(server.Client(), issuer)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	_, err = provider.ExchangeToken(context.Background(), "code-1", c)
	require.ErrorIs(t, err, ErrTelegramOAuthFailed, "a callback without the session's PKCE flow is rejected")

	c.Set(TelegramOAuthFlowContextKey, flow)
	token, err := provider.ExchangeToken(context.Background(), "code-1", c)
	require.NoError(t, err)
	require.Equal(t, flow.CodeVerifier, gotVerifier)
	user, err := provider.GetUserInfo(context.Background(), token)
	require.NoError(t, err)
	require.Equal(t, "123456789", user.ProviderUserID)
	require.Equal(t, "tguser", user.Username)

	// An ID token for another client is rejected.
	other := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{"iss": issuer, "aud": "client-2", "sub": "s", "id": 1, "exp": time.Now().Add(time.Minute).Unix()})
	other.Header["kid"] = "k1"
	signed, err := other.SignedString(key)
	require.NoError(t, err)
	_, err = provider.GetUserInfo(context.Background(), &OAuthToken{IDToken: signed})
	require.ErrorIs(t, err, ErrTelegramOAuthFailed)
}
