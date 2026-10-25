package oauth

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"eflow.local/new-api/common"
	"eflow.local/new-api/model"
	"eflow.local/new-api/setting/system_setting"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

// eflow: Telegram 统一 OAuth（上游 3e84ec0ab 意图）——OpenID Connect + PKCE，ID Token 用
// oauth.telegram.org 的 JWKS 校验。为不引入 go-oidc / x/oauth2 依赖，PKCE 与 JWKS 校验在此
// 直接实现（基于已有的 golang-jwt）。配置了 Client ID/Secret 后旧的登录小部件停用。

const (
	TelegramIssuer = "https://oauth.telegram.org"
	// TelegramOAuthFlowContextKey carries the PKCE flow from the session to ExchangeToken.
	TelegramOAuthFlowContextKey = "telegram_oauth_flow"
	telegramJWKSRefresh         = time.Hour
)

var (
	ErrTelegramOAuthNotConfigured = errors.New("Telegram OAuth is not configured or enabled. Please contact your administrator.")
	ErrTelegramOAuthFailed        = errors.New("Telegram authorization failed. Please try again.")
)

// TelegramOAuthFlow keeps the PKCE secret and client configuration on the
// server; only the authorization URL is sent to the browser.
type TelegramOAuthFlow struct {
	CodeVerifier string `json:"code_verifier"`
	ClientID     string `json:"client_id"`
	RedirectURI  string `json:"redirect_uri"`
}

func telegramRedirectURI() string {
	return strings.TrimRight(system_setting.GetServerAddress(), "/") + "/oauth/telegram"
}

// NewTelegramOAuthFlow starts a PKCE flow for the configured client.
func NewTelegramOAuthFlow() (*TelegramOAuthFlow, error) {
	settings := system_setting.GetLegacyOAuthSettings()
	if !settings.TelegramOIDCEnabled() {
		return nil, ErrTelegramOAuthNotConfigured
	}
	redirectURI := telegramRedirectURI()
	parsed, err := url.Parse(redirectURI)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "https" && parsed.Scheme != "http") {
		return nil, ErrTelegramOAuthNotConfigured
	}
	verifier := make([]byte, 32)
	if _, err := rand.Read(verifier); err != nil {
		return nil, err
	}
	return &TelegramOAuthFlow{
		CodeVerifier: base64.RawURLEncoding.EncodeToString(verifier),
		ClientID:     strings.TrimSpace(settings.TelegramClientId),
		RedirectURI:  redirectURI,
	}, nil
}

func (flow *TelegramOAuthFlow) AuthorizationURL(state string) string {
	challenge := sha256.Sum256([]byte(flow.CodeVerifier))
	values := url.Values{
		"client_id":             {flow.ClientID},
		"redirect_uri":          {flow.RedirectURI},
		"response_type":         {"code"},
		"scope":                 {"openid profile"},
		"state":                 {state},
		"code_challenge":        {base64.RawURLEncoding.EncodeToString(challenge[:])},
		"code_challenge_method": {"S256"},
	}
	return TelegramIssuer + "/auth?" + values.Encode()
}

type TelegramProvider struct {
	client  *http.Client
	issuer  string
	mu      sync.Mutex
	keys    map[string]any
	fetched time.Time
}

func init() {
	Register("telegram", NewTelegramProvider(&http.Client{Timeout: 20 * time.Second}, TelegramIssuer))
}

// NewTelegramProvider builds the provider against issuer (tests use a local server).
func NewTelegramProvider(client *http.Client, issuer string) *TelegramProvider {
	return &TelegramProvider{client: client, issuer: issuer}
}

func (p *TelegramProvider) GetName() string { return "Telegram" }

func (p *TelegramProvider) IsEnabled() bool {
	return system_setting.GetLegacyOAuthSettings().TelegramOIDCEnabled()
}

func (p *TelegramProvider) ExchangeToken(ctx context.Context, code string, c *gin.Context) (*OAuthToken, error) {
	settings := system_setting.GetLegacyOAuthSettings()
	if !settings.TelegramOIDCEnabled() {
		return nil, ErrTelegramOAuthNotConfigured
	}
	value, _ := c.Get(TelegramOAuthFlowContextKey)
	flow, ok := value.(*TelegramOAuthFlow)
	if !ok || flow == nil || flow.CodeVerifier == "" || code == "" ||
		flow.ClientID != strings.TrimSpace(settings.TelegramClientId) || flow.RedirectURI != telegramRedirectURI() {
		return nil, ErrTelegramOAuthFailed
	}
	values := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"client_id":     {flow.ClientID},
		"redirect_uri":  {flow.RedirectURI},
		"code_verifier": {flow.CodeVerifier},
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, p.issuer+"/token", strings.NewReader(values.Encode()))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrTelegramOAuthFailed, err)
	}
	request.SetBasicAuth(flow.ClientID, strings.TrimSpace(settings.TelegramClientSecret))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err := p.client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrTelegramOAuthFailed, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: token endpoint status %d", ErrTelegramOAuthFailed, response.StatusCode)
	}
	var token OAuthToken
	if err := common.DecodeJson(io.LimitReader(response.Body, 1<<20), &token); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrTelegramOAuthFailed, err)
	}
	if token.IDToken == "" {
		return nil, ErrTelegramOAuthFailed
	}
	return &token, nil
}

type telegramIDClaims struct {
	ID       json.Number `json:"id"`
	Name     string      `json:"name"`
	Username string      `json:"preferred_username"`
	jwt.RegisteredClaims
}

func (p *TelegramProvider) GetUserInfo(ctx context.Context, token *OAuthToken) (*OAuthUser, error) {
	settings := system_setting.GetLegacyOAuthSettings()
	if !settings.TelegramOIDCEnabled() {
		return nil, ErrTelegramOAuthNotConfigured
	}
	if token == nil || token.IDToken == "" {
		return nil, ErrTelegramOAuthFailed
	}
	claims := &telegramIDClaims{}
	_, err := jwt.ParseWithClaims(token.IDToken, claims, func(t *jwt.Token) (any, error) {
		kid, _ := t.Header["kid"].(string)
		return p.key(ctx, kid)
	}, jwt.WithValidMethods([]string{"RS256", "ES256"}), jwt.WithIssuer(p.issuer),
		jwt.WithAudience(strings.TrimSpace(settings.TelegramClientId)), jwt.WithExpirationRequired(), jwt.WithLeeway(30*time.Second))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrTelegramOAuthFailed, err)
	}
	id, err := strconv.ParseUint(claims.ID.String(), 10, 64)
	if err != nil || id == 0 || claims.Subject == "" {
		return nil, ErrTelegramOAuthFailed
	}
	return &OAuthUser{ProviderUserID: strconv.FormatUint(id, 10), Username: claims.Username, DisplayName: claims.Name}, nil
}

// key returns the JWKS public key for kid, refreshing the cached set hourly or
// when an unknown kid appears.
func (p *TelegramProvider) key(ctx context.Context, kid string) (any, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if key, ok := p.keys[kid]; ok && time.Since(p.fetched) < telegramJWKSRefresh {
		return key, nil
	}
	keys, err := p.fetchKeys(ctx)
	if err != nil {
		return nil, err
	}
	p.keys, p.fetched = keys, time.Now()
	if key, ok := keys[kid]; ok {
		return key, nil
	}
	return nil, errors.New("unknown signing key")
}

func (p *TelegramProvider) fetchKeys(ctx context.Context) (map[string]any, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, p.issuer+"/.well-known/jwks.json", nil)
	if err != nil {
		return nil, err
	}
	response, err := p.client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("jwks status %d", response.StatusCode)
	}
	var set struct {
		Keys []struct {
			Kid string `json:"kid"`
			Kty string `json:"kty"`
			Crv string `json:"crv"`
			N   string `json:"n"`
			E   string `json:"e"`
			X   string `json:"x"`
			Y   string `json:"y"`
		} `json:"keys"`
	}
	if err := common.DecodeJson(io.LimitReader(response.Body, 1<<20), &set); err != nil {
		return nil, err
	}
	keys := map[string]any{}
	for _, jwk := range set.Keys {
		switch jwk.Kty {
		case "RSA":
			n, errN := base64.RawURLEncoding.DecodeString(jwk.N)
			e, errE := base64.RawURLEncoding.DecodeString(jwk.E)
			if errN != nil || errE != nil || len(e) == 0 {
				continue
			}
			keys[jwk.Kid] = &rsa.PublicKey{N: new(big.Int).SetBytes(n), E: int(new(big.Int).SetBytes(e).Int64())}
		case "EC":
			if jwk.Crv != "P-256" {
				continue
			}
			x, errX := base64.RawURLEncoding.DecodeString(jwk.X)
			y, errY := base64.RawURLEncoding.DecodeString(jwk.Y)
			if errX != nil || errY != nil {
				continue
			}
			keys[jwk.Kid] = &ecdsa.PublicKey{Curve: elliptic.P256(), X: new(big.Int).SetBytes(x), Y: new(big.Int).SetBytes(y)}
		}
	}
	return keys, nil
}

func (p *TelegramProvider) IsUserIDTaken(id string) bool { return model.IsTelegramIdAlreadyTaken(id) }

func (p *TelegramProvider) FillUserByProviderID(user *model.User, id string) error {
	user.TelegramId = id
	return user.FillUserByTelegramId()
}

func (p *TelegramProvider) SetProviderUserID(user *model.User, id string) { user.TelegramId = id }

func (p *TelegramProvider) GetProviderPrefix() string { return "telegram_" }

func (p *TelegramProvider) ProviderUserIDColumn() string { return "telegram_id" }
