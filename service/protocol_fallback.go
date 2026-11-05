package service

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"eflow.local/new-api/setting/model_setting"
	"eflow.local/new-api/types"
)

// eflow: protocol routing.

// ProtocolFailureAction is what to do after a native-protocol attempt fails.
type ProtocolFailureAction int

const (
	// ProtocolFailureNextChannel keeps the legacy retry rules.
	ProtocolFailureNextChannel ProtocolFailureAction = iota
	// ProtocolFailureChatFallback retries once on the same channel through Chat.
	ProtocolFailureChatFallback
	// ProtocolFailureReturn returns the error to the client: neither another
	// protocol nor another channel can fix it.
	ProtocolFailureReturn
)

// Reason labels recorded in protocol_attempts.
const (
	ProtocolReasonEndpointMissing = "endpoint_missing"
	ProtocolReasonFormatError     = "format_error"
	ProtocolReasonUpstreamError   = "upstream_error"
	ProtocolReasonStreamAborted   = "stream_aborted"
	ProtocolReasonAuthOrQuota     = "auth_or_quota"
	ProtocolReasonRateLimited     = "rate_limited"
	ProtocolReasonTimeout         = "timeout"
	ProtocolReasonContextLength   = "context_length"
	ProtocolReasonContentPolicy   = "content_policy"
	ProtocolReasonOther           = "other"
)

// ClassifyProtocolFailure decides whether a failed native attempt may fall back
// to Chat on the same channel. When several categories match, the most
// conservative one wins: return > next channel > fallback.
func ClassifyProtocolFailure(apiErr *types.NewAPIError, settings model_setting.ProtocolRoutingSettings) (ProtocolFailureAction, string) {
	if apiErr == nil {
		return ProtocolFailureNextChannel, ProtocolReasonOther
	}
	message := strings.ToLower(apiErr.Error())
	if containsAnyKeyword(message, settings.ContextLengthKeywords) {
		return ProtocolFailureReturn, ProtocolReasonContextLength
	}
	if containsAnyKeyword(message, settings.ContentPolicyKeywords) ||
		apiErr.GetErrorCode() == types.ErrorCodeSensitiveWordsDetected ||
		apiErr.GetErrorCode() == types.ErrorCodePromptBlocked {
		return ProtocolFailureReturn, ProtocolReasonContentPolicy
	}

	code := apiErr.StatusCode
	switch {
	case isProtocolTimeout(apiErr):
		return ProtocolFailureNextChannel, ProtocolReasonTimeout
	case code == http.StatusUnauthorized || code == http.StatusForbidden || code == http.StatusPaymentRequired:
		return ProtocolFailureNextChannel, ProtocolReasonAuthOrQuota
	case code == http.StatusTooManyRequests:
		return ProtocolFailureNextChannel, ProtocolReasonRateLimited
	case code == http.StatusNotFound || code == http.StatusMethodNotAllowed ||
		code == http.StatusUnsupportedMediaType || code == http.StatusNotImplemented:
		return ProtocolFailureChatFallback, ProtocolReasonEndpointMissing
	case code == http.StatusBadRequest:
		if containsAnyKeyword(message, settings.FormatErrorKeywords) {
			return ProtocolFailureChatFallback, ProtocolReasonFormatError
		}
		return ProtocolFailureNextChannel, ProtocolReasonOther
	case code == http.StatusServiceUnavailable && apiErr.GetErrorCode() == types.ErrorCodeBadResponse:
		// StreamAbortRetryError: the stream failed before any output.
		return ProtocolFailureChatFallback, ProtocolReasonStreamAborted
	case code >= 500 && code <= 599:
		return ProtocolFailureChatFallback, ProtocolReasonUpstreamError
	default:
		return ProtocolFailureNextChannel, ProtocolReasonOther
	}
}

func isProtocolTimeout(apiErr *types.NewAPIError) bool {
	switch apiErr.StatusCode {
	case http.StatusGatewayTimeout, 524, http.StatusRequestTimeout:
		return true
	}
	if apiErr.GetErrorCode() == types.ErrorCodeChannelResponseTimeExceeded {
		return true
	}
	if errors.Is(apiErr, context.DeadlineExceeded) {
		return true
	}
	message := strings.ToLower(apiErr.Error())
	return strings.Contains(message, "timeout") || strings.Contains(message, "deadline exceeded")
}

func containsAnyKeyword(message string, keywords []string) bool {
	for _, keyword := range keywords {
		keyword = strings.ToLower(strings.TrimSpace(keyword))
		if keyword != "" && strings.Contains(message, keyword) {
			return true
		}
	}
	return false
}

// IsClientRestrictedMessage reports whether an upstream rejection names a
// client whitelist (e.g. Claude Code only).
func IsClientRestrictedMessage(message string, settings model_setting.ProtocolRoutingSettings) bool {
	return containsAnyKeyword(strings.ToLower(message), settings.ClientRestrictedKeywords)
}
