package controller

import (
	"net/http"
	"testing"
	"time"

	"eflow.local/new-api/common"
	"eflow.local/new-api/model"
	"github.com/pquerna/otp/totp"
	"github.com/stretchr/testify/require"
)

// Each self-service security change needs a proof for exactly its own scope.
func TestSelfServiceSecurityChangesRequireTheirOwnProof(t *testing.T) {
	db := useStepUpTestDB(t)
	hash, err := common.Password2Hash("old-password-1")
	require.NoError(t, err)
	user := model.User{Username: "self-sec", Password: hash, AffCode: "ssec01", Role: common.RoleCommonUser, Status: common.UserStatusEnabled}
	require.NoError(t, db.Create(&user).Error)
	h := newStepUpHarness(t, &user)
	h.engine.PUT("/self", func(c *ginContext) { asStepUpUser(t, c); UpdateSelf(c) })
	h.engine.POST("/2fa/setup", func(c *ginContext) { asStepUpUser(t, c); Setup2FA(c) })
	h.engine.POST("/2fa/enable", func(c *ginContext) { asStepUpUser(t, c); Enable2FA(c) })
	h.engine.POST("/2fa/disable", func(c *ginContext) { asStepUpUser(t, c); Disable2FA(c) })
	h.engine.POST("/2fa/backup_codes", func(c *ginContext) { asStepUpUser(t, c); RegenerateBackupCodes(c) })

	changePassword := `{"username":"self-sec","original_password":"old-password-1","password":"new-password-2"}`
	status, result := h.do(http.MethodPut, "/self", changePassword, "")
	require.Equal(t, http.StatusForbidden, status)
	require.Equal(t, "SECURITY_PROOF_REQUIRED", result["code"])

	deleteProof := h.proof(`{"method":"password","scope":"account.delete","password":"old-password-1"}`)
	_, result = h.do(http.MethodPut, "/self", changePassword, deleteProof)
	require.Equal(t, "SECURITY_PROOF_SCOPE_MISMATCH", result["code"])

	proof := h.proof(`{"method":"password","scope":"account.password.change","password":"old-password-1"}`)
	_, result = h.do(http.MethodPut, "/self", changePassword, proof)
	require.Equal(t, true, result["success"], result)

	// Enabling 2FA must continue a setup started with a 2fa.setup proof.
	_, result = h.do(http.MethodPost, "/2fa/setup", "", "")
	require.Equal(t, "SECURITY_PROOF_REQUIRED", result["code"])
	setupProof := h.proof(`{"method":"password","scope":"2fa.setup","password":"new-password-2"}`)
	_, result = h.do(http.MethodPost, "/2fa/setup", "", setupProof)
	require.Equal(t, true, result["success"], result)
	secret := result["data"].(map[string]any)["secret"].(string)

	code, err := totp.GenerateCode(secret, time.Now())
	require.NoError(t, err)
	other := newStepUpHarness(t, &user)
	other.engine.POST("/2fa/enable", func(c *ginContext) { asStepUpUser(t, c); Enable2FA(c) })
	_, result = other.do(http.MethodPost, "/2fa/enable", `{"code":"`+code+`"}`, "")
	require.Equal(t, "TWOFA_SETUP_INVALID", result["code"], "another session cannot finish this setup")
	_, result = h.do(http.MethodPost, "/2fa/enable", `{"code":"`+code+`"}`, "")
	require.Equal(t, true, result["success"], result)

	// With 2FA enabled the password no longer authorizes security changes.
	_, result = h.do(http.MethodPost, "/verify", `{"method":"password","scope":"2fa.disable","password":"new-password-2"}`, "")
	require.Equal(t, "SECURITY_PROOF_METHOD_MISMATCH", result["code"])

	_, result = h.do(http.MethodPost, "/2fa/backup_codes", "", "")
	require.Equal(t, "SECURITY_PROOF_REQUIRED", result["code"])
	nextCode, err := totp.GenerateCode(secret, time.Now().Add(30*time.Second))
	require.NoError(t, err)
	backupProof := h.proof(`{"method":"2fa","scope":"2fa.backup_codes.regenerate","code":"` + nextCode + `"}`)
	_, result = h.do(http.MethodPost, "/2fa/disable", "", backupProof)
	require.Equal(t, "SECURITY_PROOF_SCOPE_MISMATCH", result["code"])

	var actions []string
	require.NoError(t, db.Model(&model.AuditLog{}).Order("id").Pluck("action", &actions).Error)
	require.Contains(t, actions, "user.password_change")
	require.Contains(t, actions, "user.2fa_setup")
	require.Contains(t, actions, "user.2fa_enable")
}

// Replacing an existing email on an account without 2FA or a Passkey also needs
// the code sent to the current address, checked before the proof is spent.
func TestReplacingEmailConfirmsTheOldAddress(t *testing.T) {
	db := useStepUpTestDB(t)
	hash, err := common.Password2Hash("email-password-1")
	require.NoError(t, err)
	user := model.User{Username: "email-swap", Password: hash, Email: "old@example.com", AffCode: "emsw01", Role: common.RoleCommonUser, Status: common.UserStatusEnabled}
	require.NoError(t, db.Create(&user).Error)
	h := newStepUpHarness(t, &user)
	h.engine.POST("/email/bind", func(c *ginContext) { asStepUpUser(t, c); EmailBind(c) })

	require.NoError(t, common.RegisterVerificationCodeWithKey("new@example.com", "111111", common.EmailBindVerificationPurpose(user.Id)))
	require.NoError(t, common.RegisterVerificationCodeWithKey("old@example.com", "222222", common.EmailBindOldAddressPurpose(user.Id)))
	proofFor := func() string {
		return h.proof(`{"method":"password","scope":"account.binding.bind","context":{"provider":"email","email":"new@example.com"},"password":"email-password-1"}`)
	}

	proof := proofFor()
	_, result := h.do(http.MethodPost, "/email/bind", `{"email":"new@example.com","code":"111111","old_code":"999999"}`, proof)
	require.Equal(t, "EMAIL_OLD_CODE_INVALID", result["code"])
	// The wrong old code was rejected before the proof was consumed.
	_, result = h.do(http.MethodPost, "/email/bind", `{"email":"new@example.com","code":"111111","old_code":"222222"}`, proof)
	require.Equal(t, true, result["success"], result)

	var stored model.User
	require.NoError(t, db.First(&stored, user.Id).Error)
	require.Equal(t, "new@example.com", stored.Email)
}

func TestUnbindingTheOnlySignInMethodIsRefused(t *testing.T) {
	db := useStepUpTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.UserOAuthBinding{}))
	user := model.User{Username: "last-method", AffCode: "lstm01", Role: common.RoleCommonUser, Status: common.UserStatusEnabled}
	require.NoError(t, db.Create(&user).Error)
	require.NoError(t, db.Model(&user).Update("password", "").Error)
	require.NoError(t, db.Create(&model.UserOAuthBinding{UserId: user.Id, ProviderId: 9, ProviderUserId: "only"}).Error)
	h := newStepUpHarness(t, &user)
	h.engine.DELETE("/bindings/:provider_id", func(c *ginContext) { asStepUpUser(t, c); UnbindCustomOAuth(c) })

	_, result := h.do(http.MethodDelete, "/bindings/9", "", "")
	require.Equal(t, "LAST_LOGIN_METHOD", result["code"], "checked before any proof is required")
	count, err := model.OtherSignInMethodCount(user.Id, 0)
	require.NoError(t, err)
	require.Equal(t, 1, count)
}
