package controller

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"

	"eflow.local/new-api/common"
	"eflow.local/new-api/i18n"
	"eflow.local/new-api/internal/testdb"
	"eflow.local/new-api/model"
	"eflow.local/new-api/oauth"
	"eflow.local/new-api/service"
	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type oauthContractProvider struct {
	name                   string
	prefix                 string
	column                 string
	providerUser           *oauth.OAuthUser
	existingUser           *model.User
	taken                  bool
	exchangeCalls          int
	newUserValidationErr   error
	newUserValidationCalls int
}

func (p *oauthContractProvider) GetName() string { return p.name }
func (p *oauthContractProvider) IsEnabled() bool { return true }
func (p *oauthContractProvider) ExchangeToken(context.Context, string, *gin.Context) (*oauth.OAuthToken, error) {
	p.exchangeCalls++
	return &oauth.OAuthToken{AccessToken: "contract-token"}, nil
}
func (p *oauthContractProvider) GetUserInfo(context.Context, *oauth.OAuthToken) (*oauth.OAuthUser, error) {
	return p.providerUser, nil
}
func (p *oauthContractProvider) IsUserIDTaken(string) bool { return p.taken }
func (p *oauthContractProvider) FillUserByProviderID(user *model.User, _ string) error {
	if p.existingUser != nil {
		*user = *p.existingUser
	}
	return nil
}
func (p *oauthContractProvider) SetProviderUserID(user *model.User, providerUserID string) {
	setOAuthContractProviderID(user, p.column, providerUserID)
}
func (p *oauthContractProvider) GetProviderPrefix() string    { return p.prefix }
func (p *oauthContractProvider) ProviderUserIDColumn() string { return p.column }
func (p *oauthContractProvider) ValidateNewUser(context.Context, *oauth.OAuthUser) error {
	p.newUserValidationCalls++
	return p.newUserValidationErr
}

type oauthContractResponse struct {
	Success bool           `json:"success"`
	Message string         `json:"message"`
	Data    map[string]any `json:"data"`
}

func enableRegistrationForOAuthTest(t *testing.T) {
	t.Helper()
	original := common.GetAuthSettings()
	require.NoError(t, common.UpdateAuthSettings(map[string]string{"RegisterEnabled": "true"}))
	t.Cleanup(func() {
		require.NoError(t, common.UpdateAuthSettings(map[string]string{
			"RegisterEnabled": strconv.FormatBool(original.RegistrationEnabled),
		}))
	})
}

func TestUnifiedOAuthProviderContracts(t *testing.T) {
	gin.SetMode(gin.TestMode)
	require.NoError(t, i18n.Init())
	db := testdb.OpenMySQL8(t, &model.User{}, &model.TwoFA{}, &model.PasskeyCredential{})
	oldDB, oldLogDB := model.DB, model.LOG_DB
	oldRedisEnabled := common.RedisEnabled
	model.DB, model.LOG_DB = db, db
	common.RedisEnabled = false
	enableRegistrationForOAuthTest(t)
	t.Cleanup(func() {
		model.DB, model.LOG_DB = oldDB, oldLogDB
		common.RedisEnabled = oldRedisEnabled
	})

	providers := []struct {
		name   string
		prefix string
		column string
	}{
		{name: "github", prefix: "github_", column: "github_id"},
		{name: "discord", prefix: "discord_", column: "discord_id"},
		{name: "oidc", prefix: "oidc_", column: "oidc_id"},
		{name: "linuxdo", prefix: "linuxdo_", column: "linux_do_id"},
	}
	originalProviders := oauth.GetAllProviders()
	t.Cleanup(func() {
		for name, provider := range originalProviders {
			oauth.Register(name, provider)
		}
	})

	for index, providerCase := range providers {
		t.Run(providerCase.name, func(t *testing.T) {
			providerUserID := providerCase.name + "-contract-id"
			provider := &oauthContractProvider{
				name:   providerCase.name,
				prefix: providerCase.prefix,
				column: providerCase.column,
				providerUser: &oauth.OAuthUser{
					ProviderUserID: providerUserID,
				},
			}
			oauth.Register(providerCase.name, provider)

			invalidState := performOAuthContractRequest(t, providerCase.name, nil)
			assert.Equal(t, http.StatusForbidden, invalidState.Code)
			assert.False(t, decodeOAuthContractResponse(t, invalidState).Success)
			assert.Zero(t, provider.exchangeCalls)

			loginUser := &model.User{
				Username:    "oauth-login-" + providerCase.name,
				Password:    "contract-password",
				DisplayName: "OAuth Login",
				AffCode:     "oauth-login-" + providerCase.name,
				Role:        common.RoleCommonUser,
				Status:      common.UserStatusEnabled,
				Group:       "default",
			}
			require.NoError(t, db.Create(loginUser).Error)
			provider.taken = true
			provider.existingUser = loginUser
			login := performOAuthContractRequest(t, providerCase.name, map[string]any{
				"oauth_state": "contract-state",
			})
			loginResponse := decodeOAuthContractResponse(t, login)
			assert.Equal(t, http.StatusOK, login.Code)
			assert.True(t, loginResponse.Success, loginResponse.Message)
			assert.Equal(t, float64(loginUser.Id), loginResponse.Data["id"])

			bindUser := &model.User{
				Username: "oauth-bind-" + providerCase.name,
				Password: "contract-password",
				AffCode:  "oauth-bind-" + providerCase.name,
				Role:     common.RoleCommonUser,
				Status:   common.UserStatusEnabled,
				Group:    "default",
			}
			require.NoError(t, db.Create(bindUser).Error)
			provider.taken = false
			provider.existingUser = nil
			unverified := performOAuthContractRequest(t, providerCase.name, map[string]any{
				"oauth_state": "contract-state",
				"username":    bindUser.Username,
				"id":          bindUser.Id,
			})
			assert.Equal(t, http.StatusForbidden, unverified.Code, "binding needs the proof consumed when the bind started")
			bind := performOAuthContractRequest(t, providerCase.name, withOAuthBindAuthorization(t, bindUser.Id, providerCase.name, map[string]any{
				"oauth_state": "contract-state",
				"username":    bindUser.Username,
				"id":          bindUser.Id,
			}))
			bindResponse := decodeOAuthContractResponse(t, bind)
			assert.Equal(t, http.StatusOK, bind.Code)
			assert.True(t, bindResponse.Success, bindResponse.Message)
			assert.Equal(t, "bind", bindResponse.Data["action"])
			require.NoError(t, db.First(bindUser, bindUser.Id).Error)
			assert.Equal(t, providerUserID, oauthContractProviderID(bindUser, providerCase.column))

			conflictUser := &model.User{
				Username: "oauth-conflict-" + providerCase.name,
				Password: "contract-password",
				AffCode:  "oauth-conflict-" + providerCase.name,
				Role:     common.RoleCommonUser,
				Status:   common.UserStatusEnabled,
				Group:    "default",
			}
			require.NoError(t, db.Create(conflictUser).Error)
			provider.providerUser.ProviderUserID = providerUserID + "-conflict"
			provider.taken = true
			conflict := performOAuthContractRequest(t, providerCase.name, withOAuthBindAuthorization(t, conflictUser.Id, providerCase.name, map[string]any{
				"oauth_state": "contract-state",
				"username":    conflictUser.Username,
				"id":          conflictUser.Id,
			}))
			conflictResponse := decodeOAuthContractResponse(t, conflict)
			assert.Equal(t, http.StatusOK, conflict.Code)
			assert.False(t, conflictResponse.Success)
			require.NoError(t, db.First(conflictUser, conflictUser.Id).Error)
			assert.Empty(t, oauthContractProviderID(conflictUser, providerCase.column))

			assert.GreaterOrEqual(t, provider.exchangeCalls, 3, "provider %d callback path was not exercised", index)
		})
	}
}

func TestOAuthNewUserPolicyDoesNotBlockExistingLoginOrBinding(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := testdb.OpenMySQL8(t, &model.User{}, &model.TwoFA{}, &model.PasskeyCredential{})
	oldDB, oldLogDB := model.DB, model.LOG_DB
	oldRedisEnabled := common.RedisEnabled
	model.DB, model.LOG_DB = db, db
	common.RedisEnabled = false
	enableRegistrationForOAuthTest(t)
	t.Cleanup(func() {
		model.DB, model.LOG_DB = oldDB, oldLogDB
		common.RedisEnabled = oldRedisEnabled
	})

	originalProviders := oauth.GetAllProviders()
	t.Cleanup(func() {
		for name, provider := range originalProviders {
			oauth.Register(name, provider)
		}
	})
	provider := &oauthContractProvider{
		name:   "linuxdo",
		prefix: "linuxdo_",
		column: "linux_do_id",
		providerUser: &oauth.OAuthUser{
			ProviderUserID: "linuxdo-policy-id",
		},
		newUserValidationErr: &oauth.TrustLevelError{Required: 3, Current: 2},
	}
	oauth.Register("linuxdo", provider)

	existing := &model.User{
		Username:  "oauth-policy-login",
		Password:  "contract-password",
		AffCode:   "oauth-policy-login",
		Role:      common.RoleCommonUser,
		Status:    common.UserStatusEnabled,
		Group:     "default",
		LinuxDOId: provider.providerUser.ProviderUserID,
	}
	require.NoError(t, db.Create(existing).Error)
	provider.taken = true
	provider.existingUser = existing
	login := performOAuthContractRequest(t, "linuxdo", map[string]any{"oauth_state": "contract-state"})
	assert.True(t, decodeOAuthContractResponse(t, login).Success, login.Body.String())
	assert.Zero(t, provider.newUserValidationCalls)

	bindUser := &model.User{
		Username: "oauth-policy-bind",
		Password: "contract-password",
		AffCode:  "oauth-policy-bind",
		Role:     common.RoleCommonUser,
		Status:   common.UserStatusEnabled,
		Group:    "default",
	}
	require.NoError(t, db.Create(bindUser).Error)
	provider.taken = false
	provider.existingUser = nil
	provider.providerUser.ProviderUserID = "linuxdo-policy-bind-id"
	bind := performOAuthContractRequest(t, "linuxdo", withOAuthBindAuthorization(t, bindUser.Id, "linuxdo", map[string]any{
		"oauth_state": "contract-state",
		"username":    bindUser.Username,
		"id":          bindUser.Id,
	}))
	assert.True(t, decodeOAuthContractResponse(t, bind).Success, bind.Body.String())
	assert.Zero(t, provider.newUserValidationCalls)

	provider.providerUser.ProviderUserID = "linuxdo-policy-new-id"
	before := int64(0)
	require.NoError(t, db.Model(&model.User{}).Count(&before).Error)
	registration := performOAuthContractRequest(t, "linuxdo", map[string]any{"oauth_state": "contract-state"})
	registrationResponse := decodeOAuthContractResponse(t, registration)
	assert.False(t, registrationResponse.Success)
	assert.Equal(t, 1, provider.newUserValidationCalls)
	after := int64(0)
	require.NoError(t, db.Model(&model.User{}).Count(&after).Error)
	assert.Equal(t, before, after, "rejected registration created a user")
}

func performOAuthContractRequest(t *testing.T, providerName string, sessionValues map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	engine := gin.New()
	engine.Use(sessions.Sessions("session", cookie.NewStore([]byte("oauth-contract-session-secret"))))
	engine.GET("/api/oauth/:provider", func(c *gin.Context) {
		session := sessions.Default(c)
		for key, value := range sessionValues {
			session.Set(key, value)
		}
		require.NoError(t, session.Save())
		HandleOAuth(c)
	})

	query := url.Values{"state": {"contract-state"}, "code": {"contract-code"}}
	request := httptest.NewRequest(http.MethodGet, "/api/oauth/"+providerName+"?"+query.Encode(), nil)
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, request)
	return recorder
}

func decodeOAuthContractResponse(t *testing.T, recorder *httptest.ResponseRecorder) oauthContractResponse {
	t.Helper()
	var response oauthContractResponse
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response), recorder.Body.String())
	return response
}

func setOAuthContractProviderID(user *model.User, column string, value string) {
	switch column {
	case "github_id":
		user.GitHubId = value
	case "discord_id":
		user.DiscordId = value
	case "oidc_id":
		user.OidcId = value
	case "linux_do_id":
		user.LinuxDOId = value
	}
}

func oauthContractProviderID(user *model.User, column string) string {
	switch column {
	case "github_id":
		return user.GitHubId
	case "discord_id":
		return user.DiscordId
	case "oidc_id":
		return user.OidcId
	case "linux_do_id":
		return user.LinuxDOId
	default:
		return ""
	}
}

type legacyGitHubContractProvider struct {
	oauthContractProvider
}

func (p *legacyGitHubContractProvider) IsUserIDTaken(providerUserID string) bool {
	return model.IsGitHubIdAlreadyTaken(providerUserID)
}

func TestGitHubLegacyUsernameBindingCannotSignIn(t *testing.T) {
	gin.SetMode(gin.TestMode)
	require.NoError(t, i18n.Init())
	db := testdb.OpenMySQL8(t, &model.User{}, &model.TwoFA{}, &model.PasskeyCredential{})
	oldDB, oldLogDB := model.DB, model.LOG_DB
	oldRedisEnabled := common.RedisEnabled
	model.DB, model.LOG_DB = db, db
	common.RedisEnabled = false
	enableRegistrationForOAuthTest(t)
	t.Cleanup(func() {
		model.DB, model.LOG_DB = oldDB, oldLogDB
		common.RedisEnabled = oldRedisEnabled
	})
	originalProviders := oauth.GetAllProviders()
	t.Cleanup(func() {
		for name, provider := range originalProviders {
			oauth.Register(name, provider)
		}
	})

	legacy := &model.User{Username: "legacy-gh", Password: "contract-password", AffCode: "legacy-gh",
		Role: common.RoleCommonUser, Status: common.UserStatusEnabled, Group: "default", GitHubId: "alice"}
	require.NoError(t, db.Create(legacy).Error)
	provider := &legacyGitHubContractProvider{oauthContractProvider{
		name:   "github",
		prefix: "github_",
		column: "github_id",
		providerUser: &oauth.OAuthUser{
			ProviderUserID: "9001",
			Username:       "alice",
			Extra:          map[string]any{"legacy_id": "alice"},
		},
	}}
	oauth.Register("github", provider)

	recorder := performOAuthContractRequest(t, "github", map[string]any{"oauth_state": "contract-state"})
	response := decodeOAuthContractResponse(t, recorder)
	assert.False(t, response.Success, recorder.Body.String())
	assert.Contains(t, response.Message, "GitHub")
	assert.NotEqual(t, i18n.MsgOAuthLegacyBindingRebind, response.Message, "the message key must be translated")
	assert.Nil(t, response.Data, "no login may be issued")

	var users []model.User
	require.NoError(t, db.Find(&users).Error)
	require.Len(t, users, 1, "a legacy match must not register a new account")
	assert.Equal(t, "alice", users[0].GitHubId, "the legacy binding is left for manual rebinding")
}

// withOAuthBindAuthorization adds the session state GenerateOAuthCode leaves
// after consuming a password-verified account.binding.bind proof.
func withOAuthBindAuthorization(t *testing.T, userID int, provider string, values map[string]any) map[string]any {
	t.Helper()
	context, err := common.Marshal(service.AccountBindingContext{Provider: provider})
	require.NoError(t, err)
	binding, err := service.BindVerificationOperation(service.VerificationOperation{Scope: service.VerificationScopeAccountBind, Context: context})
	require.NoError(t, err)
	encoded, err := common.Marshal(oauthBindAuthorization{Provider: provider, Authorization: service.StepUpAuthorization{
		UserID: userID, SessionID: "nonce:oauth-contract", ProofID: "contract-proof",
		Scope: binding.Scope, ContextHash: binding.ContextHash, Method: service.VerificationMethodPassword,
	}})
	require.NoError(t, err)
	values["step_up_nonce"] = "oauth-contract"
	values[oauthBindAuthorizationKey] = string(encoded)
	return values
}
