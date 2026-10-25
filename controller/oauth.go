package controller

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"eflow.local/new-api/common"
	"eflow.local/new-api/i18n"
	"eflow.local/new-api/middleware"
	"eflow.local/new-api/model"
	"eflow.local/new-api/oauth"
	"eflow.local/new-api/service"
	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// providerParams returns map with Provider key for i18n templates
func providerParams(name string) map[string]any {
	return map[string]any{"Provider": name}
}

// GenerateOAuthCode generates a state code for OAuth CSRF protection
// oauthBindAuthorizationKey keeps the consumed account.binding.bind proof from
// GenerateOAuthCode until the provider callback binds the account.
const oauthBindAuthorizationKey = "oauth_bind_authorization"

type oauthBindAuthorization struct {
	Provider      string                      `json:"provider"`
	Authorization service.StepUpAuthorization `json:"authorization"`
}

func GenerateOAuthCode(c *gin.Context) {
	session := sessions.Default(c)
	// eflow: 已登录账号绑定第三方账号时，先消费绑定该 provider 的一次性验证凭证（上游
	// 0973dc2b8 意图），回调里再核对，被盗用的会话不能悄悄绑上攻击者的第三方账号。
	session.Delete(oauthBindAuthorizationKey)
	if c.Query("purpose") == "bind" {
		if !authorizeOAuthBind(c, session, strings.TrimSpace(c.Query("provider"))) {
			return
		}
	}
	state := common.GetRandomString(12)
	affCode := c.Query("aff")
	invitationCode := strings.TrimSpace(c.Query("invitation_code"))
	if affCode != "" {
		session.Set("aff", affCode)
	}
	if invitationCode != "" {
		session.Set("invitation_code", invitationCode)
	} else {
		session.Delete("invitation_code")
	}
	session.Set("oauth_state", state)
	// eflow: Telegram 走 OpenID Connect + PKCE，校验串只留在服务端会话，浏览器拿到授权地址。
	response := gin.H{"success": true, "message": "", "data": state}
	session.Delete(telegramOAuthFlowSessionKey)
	if strings.TrimSpace(c.Query("provider")) == "telegram" {
		flow, err := oauth.NewTelegramOAuthFlow()
		if err != nil {
			common.ApiErrorMsg(c, err.Error())
			return
		}
		encoded, err := common.Marshal(flow)
		if err != nil {
			common.ApiError(c, err)
			return
		}
		session.Set(telegramOAuthFlowSessionKey, string(encoded))
		response["authorization_url"] = flow.AuthorizationURL(state)
	}
	err := session.Save()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, response)
}

const telegramOAuthFlowSessionKey = "telegram_oauth_flow"

// loadTelegramOAuthFlow moves the session's PKCE flow into the request for
// the Telegram provider; the flow is single use.
func loadTelegramOAuthFlow(c *gin.Context, session sessions.Session) {
	raw, _ := session.Get(telegramOAuthFlowSessionKey).(string)
	session.Delete(telegramOAuthFlowSessionKey)
	_ = session.Save()
	var flow oauth.TelegramOAuthFlow
	if raw != "" && common.Unmarshal([]byte(raw), &flow) == nil {
		c.Set(oauth.TelegramOAuthFlowContextKey, &flow)
	}
}

// consumeOAuthState 校验回调 state 并立即作废（一次有效），防止同一回调地址被重放；
// eflow: 校验失败也作废，state 不能被反复试探。失败时已写出响应。
func consumeOAuthState(c *gin.Context, session sessions.Session) bool {
	state := c.Query("state")
	expected, _ := session.Get("oauth_state").(string)
	if expected != "" {
		session.Delete("oauth_state")
		if err := session.Save(); err != nil {
			common.ApiErrorI18n(c, i18n.MsgUserSessionSaveFailed)
			return false
		}
	}
	if state == "" || expected == "" || state != expected {
		c.JSON(http.StatusForbidden, gin.H{
			"success": false,
			"message": i18n.T(c, i18n.MsgOAuthStateInvalid),
		})
		return false
	}
	return true
}

// HandleOAuth handles OAuth callback for all standard OAuth providers
func HandleOAuth(c *gin.Context) {
	providerName := c.Param("provider")
	provider := oauth.GetProvider(providerName)
	if provider == nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": i18n.T(c, i18n.MsgOAuthUnknownProvider),
		})
		return
	}

	session := sessions.Default(c)

	// 1. Validate state (CSRF protection)
	if !consumeOAuthState(c, session) {
		return
	}
	if providerName == "telegram" {
		loadTelegramOAuthFlow(c, session)
	}

	// 2. Check if user is already logged in (bind flow)
	if _, loggedIn := session.Get("id").(int); loggedIn {
		if !consumeOAuthBindAuthorization(c, session, providerName) {
			return
		}
		handleOAuthBind(c, provider)
		return
	}

	// 3. Check if provider is enabled
	if !provider.IsEnabled() {
		common.ApiErrorI18n(c, i18n.MsgOAuthNotEnabled, providerParams(provider.GetName()))
		return
	}

	// 4. Handle error from provider
	errorCode := c.Query("error")
	if errorCode != "" {
		errorDescription := c.Query("error_description")
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": errorDescription,
		})
		return
	}

	// 5. Exchange code for token
	code := c.Query("code")
	token, err := provider.ExchangeToken(c.Request.Context(), code, c)
	if err != nil {
		handleOAuthError(c, err)
		return
	}

	// 6. Get user info
	oauthUser, err := provider.GetUserInfo(c.Request.Context(), token)
	if err != nil {
		handleOAuthError(c, err)
		return
	}

	// 7. Find or create user
	user, err := findOrCreateOAuthUser(c, provider, oauthUser, session)
	if err != nil {
		if errors.Is(err, model.ErrEmailAlreadyTaken) {
			common.ApiErrorI18n(c, i18n.MsgUserEmailAlreadyTaken)
			return
		}
		if handleInvitationCodeError(c, err) {
			return
		}
		switch err.(type) {
		case *OAuthUserDeletedError:
			common.ApiErrorI18n(c, i18n.MsgOAuthUserDeleted)
		case *OAuthLegacyBindingError:
			common.ApiErrorI18n(c, i18n.MsgOAuthLegacyBindingRebind)
		case *OAuthRegistrationDisabledError:
			common.ApiErrorI18n(c, i18n.MsgUserRegisterDisabled)
		case *OAuthEmailAlreadyTakenError:
			common.ApiErrorI18n(c, i18n.MsgUserEmailAlreadyTaken)
		case *oauth.TrustLevelError:
			handleOAuthError(c, err)
		default:
			common.ApiError(c, err)
		}
		return
	}

	// eflow: 推广码、邀请码只对本次 OAuth 注册有效，用过即清，由后续登录流程保存会话。
	session.Delete("aff")
	session.Delete("invitation_code")

	// 8. Check user status
	if user.Status != common.UserStatusEnabled {
		_ = session.Save()
		common.ApiErrorI18n(c, i18n.MsgOAuthUserBanned)
		return
	}

	// 9. Setup login
	completeLogin(user, c, loginMethodOAuthPrefix+providerName)
}

// handleOAuthBind handles binding OAuth account to existing user
func handleOAuthBind(c *gin.Context, provider oauth.Provider) {
	if !provider.IsEnabled() {
		common.ApiErrorI18n(c, i18n.MsgOAuthNotEnabled, providerParams(provider.GetName()))
		return
	}

	// Exchange code for token
	code := c.Query("code")
	token, err := provider.ExchangeToken(c.Request.Context(), code, c)
	if err != nil {
		handleOAuthError(c, err)
		return
	}

	// Get user info
	oauthUser, err := provider.GetUserInfo(c.Request.Context(), token)
	if err != nil {
		handleOAuthError(c, err)
		return
	}

	// Check if this OAuth account is already bound (check both new ID and legacy ID)
	if provider.IsUserIDTaken(oauthUser.ProviderUserID) {
		common.ApiErrorI18n(c, i18n.MsgOAuthAlreadyBound, providerParams(provider.GetName()))
		return
	}
	// Also check legacy ID to prevent duplicate bindings during migration period
	if legacyID, ok := oauthUser.Extra["legacy_id"].(string); ok && legacyID != "" {
		if provider.IsUserIDTaken(legacyID) {
			common.ApiErrorI18n(c, i18n.MsgOAuthAlreadyBound, providerParams(provider.GetName()))
			return
		}
	}

	// Get current user from session
	session := sessions.Default(c)
	userId, ok := session.Get("id").(int)
	if !ok {
		common.ApiErrorI18n(c, i18n.MsgAuthNotLoggedIn)
		return
	}

	// Handle binding based on provider type
	if genericProvider, ok := provider.(*oauth.GenericOAuthProvider); ok {
		// Custom provider: use user_oauth_bindings table
		err = model.UpdateUserOAuthBinding(userId, genericProvider.GetProviderId(), oauthUser.ProviderUserID)
		if err != nil {
			common.ApiError(c, err)
			return
		}
	} else {
		// 内置 provider 只更新绑定列，避免完整用户快照覆盖并发的状态变更。
		err = model.UpdateUserBindColumn(userId, provider.ProviderUserIDColumn(), oauthUser.ProviderUserID)
		if err != nil {
			common.ApiError(c, err)
			return
		}
	}

	recordUserSecurityAudit(c, userId, "user.binding_bind", map[string]any{"provider": provider.GetName()})
	common.ApiSuccessI18n(c, i18n.MsgOAuthBindSuccess, gin.H{
		"action": "bind",
	})
}

// findOrCreateOAuthUser finds existing user or creates new user
func findOrCreateOAuthUser(c *gin.Context, provider oauth.Provider, oauthUser *oauth.OAuthUser, session sessions.Session) (*model.User, error) {
	user := &model.User{}
	authSettings := common.GetAuthSettings()
	userQuotaSettings := common.GetUserQuotaSettings()

	// Check if user already exists with new ID
	if provider.IsUserIDTaken(oauthUser.ProviderUserID) {
		err := provider.FillUserByProviderID(user, oauthUser.ProviderUserID)
		if err != nil {
			return nil, err
		}
		// Check if user has been deleted
		if user.Id == 0 {
			return nil, &OAuthUserDeletedError{}
		}
		return user, nil
	}

	// eflow: 早期 GitHub 绑定存的是用户名。GitHub 用户名放弃后可以被他人重新注册，
	// 仅凭用户名匹配就登录会导致账号被接管（上游 2906e4f77）。命中旧绑定时拒绝登录，
	// 也不新建账号，请用户用其他方式登录后重新绑定 GitHub。
	if legacyID, ok := oauthUser.Extra["legacy_id"].(string); ok && legacyID != "" {
		if provider.IsUserIDTaken(legacyID) {
			common.SysLog(fmt.Sprintf("[OAuth] Rejected GitHub login matching a legacy username binding, github_id=%s",
				oauthUser.ProviderUserID))
			return nil, &OAuthLegacyBindingError{}
		}
	}

	// User doesn't exist, create new user if registration is enabled
	if !authSettings.RegistrationEnabled {
		return nil, &OAuthRegistrationDisabledError{}
	}
	if validator, ok := provider.(oauth.NewUserValidator); ok {
		if err := validator.ValidateNewUser(c.Request.Context(), oauthUser); err != nil {
			return nil, err
		}
	}

	// Set up new user
	user.Username = provider.GetProviderPrefix() + strconv.Itoa(model.GetMaxUserId()+1)

	if oauthUser.Username != "" {
		if exists, err := model.CheckUserExistOrDeleted(oauthUser.Username, ""); err == nil && !exists {
			// 防止索引退化
			if len(oauthUser.Username) <= model.UserNameMaxLength {
				user.Username = oauthUser.Username
			}
		}
	}

	if oauthUser.DisplayName != "" {
		user.DisplayName = oauthUser.DisplayName
	} else if oauthUser.Username != "" {
		user.DisplayName = oauthUser.Username
	} else {
		user.DisplayName = provider.GetName() + " User"
	}
	if oauthUser.Email != "" {
		user.Email = model.NormalizeEmail(oauthUser.Email)
		if err := model.EnsureEmailAvailable(user.Email, 0); err != nil {
			if errors.Is(err, model.ErrEmailAlreadyTaken) {
				return nil, &OAuthEmailAlreadyTakenError{}
			}
			return nil, err
		}
	}
	user.Role = common.RoleCommonUser
	user.Status = common.UserStatusEnabled

	// Handle affiliate code
	affCode := session.Get("aff")
	inviterId := 0
	if affCode != nil {
		inviterId, _ = model.GetUserIdByAffCode(affCode.(string))
	}
	invitationCodeValue := ""
	if storedInvitationCode, ok := session.Get("invitation_code").(string); ok {
		invitationCodeValue = strings.TrimSpace(storedInvitationCode)
	}

	// Use transaction to ensure user creation and OAuth binding are atomic
	if genericProvider, ok := provider.(*oauth.GenericOAuthProvider); ok {
		// Custom provider: create user and binding in a transaction
		err := model.DB.Transaction(func(tx *gorm.DB) error {
			var invitationCode *model.InvitationCode
			if shouldValidateOAuthInvitationCode(invitationCodeValue, authSettings) {
				invitationCodeResult, err := model.GetUsableInvitationCodeWithTx(tx, invitationCodeValue)
				if err != nil {
					return err
				}
				invitationCode = invitationCodeResult
				if invitationCode.OwnerUserId != 0 {
					inviterId = invitationCode.OwnerUserId
				}
			}
			// Create user
			user.InviterId = inviterId
			if err := user.InsertWithTx(tx, inviterId, userQuotaSettings); err != nil {
				return err
			}

			// Create OAuth binding
			binding := &model.UserOAuthBinding{
				UserId:         user.Id,
				ProviderId:     genericProvider.GetProviderId(),
				ProviderUserId: oauthUser.ProviderUserID,
			}
			if err := model.CreateUserOAuthBindingWithTx(tx, binding); err != nil {
				return err
			}
			if invitationCode != nil {
				if err := model.ConsumeInvitationCodeWithTx(tx, invitationCode, user.Id, user.Username); err != nil {
					return err
				}
			}

			return nil
		})
		if err != nil {
			return nil, err
		}

		// Perform post-transaction tasks (logs, sidebar config, inviter rewards)
		user.FinalizeOAuthUserCreation(inviterId, userQuotaSettings)
	} else {
		// Built-in provider: create user and update provider ID in a transaction
		err := model.DB.Transaction(func(tx *gorm.DB) error {
			var invitationCode *model.InvitationCode
			if shouldValidateOAuthInvitationCode(invitationCodeValue, authSettings) {
				invitationCodeResult, err := model.GetUsableInvitationCodeWithTx(tx, invitationCodeValue)
				if err != nil {
					return err
				}
				invitationCode = invitationCodeResult
				if invitationCode.OwnerUserId != 0 {
					inviterId = invitationCode.OwnerUserId
				}
			}
			// Create user
			user.InviterId = inviterId
			if err := user.InsertWithTx(tx, inviterId, userQuotaSettings); err != nil {
				return err
			}

			// Set the provider user ID on the user model and update
			provider.SetProviderUserID(user, oauthUser.ProviderUserID)
			if err := tx.Model(user).Updates(map[string]any{
				"github_id":   user.GitHubId,
				"discord_id":  user.DiscordId,
				"oidc_id":     user.OidcId,
				"linux_do_id": user.LinuxDOId,
				"wechat_id":   user.WeChatId,
				"telegram_id": user.TelegramId,
			}).Error; err != nil {
				return err
			}
			if invitationCode != nil {
				if err := model.ConsumeInvitationCodeWithTx(tx, invitationCode, user.Id, user.Username); err != nil {
					return err
				}
			}

			return nil
		})
		if err != nil {
			return nil, err
		}

		// Perform post-transaction tasks
		user.FinalizeOAuthUserCreation(inviterId, userQuotaSettings)
	}

	return user, nil
}

func shouldValidateOAuthInvitationCode(invitationCode string, settings common.AuthSettings) bool {
	return settings.InvitationCodeEnabled &&
		(settings.InvitationCodeOAuthRequired || strings.TrimSpace(invitationCode) != "")
}

// Error types for OAuth
type OAuthUserDeletedError struct{}

func (e *OAuthUserDeletedError) Error() string {
	return "user has been deleted"
}

// OAuthLegacyBindingError 命中按 GitHub 用户名保存的旧绑定，需要用户重新绑定。
type OAuthLegacyBindingError struct{}

func (e *OAuthLegacyBindingError) Error() string {
	return "legacy oauth binding requires rebinding"
}

type OAuthRegistrationDisabledError struct{}

func (e *OAuthRegistrationDisabledError) Error() string {
	return "registration is disabled"
}

type OAuthEmailAlreadyTakenError struct{}

func (e *OAuthEmailAlreadyTakenError) Error() string {
	return "email is already in use"
}

// handleOAuthError handles OAuth errors and returns translated message
func handleOAuthError(c *gin.Context, err error) {
	switch e := err.(type) {
	case *oauth.OAuthError:
		if e.Params != nil {
			common.ApiErrorI18n(c, e.MsgKey, e.Params)
		} else {
			common.ApiErrorI18n(c, e.MsgKey)
		}
	case *oauth.AccessDeniedError:
		common.ApiErrorMsg(c, e.Message)
	case *oauth.TrustLevelError:
		common.ApiErrorI18n(c, i18n.MsgOAuthTrustLevelLow)
	default:
		common.ApiError(c, err)
	}
}

// authorizeOAuthBind consumes the binding proof for provider on behalf of the
// signed-in session user. It writes the error response when it returns false.
func authorizeOAuthBind(c *gin.Context, session sessions.Session, providerName string) bool {
	userID, ok := session.Get("id").(int)
	if !ok || userID <= 0 {
		common.ApiErrorI18n(c, i18n.MsgAuthNotLoggedIn)
		return false
	}
	user, err := model.GetUserById(userID, false)
	if err != nil || user.Status != common.UserStatusEnabled {
		common.ApiErrorI18n(c, i18n.MsgAuthNotLoggedIn)
		return false
	}
	if providerName == "" || oauth.GetProvider(providerName) == nil {
		common.ApiErrorI18n(c, i18n.MsgOAuthUnknownProvider)
		return false
	}
	c.Set("id", user.Id)
	c.Set("role", user.Role)
	proofContext, err := common.Marshal(service.AccountBindingContext{Provider: providerName})
	if err != nil {
		common.ApiError(c, err)
		return false
	}
	authorization := middleware.RequireSecurityProof(c, service.VerificationOperation{Scope: service.VerificationScopeAccountBind, Context: proofContext})
	if authorization == nil {
		return false
	}
	encoded, err := common.Marshal(oauthBindAuthorization{Provider: providerName, Authorization: *authorization})
	if err != nil {
		common.ApiError(c, err)
		return false
	}
	session.Set(oauthBindAuthorizationKey, string(encoded))
	return true
}

// consumeOAuthBindAuthorization checks that this bind callback continues the
// verified binding started for the same provider in this session.
func consumeOAuthBindAuthorization(c *gin.Context, session sessions.Session, providerName string) bool {
	raw, _ := session.Get(oauthBindAuthorizationKey).(string)
	session.Delete(oauthBindAuthorizationKey)
	_ = session.Save()
	var stored oauthBindAuthorization
	if raw == "" || common.Unmarshal([]byte(raw), &stored) != nil || stored.Provider != providerName {
		writeSecurityStatusError(c, http.StatusForbidden, "SECURITY_PROOF_REQUIRED", "Additional verification required")
		return false
	}
	userID, _ := session.Get("id").(int)
	c.Set("id", userID)
	identity, ok := middleware.GetStepUpIdentity(c)
	if !ok {
		writeSecurityStatusError(c, http.StatusForbidden, "SECURITY_PROOF_INVALID", "Security verification is no longer valid. Please verify again.")
		return false
	}
	proofContext, err := common.Marshal(service.AccountBindingContext{Provider: providerName})
	if err != nil {
		common.ApiError(c, err)
		return false
	}
	if err := service.ValidateFlowAuthorization(identity, middleware.GetStepUpContext(c), service.VerificationOperation{Scope: service.VerificationScopeAccountBind, Context: proofContext}, &stored.Authorization); err != nil {
		writeSecurityOperationError(c, err)
		return false
	}
	return true
}
