package controller

import (
	"encoding/json"
	"net/http"
	"time"

	"eflow.local/new-api/common"
	"eflow.local/new-api/i18n"
	"eflow.local/new-api/model"
	"eflow.local/new-api/service"
	passkeysvc "eflow.local/new-api/service/passkey"
	"eflow.local/new-api/setting/system_setting"
	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"github.com/go-webauthn/webauthn/protocol"
	webauthnlib "github.com/go-webauthn/webauthn/webauthn"
)

// eflow: 登录二次验证统一为 TOTP 或 Passkey（上游 6f2333990 意图）。账号绑定了任一因子时，
// 密码、OAuth、微信、Telegram 登录都先进入待验证状态（存于服务端会话），返回上游同形的
// LoginChallenge：{require_verification, flow_token, expires_at, methods}；为兼容旧前端，
// 启用了 TOTP 时仍带 require_2fa。完成验证走 /api/user/login/verify（TOTP/备用码）或
// /api/user/login/passkey/begin|finish；旧的 /api/user/login/2fa 保留为 TOTP 别名。

const (
	pendingLoginFlowTokenSessionKey = "pending_login_flow"
	loginPasskeyFlowKey             = "login_passkey_flow"
)

// LoginChallenge mirrors upstream service.LoginChallenge.
type LoginChallenge struct {
	RequireVerification bool                               `json:"require_verification"`
	Require2FA          bool                               `json:"require_2fa"`
	FlowToken           string                             `json:"flow_token"`
	ExpiresAt           int64                              `json:"expires_at"`
	Methods             []service.VerificationMethodOption `json:"methods"`
}

// loginVerificationMethods returns the second factors a login must pass, empty
// when the account has none.
func loginVerificationMethods(userID int) ([]service.VerificationMethodOption, error) {
	state, err := model.GetUserVerificationState(userID)
	if err != nil {
		return nil, err
	}
	var methods []service.VerificationMethodOption
	if state.HasTwoFA {
		option := service.VerificationMethodOption{Method: service.VerificationMethodTwoFA, Available: true}
		if state.TwoFALocked {
			option.Available, option.Reason = false, service.ErrVerificationLocked.Error()
		}
		methods = append(methods, option)
	}
	if state.HasPasskey {
		option := service.VerificationMethodOption{Method: service.VerificationMethodPasskey, Available: true}
		if !system_setting.GetPasskeySettings().Enabled {
			option.Available, option.Reason = false, "Passkey authentication is disabled."
		}
		methods = append(methods, option)
	}
	return methods, nil
}

// startPendingLogin stores the pending login in the session and answers with
// the login challenge.
func startPendingLogin(c *gin.Context, user *model.User, loginMethod string, methods []service.VerificationMethodOption) {
	session := sessions.Default(c)
	flowToken := common.GetRandomString(32)
	startedAt := time.Now()
	session.Set(pending2FAUsernameSessionKey, user.Username)
	session.Set(pending2FAUserIDSessionKey, user.Id)
	session.Set(pending2FAStartedAtSessionKey, startedAt.Unix())
	session.Set(pending2FALoginMethodSessionKey, loginMethod)
	session.Set(pendingLoginFlowTokenSessionKey, flowToken)
	session.Delete(loginPasskeyFlowKey)
	if err := session.Save(); err != nil {
		common.ApiErrorI18n(c, i18n.MsgUserSessionSaveFailed)
		return
	}
	challenge := LoginChallenge{
		RequireVerification: true,
		FlowToken:           flowToken,
		ExpiresAt:           startedAt.Add(pending2FAMaxAge).Unix(),
		Methods:             methods,
	}
	for _, method := range methods {
		if method.Method == service.VerificationMethodTwoFA {
			challenge.Require2FA = true
		}
	}
	c.JSON(http.StatusOK, gin.H{
		"message": i18n.T(c, i18n.MsgUserRequire2FA),
		"success": true,
		"data":    challenge,
	})
}

// pendingLoginUser returns the user waiting for login verification in this
// session. flowToken, when not empty, must name the pending login.
func pendingLoginUser(c *gin.Context, flowToken string) (*model.User, string, bool) {
	session := sessions.Default(c)
	userID, ok := session.Get(pending2FAUserIDSessionKey).(int)
	startedAt, _ := session.Get(pending2FAStartedAtSessionKey).(int64)
	stored, _ := session.Get(pendingLoginFlowTokenSessionKey).(string)
	expired := !ok || startedAt == 0 || time.Since(time.Unix(startedAt, 0)) > pending2FAMaxAge
	if expired || (flowToken != "" && flowToken != stored) {
		if expired {
			clearPending2FA(session)
			_ = session.Save()
		}
		writeSecurityStatusError(c, http.StatusOK, "AUTH_FLOW_INVALID", "会话已过期，请重新登录")
		return nil, "", false
	}
	user, err := model.GetUserById(userID, false)
	if err != nil {
		writeSecurityStatusError(c, http.StatusOK, "AUTH_FLOW_INVALID", "会话已过期，请重新登录")
		return nil, "", false
	}
	loginMethod, _ := session.Get(pending2FALoginMethodSessionKey).(string)
	if loginMethod == "" {
		loginMethod = loginMethodPassword
	}
	return user, loginMethod, true
}

// finishPendingLogin clears the pending state and signs the user in.
func finishPendingLogin(c *gin.Context, user *model.User, loginMethod, factor string) {
	session := sessions.Default(c)
	clearPending2FA(session)
	session.Delete(pendingLoginFlowTokenSessionKey)
	session.Delete(loginPasskeyFlowKey)
	if user.Status != common.UserStatusEnabled {
		_ = session.Save()
		common.ApiErrorI18n(c, i18n.MsgAuthUserBanned)
		return
	}
	setupLogin(user, c, loginMethod+"+"+factor)
}

// VerifyLogin completes a pending login with a TOTP or backup code.
func VerifyLogin(c *gin.Context) {
	var request struct {
		FlowToken string `json:"flow_token"`
		Method    string `json:"method"`
		Code      string `json:"code"`
	}
	if common.DecodeJson(c.Request.Body, &request) != nil || request.FlowToken == "" || request.Code == "" {
		common.ApiErrorMsg(c, "Invalid parameters")
		return
	}
	if request.Method != "" && request.Method != service.VerificationMethodTwoFA {
		writeSecurityOperationError(c, service.ErrProofMethod)
		return
	}
	verifyLoginTwoFactor(c, request.FlowToken, request.Code)
}

func verifyLoginTwoFactor(c *gin.Context, flowToken, code string) {
	user, loginMethod, ok := pendingLoginUser(c, flowToken)
	if !ok {
		return
	}
	twoFA, err := model.GetTwoFAByUserId(user.Id)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if err := service.VerifyTwoFactorCode(twoFA, code); err != nil {
		writeSecurityOperationError(c, err)
		return
	}
	finishPendingLogin(c, user, loginMethod, service.VerificationMethodTwoFA)
}

// LoginPasskeyBegin starts a user-verifying assertion for the pending login.
func LoginPasskeyBegin(c *gin.Context) {
	if passkeyDisabled(c) {
		return
	}
	var request struct {
		FlowToken string `json:"flow_token"`
	}
	if common.DecodeJson(c.Request.Body, &request) != nil || request.FlowToken == "" {
		common.ApiErrorMsg(c, "Invalid parameters")
		return
	}
	user, _, ok := pendingLoginUser(c, request.FlowToken)
	if !ok {
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
	options, sessionData, err := wa.BeginLogin(passkeysvc.NewWebAuthnUser(user, credential), webauthnlib.WithUserVerification(protocol.VerificationRequired))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	token, expiresAt, err := passkeysvc.SaveFlow(c, loginPasskeyFlowKey, passkeysvc.Flow{SessionData: *sessionData, Scope: request.FlowToken})
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"flow_token": token, "expires_at": expiresAt, "options": options})
}

// LoginPasskeyFinish validates the assertion and completes the pending login.
func LoginPasskeyFinish(c *gin.Context) {
	if passkeyDisabled(c) {
		return
	}
	var request struct {
		FlowToken        string          `json:"flow_token"`
		PasskeyFlowToken string          `json:"passkey_flow_token"`
		Credential       json.RawMessage `json:"credential"`
	}
	if common.DecodeJson(c.Request.Body, &request) != nil || request.FlowToken == "" || request.PasskeyFlowToken == "" || len(request.Credential) == 0 {
		common.ApiErrorMsg(c, "Invalid parameters")
		return
	}
	user, loginMethod, ok := pendingLoginUser(c, request.FlowToken)
	if !ok {
		return
	}
	parsed, err := protocol.ParseCredentialRequestResponseBytes(request.Credential)
	if err != nil {
		writeSecurityOperationError(c, err)
		return
	}
	flow, err := passkeysvc.PopFlow(c, loginPasskeyFlowKey, request.PasskeyFlowToken)
	if err != nil {
		writeSecurityOperationError(c, err)
		return
	}
	if flow.Scope != request.FlowToken || flow.SessionData.UserVerification != protocol.VerificationRequired {
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
	if _, err := wa.ValidateLogin(passkeysvc.NewWebAuthnUser(user, credential), flow.SessionData, parsed); err != nil {
		writeSecurityOperationError(c, err)
		return
	}
	now := time.Now()
	credential.LastUsedAt = &now
	if err := model.UpsertPasskeyCredential(credential); err != nil {
		common.ApiError(c, err)
		return
	}
	finishPendingLogin(c, user, loginMethod, service.VerificationMethodPasskey)
}
