package service

import (
	"crypto/hmac"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"time"

	"eflow.local/new-api/common"
	"eflow.local/new-api/model"
	"eflow.local/new-api/setting/system_setting"
)

// eflow: 二次验证的 scope、上下文规范化与方式策略照抄上游 QuantumNous/new-api
// service/security_verification.go（scope 字符串与上下文结构一致），差异：
//   - 没有任何验证手段（2FA/Passkey/密码）的账号，在浏览器会话里刚登录 10 分钟内可用
//     "session" 方式（沿用 eflow 原有"刚登录过"规则）；访问令牌不能用该方式；
//   - 没有 auth_flows 表，一次性记录见 security_proof.go。

const (
	VerificationMethodTwoFA              = "2fa"
	VerificationMethodPasskey            = "passkey"
	VerificationMethodPassword           = "password"
	VerificationMethodOAuth              = "oauth"
	VerificationMethodSession            = "session"
	VerificationScopeChannelKeyRead      = "channel.key.read"
	VerificationScopePasskeyRegister     = "passkey.register"
	VerificationScopePasskeyDelete       = "passkey.delete"
	VerificationScopeTwoFASetup          = "2fa.setup"
	VerificationScopeTwoFADisable        = "2fa.disable"
	VerificationScopeTwoFABackupCodes    = "2fa.backup_codes.regenerate"
	VerificationScopeLogin               = "auth.login"
	VerificationScopeAccessTokenGenerate = "access_token.generate"
	VerificationScopeAccessTokenRevoke   = "access_token.revoke"
	VerificationScopeAccessTokenUpdate   = "access_token.update"
	VerificationScopeAccountBind         = "account.binding.bind"
	VerificationScopeAccountUnbind       = "account.binding.unbind"
	VerificationScopePasswordSet         = "account.password.set"
	VerificationScopePasswordChange      = "account.password.change"
	VerificationScopeAccountDelete       = "account.delete"
	// Administrative step-up scopes bind the managed user into the proof so a
	// proof issued for one account cannot be replayed against another.
	VerificationScopeAdminUserCreate       = "admin.user.create"
	VerificationScopeAdminUserUpdate       = "admin.user.update"
	VerificationScopeAdminUserDelete       = "admin.user.delete"
	VerificationScopeAdminUserManage       = "admin.user.manage"
	VerificationScopeAdminUserPasskeyReset = "admin.user.passkey.reset"
	VerificationScopeAdminUserTwoFADisable = "admin.user.2fa.disable"
	VerificationScopeAdminUserBindingClear = "admin.user.binding.clear"
	verificationScopeAdminUserPrefix       = "admin.user."

	// RecentLoginWindow lets accounts without any step-up factor confirm a
	// sensitive action with the browser sign-in they have just completed.
	RecentLoginWindow = 10 * time.Minute
)

// adminUserManageActions are the ManageUser actions that change a user's
// status or role. Quota adjustments are not gated and deletion has its own
// scope. eflow adds set_role (direct role assignment incl. support/finance).
var adminUserManageActions = []string{"disable", "enable", "promote", "demote", "set_role"}

var (
	ErrVerificationFailed         = errors.New("Verification failed. Please try again.")
	ErrVerificationUnavailable    = errors.New("This verification method is currently unavailable.")
	ErrVerificationLocked         = errors.New("Two-factor authentication is temporarily locked.")
	ErrVerificationFlowRequired   = errors.New("This verification method requires its dedicated verification flow.")
	ErrVerificationContextInvalid = errors.New("The action details are invalid.")
	ErrVerificationForbidden      = errors.New("You do not have permission to perform this action.")
	ErrVerificationReauthRequired = errors.New("For your security, sign out and sign in again, then retry within 10 minutes.")
)

// VerificationOperation describes the action being authorized, independently of
// its authentication method. Each scope owns a strict, normalized context schema.
type VerificationOperation struct {
	Scope   string          `json:"scope"`
	Context json.RawMessage `json:"context,omitempty"`
}

type ChannelKeyReadContext struct {
	ChannelID int `json:"channel_id"`
}

type AccountBindingContext struct {
	Provider string `json:"provider"`
	Email    string `json:"email,omitempty"`
	Code     string `json:"code,omitempty"`
}

type AccountUnbindingContext struct {
	ProviderID int `json:"provider_id"`
}

type AdminUserContext struct {
	UserID int `json:"user_id"`
}

// AdminUserManageContext names the managed user and action. Role is set only
// for eflow's set_role action and binds the exact target role.
type AdminUserManageContext struct {
	UserID int    `json:"user_id"`
	Action string `json:"action"`
	Role   *int   `json:"role,omitempty"`
}

// AdminUserBindingContext names exactly one binding: a built-in binding type
// or a custom OAuth provider ID.
type AdminUserBindingContext struct {
	UserID      int    `json:"user_id"`
	BindingType string `json:"binding_type,omitempty"`
	ProviderID  int    `json:"provider_id,omitempty"`
}

type AdminUserCreateContext struct {
	Role int `json:"role"`
}

// AccessTokenGenerateContext binds a creation proof to the exact grant. Scopes
// are compared as a sorted, de-duplicated set.
type AccessTokenGenerateContext struct {
	Scopes    []string `json:"scopes"`
	ExpiresAt int64    `json:"expires_at"`
}

// AccessTokenUpdateContext binds a grant change to one token and the exact new grant.
type AccessTokenUpdateContext struct {
	TokenID int      `json:"token_id"`
	Scopes  []string `json:"scopes"`
}

// AccessTokenRevokeContext names exactly one token: a scoped token ID or the legacy token.
type AccessTokenRevokeContext struct {
	TokenID int  `json:"token_id,omitempty"`
	Legacy  bool `json:"legacy,omitempty"`
}

const (
	maxAccessTokenContextScopes   = 128
	maxAccessTokenContextScopeLen = 64
)

// NormalizeAccessTokenScopeList trims, de-duplicates and sorts scope keys. It
// only checks the shape; grantability is checked against the scope catalog.
func NormalizeAccessTokenScopeList(scopes []string) ([]string, bool) {
	if len(scopes) == 0 || len(scopes) > maxAccessTokenContextScopes {
		return nil, false
	}
	normalized := make([]string, 0, len(scopes))
	for _, scope := range scopes {
		scope = strings.TrimSpace(scope)
		if scope == "" || len(scope) > maxAccessTokenContextScopeLen {
			return nil, false
		}
		normalized = append(normalized, scope)
	}
	slices.Sort(normalized)
	return slices.Compact(normalized), true
}

// VerificationBinding contains no original operation parameters. It can safely
// travel through a signed proof or a server-owned interactive verification flow.
type VerificationBinding struct {
	Scope       string `json:"scope"`
	ContextHash string `json:"context_hash"`
}

func BindVerificationOperation(operation VerificationOperation) (VerificationBinding, error) {
	var fields map[string]json.RawMessage
	if len(operation.Context) > 0 && string(operation.Context) != "null" {
		if common.GetJsonType(operation.Context) != "object" || common.Unmarshal(operation.Context, &fields) != nil {
			return VerificationBinding{}, ErrVerificationContextInvalid
		}
	}
	var normalized any
	switch operation.Scope {
	case VerificationScopeChannelKeyRead:
		var context ChannelKeyReadContext
		if len(fields) != 1 || common.Unmarshal(fields["channel_id"], &context.ChannelID) != nil || context.ChannelID <= 0 {
			return VerificationBinding{}, ErrVerificationContextInvalid
		}
		normalized = context
	case VerificationScopeAccountBind:
		var context AccountBindingContext
		if common.Unmarshal(operation.Context, &context) != nil {
			return VerificationBinding{}, ErrVerificationContextInvalid
		}
		context.Provider = strings.TrimSpace(context.Provider)
		switch context.Provider {
		case "email":
			context.Email = model.NormalizeEmail(context.Email)
			if len(fields) != 2 || common.Validate.Var(context.Email, "required,email") != nil || context.Code != "" {
				return VerificationBinding{}, ErrVerificationContextInvalid
			}
		case "wechat":
			context.Code = strings.TrimSpace(context.Code)
			if len(fields) != 2 || context.Code == "" || len(context.Code) > 128 || context.Email != "" {
				return VerificationBinding{}, ErrVerificationContextInvalid
			}
		default:
			if len(fields) != 1 || context.Provider == "" || len(context.Provider) > 64 {
				return VerificationBinding{}, ErrVerificationContextInvalid
			}
		}
		normalized = context
	case VerificationScopeAccountUnbind:
		var context AccountUnbindingContext
		if len(fields) != 1 || common.Unmarshal(fields["provider_id"], &context.ProviderID) != nil || context.ProviderID <= 0 {
			return VerificationBinding{}, ErrVerificationContextInvalid
		}
		normalized = context
	case VerificationScopeAdminUserUpdate, VerificationScopeAdminUserDelete, VerificationScopeAdminUserPasskeyReset, VerificationScopeAdminUserTwoFADisable:
		var context AdminUserContext
		if len(fields) != 1 || common.Unmarshal(fields["user_id"], &context.UserID) != nil || context.UserID <= 0 {
			return VerificationBinding{}, ErrVerificationContextInvalid
		}
		normalized = context
	case VerificationScopeAdminUserManage:
		var context AdminUserManageContext
		if common.Unmarshal(operation.Context, &context) != nil || context.UserID <= 0 || !slices.Contains(adminUserManageActions, context.Action) {
			return VerificationBinding{}, ErrVerificationContextInvalid
		}
		if context.Action == "set_role" {
			if len(fields) != 3 || context.Role == nil || !common.IsValidateRole(*context.Role) {
				return VerificationBinding{}, ErrVerificationContextInvalid
			}
		} else if len(fields) != 2 || context.Role != nil {
			return VerificationBinding{}, ErrVerificationContextInvalid
		}
		normalized = context
	case VerificationScopeAdminUserBindingClear:
		var context AdminUserBindingContext
		if len(fields) != 2 || common.Unmarshal(operation.Context, &context) != nil || context.UserID <= 0 || context.ProviderID < 0 {
			return VerificationBinding{}, ErrVerificationContextInvalid
		}
		context.BindingType = strings.ToLower(strings.TrimSpace(context.BindingType))
		if (context.BindingType == "") == (context.ProviderID == 0) || len(context.BindingType) > 32 {
			return VerificationBinding{}, ErrVerificationContextInvalid
		}
		normalized = context
	case VerificationScopeAdminUserCreate:
		var context AdminUserCreateContext
		if len(fields) != 1 || common.Unmarshal(fields["role"], &context.Role) != nil || context.Role < common.RoleAdminUser || !common.IsValidateRole(context.Role) {
			return VerificationBinding{}, ErrVerificationContextInvalid
		}
		normalized = context
	case VerificationScopeAccessTokenGenerate:
		var context AccessTokenGenerateContext
		if len(fields) != 2 || fields["scopes"] == nil || fields["expires_at"] == nil ||
			common.Unmarshal(operation.Context, &context) != nil || context.ExpiresAt < 0 {
			return VerificationBinding{}, ErrVerificationContextInvalid
		}
		scopes, ok := NormalizeAccessTokenScopeList(context.Scopes)
		if !ok {
			return VerificationBinding{}, ErrVerificationContextInvalid
		}
		context.Scopes = scopes
		normalized = context
	case VerificationScopeAccessTokenUpdate:
		var context AccessTokenUpdateContext
		if len(fields) != 2 || common.Unmarshal(operation.Context, &context) != nil || context.TokenID <= 0 {
			return VerificationBinding{}, ErrVerificationContextInvalid
		}
		scopes, ok := NormalizeAccessTokenScopeList(context.Scopes)
		if !ok {
			return VerificationBinding{}, ErrVerificationContextInvalid
		}
		context.Scopes = scopes
		normalized = context
	case VerificationScopeAccessTokenRevoke:
		var context AccessTokenRevokeContext
		if len(fields) != 1 || common.Unmarshal(operation.Context, &context) != nil || context.TokenID < 0 {
			return VerificationBinding{}, ErrVerificationContextInvalid
		}
		// Exactly one target: {"token_id": n} or {"legacy": true}.
		if (context.TokenID > 0) == context.Legacy {
			return VerificationBinding{}, ErrVerificationContextInvalid
		}
		normalized = context
	case VerificationScopePasskeyRegister, VerificationScopePasskeyDelete, VerificationScopeTwoFASetup,
		VerificationScopeTwoFADisable, VerificationScopeTwoFABackupCodes,
		VerificationScopePasswordSet, VerificationScopePasswordChange, VerificationScopeAccountDelete:
		if len(fields) != 0 {
			return VerificationBinding{}, ErrVerificationContextInvalid
		}
		normalized = struct{}{}
	default:
		return VerificationBinding{}, ErrProofScope
	}
	payload, err := common.Marshal(struct {
		Scope   string `json:"scope"`
		Context any    `json:"context"`
	}{Scope: operation.Scope, Context: normalized})
	if err != nil {
		return VerificationBinding{}, err
	}
	return VerificationBinding{
		Scope:       operation.Scope,
		ContextHash: common.GenerateHMACWithKey(authSigningKey("verification-context"), string(payload)),
	}, nil
}

type SecurityProof struct {
	ProofToken string `json:"proof_token"`
	ExpiresAt  int64  `json:"expires_at"`
	Method     string `json:"method"`
	Scope      string `json:"scope"`
}

type VerificationMethodOption struct {
	Method    string `json:"method"`
	Available bool   `json:"available"`
	Reason    string `json:"reason,omitempty"`
}

type VerificationOAuthProvider struct {
	Slug   string `json:"slug"`
	Name   string `json:"name"`
	UserID string `json:"-"`
}

type VerificationRequirements struct {
	Scope                     string                      `json:"scope"`
	Methods                   []VerificationMethodOption  `json:"methods"`
	OAuthProviders            []VerificationOAuthProvider `json:"oauth_providers"`
	PasswordEncryptionEnabled bool                        `json:"password_encryption_enabled"`
}

// StepUpContext carries request facts the policy needs beyond the identity.
type StepUpContext struct {
	// AccessToken is true when the request authenticated with an access token.
	AccessToken bool
	// LoginAt is the browser session's sign-in time (unix seconds), 0 if unknown.
	LoginAt int64
}

func (ctx StepUpContext) recentLogin(now time.Time) bool {
	if ctx.AccessToken || ctx.LoginAt <= 0 {
		return false
	}
	return now.Sub(time.Unix(ctx.LoginAt, 0)) < RecentLoginWindow
}

// securityVerificationPolicy is the only operation-to-method policy. Device
// support and disabled providers never turn an enrolled factor into an absent one.
func securityVerificationPolicy(scope string, state model.UserVerificationState, stepUp StepUpContext) ([]VerificationMethodOption, error) {
	var methods []string
	if state.HasTwoFA {
		methods = append(methods, VerificationMethodTwoFA)
	}
	if state.HasPasskey {
		methods = append(methods, VerificationMethodPasskey)
	}
	switch scope {
	case VerificationScopeChannelKeyRead, VerificationScopePasskeyDelete, VerificationScopeLogin:
	case VerificationScopeTwoFADisable, VerificationScopeTwoFABackupCodes:
		if !state.HasTwoFA {
			return nil, model.ErrTwoFANotEnabled
		}
	case VerificationScopePasskeyRegister, VerificationScopeTwoFASetup,
		VerificationScopeAccessTokenGenerate, VerificationScopeAccessTokenUpdate, VerificationScopeAccessTokenRevoke,
		VerificationScopeAccountBind, VerificationScopeAccountUnbind,
		VerificationScopePasswordSet, VerificationScopePasswordChange, VerificationScopeAccountDelete,
		VerificationScopeAdminUserCreate, VerificationScopeAdminUserUpdate, VerificationScopeAdminUserDelete,
		VerificationScopeAdminUserManage, VerificationScopeAdminUserPasskeyReset,
		VerificationScopeAdminUserTwoFADisable, VerificationScopeAdminUserBindingClear:
		if scope == VerificationScopeAccountDelete && state.Role == common.RoleRootUser {
			return nil, ErrVerificationForbidden
		}
		if scope == VerificationScopeTwoFASetup && state.HasTwoFA {
			return nil, model.ErrTwoFAAlreadyEnabled
		}
		if (scope == VerificationScopePasswordSet && state.HasPassword) || (scope == VerificationScopePasswordChange && !state.HasPassword) {
			return nil, ErrVerificationForbidden
		}
		if len(methods) == 0 {
			if state.HasPassword {
				methods = []string{VerificationMethodPassword}
			} else {
				methods = []string{VerificationMethodSession}
			}
		}
	default:
		return nil, ErrProofScope
	}
	options := make([]VerificationMethodOption, 0, len(methods))
	for _, method := range methods {
		option := VerificationMethodOption{Method: method, Available: true}
		if method == VerificationMethodTwoFA && state.TwoFALocked {
			option.Available, option.Reason = false, ErrVerificationLocked.Error()
		}
		if !system_setting.GetPasskeySettings().Enabled && (method == VerificationMethodPasskey || scope == VerificationScopePasskeyRegister) {
			option.Available, option.Reason = false, "Passkey authentication is disabled."
		}
		if method == VerificationMethodPassword && !common.GetAuthSettings().PasswordLoginEnabled {
			option.Available, option.Reason = false, "Password authentication is disabled."
		}
		if method == VerificationMethodSession && !stepUp.recentLogin(time.Now()) {
			option.Available, option.Reason = false, ErrVerificationReauthRequired.Error()
		}
		options = append(options, option)
	}
	return options, nil
}

func GetVerificationRequirements(identity AuthIdentity, stepUp StepUpContext, scope string) (*VerificationRequirements, error) {
	if scope == VerificationScopeLogin {
		return nil, ErrProofScope
	}
	if !identity.valid() {
		return nil, ErrAuthTokenInvalid
	}
	state, err := model.GetUserVerificationState(identity.UserID)
	if err != nil {
		return nil, err
	}
	if state.Status != common.UserStatusEnabled {
		return nil, ErrAuthTokenInvalid
	}
	if scope == VerificationScopeChannelKeyRead && state.Role != common.RoleRootUser {
		return nil, ErrVerificationForbidden
	}
	if strings.HasPrefix(scope, verificationScopeAdminUserPrefix) && state.Role < common.RoleAdminUser {
		return nil, ErrVerificationForbidden
	}
	if err := requireAccessTokenVerificationScope(identity, scope); err != nil {
		return nil, err
	}
	methods, err := securityVerificationPolicy(scope, *state, stepUp)
	if err != nil {
		return nil, err
	}
	return &VerificationRequirements{Scope: scope, Methods: methods, OAuthProviders: []VerificationOAuthProvider{}}, nil
}

// requireAccessTokenVerificationScope keeps access tokens from obtaining proofs
// for actions outside their grant. Access token management itself always needs
// a browser session.
func requireAccessTokenVerificationScope(identity AuthIdentity, scope string) error {
	if !strings.HasPrefix(identity.SessionID, AccessTokenSessionPrefix) {
		return nil
	}
	required, ok := accessTokenScopeForVerification[scope]
	if !ok || required == "" {
		return ErrVerificationForbidden
	}
	scopes, granted := lookupAccessTokenScopes(identity)
	if !granted || !slices.Contains(scopes, required) {
		return ErrVerificationForbidden
	}
	return nil
}

// AccessTokenSessionPrefix marks a step-up identity bound to a scoped access token.
const AccessTokenSessionPrefix = model.AccessTokenSessionPrefix

// accessTokenScopeForVerification maps a proof scope to the token scope a
// scoped access token must hold to obtain it. Token management maps to "" so a
// token can never authorize changes to tokens. Mirrors upstream.
var accessTokenScopeForVerification = map[string]string{
	VerificationScopeChannelKeyRead:        "channel:secret_view",
	VerificationScopePasskeyRegister:       "account_security:write",
	VerificationScopePasskeyDelete:         "account_security:write",
	VerificationScopeTwoFASetup:            "account_security:write",
	VerificationScopeTwoFADisable:          "account_security:write",
	VerificationScopeTwoFABackupCodes:      "account_security:write",
	VerificationScopeAccountBind:           "account_security:write",
	VerificationScopeAccountUnbind:         "account_security:write",
	VerificationScopePasswordSet:           "account_security:write",
	VerificationScopePasswordChange:        "account_security:write",
	VerificationScopeAccountDelete:         "account_security:write",
	VerificationScopeAdminUserCreate:       "user:write",
	VerificationScopeAdminUserUpdate:       "user:write",
	VerificationScopeAdminUserDelete:       "user:write",
	VerificationScopeAdminUserManage:       "user:write",
	VerificationScopeAdminUserPasskeyReset: "user:write",
	VerificationScopeAdminUserTwoFADisable: "user:write",
	VerificationScopeAdminUserBindingClear: "user:write",
	VerificationScopeAccessTokenGenerate:   "",
	VerificationScopeAccessTokenUpdate:     "",
	VerificationScopeAccessTokenRevoke:     "",
	VerificationScopeLogin:                 "",
}

// lookupAccessTokenScopes returns the grant of the token behind a "pat:<id>"
// identity. It is replaced once scoped access tokens exist.
var lookupAccessTokenScopes = func(identity AuthIdentity) ([]string, bool) { return nil, false }

func RequireVerificationMethod(identity AuthIdentity, stepUp StepUpContext, scope, method string) (*VerificationRequirements, error) {
	requirements, err := GetVerificationRequirements(identity, stepUp, scope)
	if err != nil {
		return nil, err
	}
	for _, option := range requirements.Methods {
		if option.Method != method {
			continue
		}
		if !option.Available {
			if method == VerificationMethodSession {
				return nil, ErrVerificationReauthRequired
			}
			return nil, ErrVerificationUnavailable
		}
		return requirements, nil
	}
	return nil, ErrProofMethod
}

// CompleteSecurityVerification runs after the concrete authentication ceremony
// and rechecks the policy, which may have changed during a slow ceremony.
func CompleteSecurityVerification(identity AuthIdentity, stepUp StepUpContext, binding VerificationBinding, method string) (*SecurityProof, error) {
	if _, err := RequireVerificationMethod(identity, stepUp, binding.Scope, method); err != nil {
		return nil, err
	}
	token, expiresAt, err := IssueSecurityProof(identity, method, binding)
	if err != nil {
		return nil, err
	}
	return &SecurityProof{ProofToken: token, ExpiresAt: expiresAt, Method: method, Scope: binding.Scope}, nil
}

// StepUpAuthorization records a consumed proof so a multi-step flow (begin →
// finish) can verify that its later steps belong to the same authorization.
type StepUpAuthorization struct {
	UserID      int    `json:"user_id"`
	SessionID   string `json:"session_id"`
	ProofID     string `json:"proof_id"`
	Scope       string `json:"scope"`
	ContextHash string `json:"context_hash"`
	Method      string `json:"method"`
}

// ConsumeOperationProof accepts a proof at most once. The consumption commits
// before the caller performs its action; an action failure must not restore it.
func ConsumeOperationProof(raw string, identity AuthIdentity, stepUp StepUpContext, operation VerificationOperation) (*StepUpAuthorization, error) {
	binding, err := BindVerificationOperation(operation)
	if err != nil {
		return nil, err
	}
	claims, err := verifySecurityProof(raw, identity, binding)
	if err != nil {
		return nil, err
	}
	if _, err := RequireVerificationMethod(identity, stepUp, binding.Scope, claims.Method); err != nil {
		// A session confirmation stays valid for the proof it already produced.
		if !(claims.Method == VerificationMethodSession && errors.Is(err, ErrVerificationReauthRequired)) {
			return nil, err
		}
	}
	if err := consumeSecurityProof(claims, identity); err != nil {
		return nil, err
	}
	return &StepUpAuthorization{
		UserID: identity.UserID, SessionID: identity.SessionID, ProofID: claims.ID,
		Scope: binding.Scope, ContextHash: binding.ContextHash, Method: claims.Method,
	}, nil
}

// ValidateFlowAuthorization permits a dedicated multi-step flow to outlive its
// consumed proof, while retaining its operation, identity and method policy.
func ValidateFlowAuthorization(identity AuthIdentity, stepUp StepUpContext, operation VerificationOperation, authorization *StepUpAuthorization) error {
	if authorization == nil || authorization.ProofID == "" || authorization.UserID != identity.UserID || authorization.SessionID != identity.SessionID {
		return ErrAuthTokenInvalid
	}
	binding, err := BindVerificationOperation(operation)
	if err != nil {
		return err
	}
	if authorization.Scope != binding.Scope || !hmac.Equal([]byte(authorization.ContextHash), []byte(binding.ContextHash)) {
		return ErrAuthTokenInvalid
	}
	_, err = RequireVerificationMethod(identity, stepUp, binding.Scope, authorization.Method)
	if authorization.Method == VerificationMethodSession && errors.Is(err, ErrVerificationReauthRequired) {
		return nil
	}
	return err
}

type VerificationInput struct {
	Method   string          `json:"method"`
	Scope    string          `json:"scope"`
	Context  json.RawMessage `json:"context,omitempty"`
	Code     string          `json:"code,omitempty"`
	Password string          `json:"password,omitempty"`
}

func VerifySecurityInput(identity AuthIdentity, stepUp StepUpContext, input VerificationInput) (*SecurityProof, error) {
	binding, err := BindVerificationOperation(VerificationOperation{Scope: input.Scope, Context: input.Context})
	if err != nil {
		return nil, err
	}
	if _, err := RequireVerificationMethod(identity, stepUp, input.Scope, input.Method); err != nil {
		return nil, err
	}
	switch input.Method {
	case VerificationMethodPassword:
		user, err := model.GetUserById(identity.UserID, true)
		if err != nil {
			return nil, err
		}
		if input.Password == "" || user.Password == "" || !common.ValidatePasswordAndHash(input.Password, user.Password) {
			return nil, ErrVerificationFailed
		}
	case VerificationMethodTwoFA:
		if input.Scope == VerificationScopeTwoFABackupCodes {
			if _, err := common.ValidateNumericCode(input.Code); err != nil {
				return nil, ErrVerificationFailed
			}
		}
		twoFA, err := model.GetTwoFAByUserId(identity.UserID)
		if err != nil {
			return nil, err
		}
		if err := VerifyTwoFactorCode(twoFA, input.Code); err != nil {
			return nil, err
		}
	case VerificationMethodSession:
		// The policy above already required a recent browser sign-in.
	case VerificationMethodPasskey, VerificationMethodOAuth:
		return nil, ErrVerificationFlowRequired
	default:
		return nil, ErrProofMethod
	}
	return CompleteSecurityVerification(identity, stepUp, binding, input.Method)
}

// VerifyTwoFactorCode classifies the input before verification so one failed
// submission cannot increment the failure counter for both TOTP and backup codes.
func VerifyTwoFactorCode(twoFA *model.TwoFA, code string) error {
	if twoFA == nil || !twoFA.IsEnabled {
		return model.ErrTwoFANotEnabled
	}
	if twoFA.IsLocked() {
		return ErrVerificationLocked
	}
	code = strings.TrimSpace(code)
	var valid bool
	var err error
	if numeric, numericErr := common.ValidateNumericCode(code); numericErr == nil {
		valid, err = twoFA.ValidateTOTPAndUpdateUsage(numeric)
	} else if common.ValidateBackupCode(code) {
		valid, err = twoFA.ValidateBackupCodeAndUpdateUsage(code)
	} else {
		err = twoFA.IncrementFailedAttempts()
	}
	if err != nil {
		return err
	}
	if !valid {
		return ErrVerificationFailed
	}
	return nil
}
