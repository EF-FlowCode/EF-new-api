package service

import (
	"errors"
	"net/http"
	"testing"

	"eflow.local/new-api/setting/model_setting"
	"eflow.local/new-api/types"
	"github.com/stretchr/testify/require"
)

func TestClassifyProtocolFailure(t *testing.T) {
	settings := model_setting.DefaultProtocolRoutingSettings()
	cases := []struct {
		name    string
		status  int
		message string
		code    types.ErrorCode
		action  ProtocolFailureAction
		reason  string
	}{
		{"endpoint missing", http.StatusNotFound, "Invalid URL (POST /v1/messages)", "", ProtocolFailureChatFallback, ProtocolReasonEndpointMissing},
		{"not implemented", http.StatusNotImplemented, "nope", "", ProtocolFailureChatFallback, ProtocolReasonEndpointMissing},
		{"format 400", http.StatusBadRequest, "unknown field: system", "", ProtocolFailureChatFallback, ProtocolReasonFormatError},
		{"plain 400", http.StatusBadRequest, "model is required", "", ProtocolFailureNextChannel, ProtocolReasonOther},
		{"upstream new-api 500", http.StatusInternalServerError, "field messages is required", "", ProtocolFailureChatFallback, ProtocolReasonUpstreamError},
		{"stream aborted", http.StatusServiceUnavailable, "stream failed before sending data", types.ErrorCodeBadResponse, ProtocolFailureChatFallback, ProtocolReasonStreamAborted},
		{"client restricted", http.StatusForbidden, "请使用标准 Claude Code 客户端请求", "", ProtocolFailureNextChannel, ProtocolReasonAuthOrQuota},
		{"rate limited", http.StatusTooManyRequests, "slow down", "", ProtocolFailureNextChannel, ProtocolReasonRateLimited},
		{"gateway timeout", http.StatusGatewayTimeout, "upstream", "", ProtocolFailureNextChannel, ProtocolReasonTimeout},
		{"cloudflare 524", 524, "a timeout occurred", "", ProtocolFailureNextChannel, ProtocolReasonTimeout},
		{"context length", http.StatusBadRequest, "prompt is too long: 250000 tokens", "", ProtocolFailureReturn, ProtocolReasonContextLength},
		// Conflicts resolve to the most conservative category.
		{"context length beats 404", http.StatusNotFound, "maximum context length exceeded", "", ProtocolFailureReturn, ProtocolReasonContextLength},
		{"content policy", http.StatusBadRequest, "blocked by content_filter", "", ProtocolFailureReturn, ProtocolReasonContentPolicy},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code := tc.code
			if code == "" {
				code = types.ErrorCodeBadResponseStatusCode
			}
			apiErr := types.NewErrorWithStatusCode(errors.New(tc.message), code, tc.status)
			action, reason := ClassifyProtocolFailure(apiErr, settings)
			require.Equal(t, tc.action, action)
			require.Equal(t, tc.reason, reason)
		})
	}
}

func TestIsClientRestrictedMessage(t *testing.T) {
	settings := model_setting.DefaultProtocolRoutingSettings()
	require.True(t, IsClientRestrictedMessage("We have detected an anomaly in your client. Please use the standard Claude Code client for requests.", settings))
	require.False(t, IsClientRestrictedMessage("invalid api key", settings))
}
