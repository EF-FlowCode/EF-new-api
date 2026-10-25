package operation_setting

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
)

// ChannelFailurePolicy is one immutable runtime version of channel disable
// and retry behavior. Callers making a multi-step decision should retain one
// returned value for the whole operation.
type ChannelFailurePolicy struct {
	AutomaticDisableChannelEnabled bool
	AutomaticEnableChannelEnabled  bool
	ChannelDisableThreshold        float64
	AutomaticDisableKeywords       []string
	AutomaticDisableStatusCodes    []StatusCodeRange
	RetryTimes                     int
	AutomaticRetryStatusCodes      []StatusCodeRange
}

var (
	channelFailurePolicy   atomic.Pointer[ChannelFailurePolicy]
	channelFailurePolicyMu sync.Mutex
)

func init() {
	publishChannelFailurePolicy(ChannelFailurePolicy{
		ChannelDisableThreshold:     5,
		AutomaticDisableKeywords:    defaultAutomaticDisableKeywords(),
		AutomaticDisableStatusCodes: []StatusCodeRange{{Start: 401, End: 401}},
		AutomaticRetryStatusCodes:   defaultAutomaticRetryStatusCodes(),
	})
}

// GetChannelFailurePolicy returns an isolated copy of one published version.
func GetChannelFailurePolicy() ChannelFailurePolicy {
	current := channelFailurePolicy.Load()
	if current == nil {
		return ChannelFailurePolicy{}
	}
	return cloneChannelFailurePolicy(*current)
}

// IsChannelFailurePolicyOptionKey reports whether a flat legacy option belongs
// to this snapshot and therefore must be published with the rest of the domain.
func IsChannelFailurePolicyOptionKey(key string) bool {
	switch key {
	case "AutomaticDisableChannelEnabled", "AutomaticEnableChannelEnabled",
		"ChannelDisableThreshold", "AutomaticDisableKeywords",
		"AutomaticDisableStatusCodes", "RetryTimes", "AutomaticRetryStatusCodes":
		return true
	default:
		return false
	}
}

// UpdateChannelFailurePolicy parses every supplied field before atomically
// publishing one complete replacement. Unknown keys are ignored.
func UpdateChannelFailurePolicy(values map[string]string) error {
	channelFailurePolicyMu.Lock()
	defer channelFailurePolicyMu.Unlock()

	next := GetChannelFailurePolicy()
	touched := false
	for key, value := range values {
		switch key {
		case "AutomaticDisableChannelEnabled":
			parsed, err := parseChannelFailurePolicyBool(key, value)
			if err != nil {
				return err
			}
			next.AutomaticDisableChannelEnabled = parsed
			touched = true
		case "AutomaticEnableChannelEnabled":
			parsed, err := parseChannelFailurePolicyBool(key, value)
			if err != nil {
				return err
			}
			next.AutomaticEnableChannelEnabled = parsed
			touched = true
		case "ChannelDisableThreshold":
			parsed, err := strconv.ParseFloat(value, 64)
			if err != nil || math.IsNaN(parsed) || math.IsInf(parsed, 0) || parsed < 0 {
				return fmt.Errorf("ChannelDisableThreshold must be a non-negative finite number")
			}
			next.ChannelDisableThreshold = parsed
			touched = true
		case "AutomaticDisableKeywords":
			next.AutomaticDisableKeywords = parseAutomaticDisableKeywords(value)
			touched = true
		case "AutomaticDisableStatusCodes":
			parsed, err := ParseHTTPStatusCodeRanges(value)
			if err != nil {
				return err
			}
			next.AutomaticDisableStatusCodes = parsed
			touched = true
		case "RetryTimes":
			parsed, err := strconv.Atoi(value)
			if err != nil || parsed < 0 || parsed > 10 {
				return fmt.Errorf("RetryTimes must be between 0 and 10")
			}
			next.RetryTimes = parsed
			touched = true
		case "AutomaticRetryStatusCodes":
			parsed, err := ParseHTTPStatusCodeRanges(value)
			if err != nil {
				return err
			}
			next.AutomaticRetryStatusCodes = parsed
			touched = true
		}
	}
	if touched {
		publishChannelFailurePolicy(next)
	}
	return nil
}

func (policy ChannelFailurePolicy) ShouldDisableByStatusCode(code int) bool {
	return shouldMatchStatusCodeRanges(policy.AutomaticDisableStatusCodes, code)
}

func (policy ChannelFailurePolicy) AutomaticDisableKeywordsString() string {
	return strings.Join(policy.AutomaticDisableKeywords, "\n")
}

func (policy ChannelFailurePolicy) AutomaticDisableStatusCodesString() string {
	return statusCodeRangesToString(policy.AutomaticDisableStatusCodes)
}

func (policy ChannelFailurePolicy) AutomaticRetryStatusCodesString() string {
	return statusCodeRangesToString(policy.AutomaticRetryStatusCodes)
}

func (policy ChannelFailurePolicy) ShouldRetryByStatusCode(code int) bool {
	if IsAlwaysSkipRetryStatusCode(code) {
		return false
	}
	return shouldMatchStatusCodeRanges(policy.AutomaticRetryStatusCodes, code)
}

func parseChannelFailurePolicyBool(key, value string) (bool, error) {
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return false, fmt.Errorf("%s must be a boolean: %w", key, err)
	}
	return parsed, nil
}

func defaultAutomaticDisableKeywords() []string {
	return []string{
		"Your credit balance is too low",
		"This organization has been disabled.",
		"You exceeded your current quota",
		"Permission denied",
		"The security token included in the request is invalid",
		"Operation not allowed",
		"Your account is not authorized",
	}
}

func parseAutomaticDisableKeywords(value string) []string {
	keywords := make([]string, 0)
	for keyword := range strings.SplitSeq(value, "\n") {
		keyword = strings.ToLower(strings.TrimSpace(keyword))
		if keyword != "" {
			keywords = append(keywords, keyword)
		}
	}
	return keywords
}

func cloneChannelFailurePolicy(policy ChannelFailurePolicy) ChannelFailurePolicy {
	policy.AutomaticDisableKeywords = append([]string(nil), policy.AutomaticDisableKeywords...)
	policy.AutomaticDisableStatusCodes = append([]StatusCodeRange(nil), policy.AutomaticDisableStatusCodes...)
	policy.AutomaticRetryStatusCodes = append([]StatusCodeRange(nil), policy.AutomaticRetryStatusCodes...)
	return policy
}

func publishChannelFailurePolicy(next ChannelFailurePolicy) {
	owned := cloneChannelFailurePolicy(next)
	channelFailurePolicy.Store(&owned)
}

func defaultAutomaticRetryStatusCodes() []StatusCodeRange {
	return []StatusCodeRange{
		{Start: 100, End: 199},
		{Start: 300, End: 399},
		{Start: 401, End: 407},
		{Start: 409, End: 499},
		{Start: 500, End: 503},
		{Start: 505, End: 523},
		{Start: 525, End: 599},
	}
}
