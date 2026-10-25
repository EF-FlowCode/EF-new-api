package middleware

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"eflow.local/new-api/common"
	"eflow.local/new-api/model"
	"eflow.local/new-api/service"
	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
)

const (
	// LoginAtSessionKey 与 controller.LoginAtSessionKey 保持一致：本次登录完成的时间戳（秒）。
	LoginAtSessionKey = "login_at"
	// stepUpNonceSessionKey binds proofs to a browser session when the session
	// store has no server-side id (cookie store in development and tests).
	stepUpNonceSessionKey = "step_up_nonce"
	// SecurityProofHeader carries a single-use, action-bound verification proof.
	SecurityProofHeader = "X-Security-Proof"
)

// SecureVerificationRequired protects channel key disclosure. Other sensitive
// operations validate their narrower proof scopes in their controller.
func SecureVerificationRequired() gin.HandlerFunc {
	return func(c *gin.Context) {
		channelID, err := strconv.Atoi(c.Param("id"))
		if err != nil || channelID <= 0 {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"success": false, "code": "SECURITY_CONTEXT_INVALID", "message": service.ErrVerificationContextInvalid.Error()})
			return
		}
		context, err := common.Marshal(service.ChannelKeyReadContext{ChannelID: channelID})
		if err != nil {
			c.AbortWithStatus(http.StatusInternalServerError)
			return
		}
		if RequireSecurityProof(c, service.VerificationOperation{Scope: service.VerificationScopeChannelKeyRead, Context: context}) == nil {
			return
		}
		c.Set("secure_verified", true)
		c.Next()
	}
}

// GetStepUpIdentity returns the identity a proof is bound to: the browser
// session, or a scoped access token. Legacy access tokens cannot complete
// step-up verification.
func GetStepUpIdentity(c *gin.Context) (service.AuthIdentity, bool) {
	userID := c.GetInt("id")
	if userID <= 0 {
		return service.AuthIdentity{}, false
	}
	if c.GetBool("use_access_token") {
		tokenID := c.GetInt("access_token_id")
		if tokenID <= 0 {
			return service.AuthIdentity{}, false
		}
		return service.NewAuthIdentity(userID, model.AccessTokenSessionID(tokenID)), true
	}
	sessionID, ok := stepUpSessionID(c)
	if !ok {
		return service.AuthIdentity{}, false
	}
	return service.NewAuthIdentity(userID, sessionID), true
}

// GetStepUpContext returns the request facts the verification policy needs.
func GetStepUpContext(c *gin.Context) service.StepUpContext {
	if c.GetBool("use_access_token") {
		return service.StepUpContext{AccessToken: true}
	}
	stepUp := service.StepUpContext{}
	if _, ok := c.Get(sessions.DefaultKey); ok {
		stepUp.LoginAt, _ = sessions.Default(c).Get(LoginAtSessionKey).(int64)
	}
	return stepUp
}

func stepUpSessionID(c *gin.Context) (string, bool) {
	if _, ok := c.Get(sessions.DefaultKey); !ok {
		return "", false
	}
	session := sessions.Default(c)
	if sid := common.CurrentSessionID(session); sid != "" {
		return "session:" + sid, true
	}
	nonce, _ := session.Get(stepUpNonceSessionKey).(string)
	if nonce == "" {
		nonce = common.GetRandomString(32)
		session.Set(stepUpNonceSessionKey, nonce)
		if err := session.Save(); err != nil {
			return "", false
		}
	}
	return "nonce:" + nonce, true
}

// RequireSecurityProof validates a proof against the authenticated session or
// scoped access token and writes the shared proof error contract on failure.
func RequireSecurityProof(c *gin.Context, operation service.VerificationOperation) *service.StepUpAuthorization {
	identity, ok := GetStepUpIdentity(c)
	if !ok {
		securityProofError(c, "SECURITY_PROOF_INVALID", "Security verification is no longer valid. Please verify again.")
		return nil
	}
	raw := strings.TrimSpace(c.GetHeader(SecurityProofHeader))
	if raw == "" {
		securityProofError(c, "SECURITY_PROOF_REQUIRED", "Additional verification required")
		return nil
	}
	authorization, err := service.ConsumeOperationProof(raw, identity, GetStepUpContext(c), operation)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrAuthTokenExpired):
			securityProofError(c, "SECURITY_PROOF_EXPIRED", "Security verification has expired. Please verify again.")
		case errors.Is(err, service.ErrProofScope):
			securityProofError(c, "SECURITY_PROOF_SCOPE_MISMATCH", "Verification does not match this action.")
		case errors.Is(err, service.ErrVerificationContextInvalid):
			c.Set("security_error_code", "SECURITY_CONTEXT_INVALID")
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"success": false, "code": "SECURITY_CONTEXT_INVALID", "message": service.ErrVerificationContextInvalid.Error()})
		case errors.Is(err, service.ErrVerificationUnavailable):
			securityProofError(c, "SECURITY_METHOD_UNAVAILABLE", service.ErrVerificationUnavailable.Error())
		case errors.Is(err, service.ErrProofMethod):
			securityProofError(c, "SECURITY_PROOF_METHOD_MISMATCH", "This verification method is not allowed for this action.")
		case errors.Is(err, service.ErrProofConsumed):
			securityProofError(c, "SECURITY_PROOF_CONSUMED", "This verification has already been used. Please verify again.")
		case errors.Is(err, service.ErrProofContext):
			securityProofError(c, "SECURITY_PROOF_CONTEXT_MISMATCH", "Verification does not match this action's details. Please verify again.")
		case errors.Is(err, service.ErrVerificationForbidden):
			securityProofError(c, "SECURITY_ACTION_FORBIDDEN", service.ErrVerificationForbidden.Error())
		case errors.Is(err, service.ErrAuthTokenInvalid):
			securityProofError(c, "SECURITY_PROOF_INVALID", "Security verification is no longer valid. Please verify again.")
		default:
			_ = c.Error(err)
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"success": false, "code": "AUTH_INTERNAL_ERROR", "message": "Please try again later."})
		}
		return nil
	}
	return authorization
}

func securityProofError(c *gin.Context, code, message string) {
	c.Set("security_error_code", code)
	c.JSON(http.StatusForbidden, gin.H{
		"success": false,
		"message": message,
		"code":    code,
	})
	c.Abort()
}

// ResetStepUpSession drops step-up state bound to the browser session, so a
// sign-in in the same browser never inherits a previous account's binding.
// It also removes the pre-proof session markers of older releases.
func ResetStepUpSession(session sessions.Session) {
	session.Delete(stepUpNonceSessionKey)
	session.Delete("secure_verified_at")
	session.Delete("secure_verified_method")
	session.Delete("secure_passkey_ready_at")
}
