package model

import "errors"

// Common errors
var (
	ErrDatabase = errors.New("database error")
)

// User auth errors
var (
	ErrInvalidCredentials   = errors.New("invalid credentials")
	ErrUserEmptyCredentials = errors.New("empty credentials")
	ErrEmailAlreadyTaken    = errors.New("email already taken")
	ErrEmailNotFound        = errors.New("email not found")
	ErrEmailAmbiguous       = errors.New("email matches multiple users")
)

// Token auth errors
var (
	ErrTokenNotProvided = errors.New("token not provided")
	ErrTokenInvalid     = errors.New("token invalid")
)

// Redemption errors
var ErrRedeemFailed = errors.New("redeem.failed")

// ErrRedemptionAlreadyUsed rejects edits to a code that has been redeemed.
var ErrRedemptionAlreadyUsed = errors.New("redemption code has been used")

// 2FA errors
var ErrTwoFANotEnabled = errors.New("2fa not enabled")
var ErrTwoFAAlreadyEnabled = errors.New("2fa already enabled")

// Invitation code errors
var (
	ErrInvitationCodeRequired             = errors.New("invitation_code.required")
	ErrInvitationCodeInvalid              = errors.New("invitation_code.invalid")
	ErrInvitationCodeExhausted            = errors.New("invitation_code.exhausted")
	ErrInvitationCodeExpired              = errors.New("invitation_code.expired")
	ErrInvitationCodeDisabled             = errors.New("invitation_code.disabled")
	ErrInvitationCodeQuotaExceeded        = errors.New("invitation_code.quota_exceeded")
	ErrInvitationCodeMonthlyQuotaExceeded = errors.New("invitation_code.monthly_quota_exceeded")
	ErrInvitationCodeAccountTooYoung      = errors.New("invitation_code.account_too_young")
	ErrInvitationCodeTopupNotEnough       = errors.New("invitation_code.topup_not_enough")
	ErrInvitationCodeUserGenerateDisabled = errors.New("invitation_code.user_generate_disabled")
)
