package dto

import (
	"strings"

	"eflow.local/new-api/constant"
)

// eflow: protocol routing probe results.

const (
	UpstreamProtocolVerdictSupported   = "supported"
	UpstreamProtocolVerdictUnsupported = "unsupported"
	UpstreamProtocolVerdictUnknown     = "unknown"
)

// UpstreamProtocolProbeAttempt is one probe request against the upstream.
type UpstreamProtocolProbeAttempt struct {
	Stream           bool   `json:"stream"`
	StatusCode       int    `json:"status_code"`
	DurationMs       int64  `json:"duration_ms"`
	OK               bool   `json:"ok"`
	ClientRestricted bool   `json:"client_restricted,omitempty"`
	// AnswerOK reports whether the reply solved the random question. A wrong
	// answer with a valid structure suggests a canned upstream reply.
	AnswerOK bool `json:"answer_ok"`
	Error            string `json:"error,omitempty"`
}

// UpstreamProtocolProbeItem is the verdict for one protocol.
type UpstreamProtocolProbeItem struct {
	Protocol string                         `json:"protocol"`
	Verdict  string                         `json:"verdict"`
	Reason   string                         `json:"reason,omitempty"`
	Attempts []UpstreamProtocolProbeAttempt `json:"attempts,omitempty"`
	// TrafficCount is the number of native successful requests on this
	// channel in the evidence window. It is reference information only.
	TrafficCount int64 `json:"traffic_count"`
}

// UpstreamProtocolProbeResult is the last probe of a channel. It is shown to
// the administrator for confirmation and never changes routing by itself.
type UpstreamProtocolProbeResult struct {
	ProbedAt int64                       `json:"probed_at"`
	Model    string                      `json:"model"`
	Items    []UpstreamProtocolProbeItem `json:"items"`
}

// NormalizeUpstreamProtocols lowercases, de-duplicates and drops unknown
// values, keeping the canonical messages/chat/responses order.
func NormalizeUpstreamProtocols(values []string) []string {
	seen := make(map[string]bool, len(values))
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if constant.IsUpstreamProtocol(value) {
			seen[value] = true
		}
	}
	result := make([]string, 0, len(seen))
	for _, protocol := range []string{constant.UpstreamProtocolMessages, constant.UpstreamProtocolChat, constant.UpstreamProtocolResponses} {
		if seen[protocol] {
			result = append(result, protocol)
		}
	}
	return result
}

// DeclaresUpstreamProtocols reports whether the administrator confirmed a
// protocol set for the channel.
func (s *ChannelOtherSettings) DeclaresUpstreamProtocols() bool {
	return s != nil && len(NormalizeUpstreamProtocols(s.UpstreamProtocols)) > 0
}

// AllowsUpstreamProtocol reports whether protocol is in the confirmed set.
func (s *ChannelOtherSettings) AllowsUpstreamProtocol(protocol string) bool {
	if s == nil {
		return false
	}
	for _, declared := range NormalizeUpstreamProtocols(s.UpstreamProtocols) {
		if declared == protocol {
			return true
		}
	}
	return false
}
