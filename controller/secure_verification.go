package controller

import (
	"errors"
	"net/http"

	"eflow.local/new-api/common"
	"eflow.local/new-api/middleware"
	"eflow.local/new-api/model"
	"eflow.local/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/go-webauthn/webauthn/protocol"
)

// LoginAtSessionKey eflow: 本次登录完成的时间戳（秒），无任何验证手段的账号据此判断"刚登录过"。
const LoginAtSessionKey = middleware.LoginAtSessionKey

// GetVerificationMethods returns the verification methods allowed for ?scope=.
func GetVerificationMethods(c *gin.Context) {
	identity, ok := middleware.GetStepUpIdentity(c)
	if !ok {
		writeSecurityStatusError(c, http.StatusUnauthorized, "SECURITY_PROOF_INVALID", "The current sign-in method does not support security verification")
		return
	}
	requirements, err := service.GetVerificationRequirements(identity, middleware.GetStepUpContext(c), c.Query("scope"))
	if err != nil {
		writeSecurityOperationError(c, err)
		return
	}
	common.ApiSuccess(c, requirements)
}

// UniversalVerify verifies a TOTP/backup code, the password or a recent
// sign-in for one scoped action and returns a single-use proof for it.
func UniversalVerify(c *gin.Context) {
	identity, ok := middleware.GetStepUpIdentity(c)
	if !ok {
		writeSecurityStatusError(c, http.StatusUnauthorized, "SECURITY_PROOF_INVALID", "The current sign-in method does not support security verification")
		return
	}
	var request service.VerificationInput
	if err := common.DecodeJson(c.Request.Body, &request); err != nil {
		common.ApiErrorMsg(c, "Invalid parameters")
		return
	}
	proof, err := service.VerifySecurityInput(identity, middleware.GetStepUpContext(c), request)
	if err != nil {
		recordUserSecurityAudit(c, identity.UserID, "user.security_verify", map[string]any{"method": request.Method, "scope": request.Scope, "success": false})
		writeSecurityOperationError(c, err)
		return
	}
	recordUserSecurityAudit(c, identity.UserID, "user.security_verify", map[string]any{"method": proof.Method, "scope": proof.Scope})
	common.ApiSuccess(c, proof)
}

// writeSecurityOperationError only exposes known, fixed business messages
// with a stable code; the frontend translates the English message.
func writeSecurityOperationError(c *gin.Context, err error) {
	status := http.StatusOK
	var code, message string
	var protocolError *protocol.Error
	switch {
	case errors.Is(err, service.ErrVerificationContextInvalid):
		status = http.StatusBadRequest
		code, message = "SECURITY_CONTEXT_INVALID", service.ErrVerificationContextInvalid.Error()
	case errors.Is(err, service.ErrVerificationForbidden):
		status = http.StatusForbidden
		code, message = "SECURITY_ACTION_FORBIDDEN", service.ErrVerificationForbidden.Error()
	case errors.Is(err, service.ErrVerificationFailed), errors.As(err, &protocolError):
		code, message = "SECURITY_VERIFICATION_FAILED", service.ErrVerificationFailed.Error()
	case errors.Is(err, service.ErrVerificationLocked):
		code, message = "SECURITY_VERIFICATION_LOCKED", service.ErrVerificationLocked.Error()
	case errors.Is(err, service.ErrVerificationReauthRequired):
		code, message = "REAUTH_REQUIRED", service.ErrVerificationReauthRequired.Error()
	case errors.Is(err, service.ErrVerificationUnavailable):
		code, message = "SECURITY_METHOD_UNAVAILABLE", service.ErrVerificationUnavailable.Error()
	case errors.Is(err, service.ErrVerificationFlowRequired):
		status = http.StatusBadRequest
		code, message = "SECURITY_VERIFICATION_FLOW_REQUIRED", service.ErrVerificationFlowRequired.Error()
	case errors.Is(err, service.ErrProofMethod):
		code, message = "SECURITY_PROOF_METHOD_MISMATCH", "This verification method is not allowed for this action."
	case errors.Is(err, service.ErrProofScope):
		code, message = "SECURITY_PROOF_SCOPE_MISMATCH", "Verification does not match this action."
	case errors.Is(err, model.ErrTwoFAAlreadyEnabled):
		code, message = "TWOFA_ALREADY_ENABLED", "Two-factor authentication is already enabled."
	case errors.Is(err, model.ErrTwoFANotEnabled):
		code, message = "TWOFA_NOT_ENABLED", "Two-factor authentication is not enabled."
	case errors.Is(err, model.ErrPasskeyNotFound):
		code, message = "PASSKEY_NOT_FOUND", "No Passkey is registered."
	case errors.Is(err, errPasskeyFlowInvalid), errors.Is(err, service.ErrAuthTokenInvalid), errors.Is(err, service.ErrAuthTokenExpired):
		code, message = "AUTH_FLOW_INVALID", "Verification flow expired"
	default:
		c.Set("security_error_code", "AUTH_INTERNAL_ERROR")
		common.SysError("security operation failed: " + err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "code": "AUTH_INTERNAL_ERROR", "message": "Please try again later."})
		return
	}
	writeSecurityStatusError(c, status, code, message)
}

func writeSecurityStatusError(c *gin.Context, status int, code, message string) {
	c.Set("security_error_code", code)
	c.JSON(status, gin.H{"success": false, "code": code, "message": message})
}

// requireAdminUserProof consumes the step-up proof for an administrative user
// operation after the caller has already authorized the operator against the
// managed user. context is one of the service.AdminUser*Context structs.
func requireAdminUserProof(c *gin.Context, scope string, context any) *service.StepUpAuthorization {
	payload, err := common.Marshal(context)
	if err != nil {
		common.ApiError(c, err)
		return nil
	}
	return middleware.RequireSecurityProof(c, service.VerificationOperation{Scope: scope, Context: payload})
}

// adminUserStatusExempt reports whether an enable/disable request may skip
// step-up. eflow: automation authenticated with an access token (admin-api)
// enables and disables users without a proof; scoped tokens still need the
// user:write route scope. Browser sessions always need a proof.
func adminUserStatusExempt(c *gin.Context, action string) bool {
	return (action == "enable" || action == "disable") && c.GetBool("use_access_token")
}
