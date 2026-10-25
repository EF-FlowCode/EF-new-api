package controller

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"eflow.local/new-api/common"
	"eflow.local/new-api/middleware"
	"eflow.local/new-api/model"
	"eflow.local/new-api/service"
	passkeysvc "eflow.local/new-api/service/passkey"
	"eflow.local/new-api/setting/system_setting"

	"github.com/gin-gonic/gin"
	"github.com/go-webauthn/webauthn/protocol"
	webauthnlib "github.com/go-webauthn/webauthn/webauthn"
)

type passkeyFinishRequest struct {
	FlowToken  string          `json:"flow_token"`
	Credential json.RawMessage `json:"credential"`
}

type passkeyVerifyBeginRequest struct {
	Scope   string          `json:"scope"`
	Context json.RawMessage `json:"context,omitempty"`
}

// errPasskeyFlowInvalid reports a missing, expired or mismatched WebAuthn ceremony.
var errPasskeyFlowInvalid = passkeysvc.ErrFlowInvalid()

const (
	passkeyRegisterFlowKey = "passkey_register_flow"
	passkeyStepUpFlowKey   = "passkey_step_up_flow"
)

func parsePasskeyFinishRequest(c *gin.Context) (*passkeyFinishRequest, error) {
	var request passkeyFinishRequest
	if err := common.DecodeJson(c.Request.Body, &request); err != nil {
		return nil, err
	}
	if request.FlowToken == "" || len(request.Credential) == 0 {
		return nil, errors.New("incomplete Passkey flow parameters")
	}
	return &request, nil
}

// getAuthenticatedUser loads the enabled user the auth middleware identified.
func getAuthenticatedUser(c *gin.Context) (*model.User, error) {
	id := c.GetInt("id")
	if id == 0 {
		return nil, service.ErrAuthTokenInvalid
	}
	user, err := model.GetUserById(id, false)
	if err != nil {
		return nil, err
	}
	if user.Status != common.UserStatusEnabled {
		return nil, service.ErrAuthTokenInvalid
	}
	return user, nil
}

func passkeyDisabled(c *gin.Context) bool {
	if system_setting.GetPasskeySettings().Enabled {
		return false
	}
	common.ApiErrorMsg(c, "管理员未启用 Passkey 登录")
	return true
}

// PasskeyRegisterBegin requires a passkey.register proof and starts a
// registration that requires user verification.
func PasskeyRegisterBegin(c *gin.Context) {
	if passkeyDisabled(c) {
		return
	}
	user, err := getAuthenticatedUser(c)
	if err != nil {
		writeSecurityOperationError(c, err)
		return
	}
	authorization := middleware.RequireSecurityProof(c, service.VerificationOperation{Scope: service.VerificationScopePasskeyRegister})
	if authorization == nil {
		return
	}

	credential, err := model.GetPasskeyByUserID(user.Id)
	if err != nil && !errors.Is(err, model.ErrPasskeyNotFound) {
		common.ApiError(c, err)
		return
	}
	if errors.Is(err, model.ErrPasskeyNotFound) {
		credential = nil
	}

	wa, err := passkeysvc.BuildWebAuthn(c.Request)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	waUser := passkeysvc.NewWebAuthnUser(user, credential)
	selection := wa.Config.AuthenticatorSelection
	selection.UserVerification = protocol.VerificationRequired
	options := []webauthnlib.RegistrationOption{webauthnlib.WithAuthenticatorSelection(selection)}
	if credential != nil {
		descriptor := credential.ToWebAuthnCredential().Descriptor()
		options = append(options, webauthnlib.WithExclusions([]protocol.CredentialDescriptor{descriptor}))
	}

	creation, sessionData, err := wa.BeginRegistration(waUser, options...)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	encodedAuthorization, err := common.Marshal(authorization)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	flowToken, expiresAt, err := passkeysvc.SaveFlow(c, passkeyRegisterFlowKey, passkeysvc.Flow{
		SessionData: *sessionData, Scope: authorization.Scope, ContextHash: authorization.ContextHash, Authorization: encodedAuthorization,
	})
	if err != nil {
		common.ApiError(c, err)
		return
	}

	common.ApiSuccess(c, gin.H{"options": creation, "flow_token": flowToken, "expires_at": expiresAt})
}

func PasskeyRegisterFinish(c *gin.Context) {
	if passkeyDisabled(c) {
		return
	}
	user, err := getAuthenticatedUser(c)
	if err != nil {
		writeSecurityOperationError(c, err)
		return
	}
	request, err := parsePasskeyFinishRequest(c)
	if err != nil {
		common.ApiErrorMsg(c, "Invalid Passkey verification request")
		return
	}
	parsedCredential, err := protocol.ParseCredentialCreationResponseBytes(request.Credential)
	if err != nil {
		writeSecurityOperationError(c, err)
		return
	}
	identity, ok := middleware.GetStepUpIdentity(c)
	if !ok {
		writeSecurityOperationError(c, service.ErrAuthTokenInvalid)
		return
	}
	flow, err := passkeysvc.PopFlow(c, passkeyRegisterFlowKey, request.FlowToken)
	if err != nil {
		writeSecurityOperationError(c, err)
		return
	}
	if flow.SessionData.UserVerification != protocol.VerificationRequired {
		writeSecurityOperationError(c, errPasskeyFlowInvalid)
		return
	}
	var authorization service.StepUpAuthorization
	if common.Unmarshal(flow.Authorization, &authorization) != nil {
		writeSecurityOperationError(c, errPasskeyFlowInvalid)
		return
	}
	if err := service.ValidateFlowAuthorization(identity, middleware.GetStepUpContext(c), service.VerificationOperation{Scope: service.VerificationScopePasskeyRegister}, &authorization); err != nil {
		writeSecurityOperationError(c, err)
		return
	}

	credentialRecord, err := model.GetPasskeyByUserID(user.Id)
	if err != nil && !errors.Is(err, model.ErrPasskeyNotFound) {
		common.ApiError(c, err)
		return
	}
	if errors.Is(err, model.ErrPasskeyNotFound) {
		credentialRecord = nil
	}
	wa, err := passkeysvc.BuildWebAuthn(c.Request)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	waUser := passkeysvc.NewWebAuthnUser(user, credentialRecord)
	credential, err := wa.CreateCredential(waUser, flow.SessionData, parsedCredential)
	if err != nil {
		writeSecurityOperationError(c, err)
		return
	}

	passkeyCredential := model.NewPasskeyCredentialFromWebAuthn(user.Id, credential)
	if passkeyCredential == nil {
		common.ApiErrorMsg(c, "无法创建 Passkey 凭证")
		return
	}
	if rpID := flow.SessionData.RelyingPartyID; rpID != "" {
		passkeyCredential.RPID = &rpID
	}
	if err := model.UpsertPasskeyCredential(passkeyCredential); err != nil {
		common.ApiError(c, err)
		return
	}
	// A new sign-in factor signs out the account's other browser sessions.
	revokeUserSessions(c, user.Id, true)
	recordUserSecurityAudit(c, user.Id, "user.passkey_register", nil)
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "Passkey 注册成功"})
}

func PasskeyDelete(c *gin.Context) {
	user, err := getAuthenticatedUser(c)
	if err != nil {
		writeSecurityOperationError(c, err)
		return
	}
	if middleware.RequireSecurityProof(c, service.VerificationOperation{Scope: service.VerificationScopePasskeyDelete}) == nil {
		return
	}
	if err := model.DeletePasskeyByUserID(user.Id); err != nil {
		common.ApiError(c, err)
		return
	}
	revokeUserSessions(c, user.Id, true)
	recordUserSecurityAudit(c, user.Id, "user.passkey_delete", nil)
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "Passkey 已解绑"})
}

func PasskeyStatus(c *gin.Context) {
	user, err := getAuthenticatedUser(c)
	if err != nil {
		writeSecurityOperationError(c, err)
		return
	}

	credential, err := model.GetPasskeyByUserID(user.Id)
	if errors.Is(err, model.ErrPasskeyNotFound) {
		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"message": "",
			"data": gin.H{
				"enabled": false,
			},
		})
		return
	}
	if err != nil {
		common.ApiError(c, err)
		return
	}

	data := gin.H{
		"enabled":      true,
		"last_used_at": credential.LastUsedAt,
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    data,
	})
}

func PasskeyLoginBegin(c *gin.Context) {
	if !system_setting.GetPasskeySettings().Enabled {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "管理员未启用 Passkey 登录",
		})
		return
	}

	wa, err := passkeysvc.BuildWebAuthn(c.Request)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	assertion, sessionData, err := wa.BeginDiscoverableLogin(webauthnlib.WithUserVerification(protocol.VerificationRequired))
	if err != nil {
		common.ApiError(c, err)
		return
	}

	if err := passkeysvc.SaveSessionData(c, passkeysvc.LoginSessionKey, sessionData); err != nil {
		common.ApiError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data": gin.H{
			"options": assertion,
		},
	})
}

func PasskeyLoginFinish(c *gin.Context) {
	if !system_setting.GetPasskeySettings().Enabled {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "管理员未启用 Passkey 登录",
		})
		return
	}

	wa, err := passkeysvc.BuildWebAuthn(c.Request)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	sessionData, err := passkeysvc.PopSessionData(c, passkeysvc.LoginSessionKey)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if sessionData.UserVerification != protocol.VerificationRequired {
		writeSecurityOperationError(c, errPasskeyFlowInvalid)
		return
	}

	handler := func(rawID, userHandle []byte) (webauthnlib.User, error) {
		// 首先通过凭证ID查找用户
		credential, err := model.GetPasskeyByCredentialID(rawID)
		if err != nil {
			return nil, fmt.Errorf("未找到 Passkey 凭证: %w", err)
		}

		// 通过凭证获取用户
		user := &model.User{Id: credential.UserID}
		if err := user.FillUserById(); err != nil {
			return nil, fmt.Errorf("用户信息获取失败: %w", err)
		}

		if user.Status != common.UserStatusEnabled {
			return nil, errors.New("该用户已被禁用")
		}

		if len(userHandle) > 0 {
			userID, parseErr := strconv.Atoi(string(userHandle))
			if parseErr != nil {
				// 记录异常但继续验证，因为某些客户端可能使用非数字格式
				common.SysLog(fmt.Sprintf("PasskeyLogin: userHandle parse error for credential, length: %d", len(userHandle)))
			} else if userID != user.Id {
				return nil, errors.New("用户句柄与凭证不匹配")
			}
		}

		return passkeysvc.NewWebAuthnUser(user, credential), nil
	}

	waUser, credential, err := wa.FinishPasskeyLogin(handler, *sessionData, c.Request)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	userWrapper, ok := waUser.(*passkeysvc.WebAuthnUser)
	if !ok {
		common.ApiErrorMsg(c, "Passkey 登录状态异常")
		return
	}

	modelUser := userWrapper.ModelUser()
	if modelUser == nil {
		common.ApiErrorMsg(c, "Passkey 登录状态异常")
		return
	}

	if modelUser.Status != common.UserStatusEnabled {
		common.ApiErrorMsg(c, "该用户已被禁用")
		return
	}

	// 更新凭证信息
	updatedCredential := model.NewPasskeyCredentialFromWebAuthn(modelUser.Id, credential)
	if updatedCredential == nil {
		common.ApiErrorMsg(c, "Passkey 凭证更新失败")
		return
	}
	if stored := userWrapper.PasskeyCredential(); stored != nil {
		updatedCredential.RPID = stored.RPID
	}
	if err := passkeysvc.CheckCredentialRPID(updatedCredential, sessionData.RelyingPartyID); err != nil {
		common.ApiErrorMsg(c, err.Error())
		return
	}
	now := time.Now()
	updatedCredential.LastUsedAt = &now
	if err := model.UpsertPasskeyCredential(updatedCredential); err != nil {
		common.ApiError(c, err)
		return
	}

	setupLogin(modelUser, c, loginMethodPasskey)
	return
}

func AdminResetPasskey(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiErrorMsg(c, "无效的用户 ID")
		return
	}

	user := &model.User{Id: id}
	if err := user.FillUserById(); err != nil {
		common.ApiError(c, err)
		return
	}
	myRole := c.GetInt("role")
	if !canManageTargetRole(myRole, user.Role) {
		common.ApiErrorMsg(c, "no permission")
		return
	}

	if _, err := model.GetPasskeyByUserID(user.Id); err != nil {
		if errors.Is(err, model.ErrPasskeyNotFound) {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": "该用户尚未绑定 Passkey",
			})
			return
		}
		common.ApiError(c, err)
		return
	}

	if requireAdminUserProof(c, service.VerificationScopeAdminUserPasskeyReset, service.AdminUserContext{UserID: user.Id}) == nil {
		return
	}
	if err := model.DeletePasskeyByUserID(user.Id); err != nil {
		common.ApiError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "Passkey 已重置",
	})
}

// PasskeyVerifyBegin starts a user-verifying assertion for one scoped action.
func PasskeyVerifyBegin(c *gin.Context) {
	if passkeyDisabled(c) {
		return
	}
	user, err := getAuthenticatedUser(c)
	if err != nil {
		writeSecurityOperationError(c, err)
		return
	}
	var request passkeyVerifyBeginRequest
	if err := common.DecodeJson(c.Request.Body, &request); err != nil {
		common.ApiErrorMsg(c, "Invalid Passkey verification request")
		return
	}
	binding, err := service.BindVerificationOperation(service.VerificationOperation{Scope: request.Scope, Context: request.Context})
	if err != nil {
		writeSecurityOperationError(c, err)
		return
	}
	identity, ok := middleware.GetStepUpIdentity(c)
	if !ok {
		writeSecurityOperationError(c, service.ErrAuthTokenInvalid)
		return
	}
	if _, err := service.RequireVerificationMethod(identity, middleware.GetStepUpContext(c), request.Scope, service.VerificationMethodPasskey); err != nil {
		writeSecurityOperationError(c, err)
		return
	}

	credential, err := model.GetPasskeyByUserID(user.Id)
	if err != nil {
		writeSecurityOperationError(c, err)
		return
	}
	wa, err := passkeysvc.BuildWebAuthn(c.Request)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	waUser := passkeysvc.NewWebAuthnUser(user, credential)
	assertion, sessionData, err := wa.BeginLogin(waUser, webauthnlib.WithUserVerification(protocol.VerificationRequired))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	flowToken, expiresAt, err := passkeysvc.SaveFlow(c, passkeyStepUpFlowKey, passkeysvc.Flow{
		SessionData: *sessionData, Scope: binding.Scope, ContextHash: binding.ContextHash,
	})
	if err != nil {
		common.ApiError(c, err)
		return
	}

	common.ApiSuccess(c, gin.H{"options": assertion, "flow_token": flowToken, "expires_at": expiresAt})
}

// PasskeyVerifyFinish validates the assertion and returns the security proof.
func PasskeyVerifyFinish(c *gin.Context) {
	if passkeyDisabled(c) {
		return
	}
	user, err := getAuthenticatedUser(c)
	if err != nil {
		writeSecurityOperationError(c, err)
		return
	}
	request, err := parsePasskeyFinishRequest(c)
	if err != nil {
		common.ApiErrorMsg(c, "Invalid Passkey verification request")
		return
	}
	parsedCredential, err := protocol.ParseCredentialRequestResponseBytes(request.Credential)
	if err != nil {
		writeSecurityOperationError(c, err)
		return
	}
	identity, ok := middleware.GetStepUpIdentity(c)
	if !ok {
		writeSecurityOperationError(c, service.ErrAuthTokenInvalid)
		return
	}
	flow, err := passkeysvc.PopFlow(c, passkeyStepUpFlowKey, request.FlowToken)
	if err != nil {
		writeSecurityOperationError(c, err)
		return
	}
	if flow.SessionData.UserVerification != protocol.VerificationRequired {
		writeSecurityOperationError(c, errPasskeyFlowInvalid)
		return
	}

	credential, err := model.GetPasskeyByUserID(user.Id)
	if err != nil {
		writeSecurityOperationError(c, err)
		return
	}
	wa, err := passkeysvc.BuildWebAuthn(c.Request)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if err := passkeysvc.CheckCredentialRPID(credential, flow.SessionData.RelyingPartyID); err != nil {
		common.ApiErrorMsg(c, err.Error())
		return
	}
	waUser := passkeysvc.NewWebAuthnUser(user, credential)
	if _, err := wa.ValidateLogin(waUser, flow.SessionData, parsedCredential); err != nil {
		writeSecurityOperationError(c, err)
		return
	}
	now := time.Now()
	credential.LastUsedAt = &now
	if err := model.UpsertPasskeyCredential(credential); err != nil {
		common.ApiError(c, err)
		return
	}

	proof, err := service.CompleteSecurityVerification(identity, middleware.GetStepUpContext(c), service.VerificationBinding{Scope: flow.Scope, ContextHash: flow.ContextHash}, service.VerificationMethodPasskey)
	if err != nil {
		writeSecurityOperationError(c, err)
		return
	}
	recordUserSecurityAudit(c, user.Id, "user.security_verify", map[string]any{"method": proof.Method, "scope": proof.Scope})
	common.ApiSuccess(c, proof)
}
