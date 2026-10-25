package service

import (
	"eflow.local/new-api/common"
	"eflow.local/new-api/constant"
	"eflow.local/new-api/setting"
	"eflow.local/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
)

// RelayRuntimePolicy is the immutable routing and channel-failure policy used
// by every selection and retry attempt in one relay request.
type RelayRuntimePolicy struct {
	AutoGroupPolicy      setting.AutoGroupPolicy
	ChannelFailurePolicy operation_setting.ChannelFailurePolicy
}

// CurrentRelayRuntimePolicy captures the current versions for callers that do
// not participate in a Gin request lifecycle.
func CurrentRelayRuntimePolicy() RelayRuntimePolicy {
	return RelayRuntimePolicy{
		AutoGroupPolicy:      setting.GetAutoGroupPolicy(),
		ChannelFailurePolicy: operation_setting.GetChannelFailurePolicy(),
	}
}

// GetOrCreateRelayRuntimePolicy pins one policy version to the request. Calls
// made by later middleware and retry loops receive the same version.
func GetOrCreateRelayRuntimePolicy(c *gin.Context) RelayRuntimePolicy {
	if c != nil {
		if policy, ok := common.GetContextKeyType[RelayRuntimePolicy](c, constant.ContextKeyRelayPolicy); ok {
			return cloneRelayRuntimePolicy(policy)
		}
	}
	policy := CurrentRelayRuntimePolicy()
	if c != nil {
		common.SetContextKey(c, constant.ContextKeyRelayPolicy, cloneRelayRuntimePolicy(policy))
	}
	return policy
}

func cloneRelayRuntimePolicy(policy RelayRuntimePolicy) RelayRuntimePolicy {
	policy.AutoGroupPolicy.Groups = append([]string(nil), policy.AutoGroupPolicy.Groups...)
	policy.ChannelFailurePolicy.AutomaticDisableKeywords = append([]string(nil), policy.ChannelFailurePolicy.AutomaticDisableKeywords...)
	policy.ChannelFailurePolicy.AutomaticDisableStatusCodes = append([]operation_setting.StatusCodeRange(nil), policy.ChannelFailurePolicy.AutomaticDisableStatusCodes...)
	policy.ChannelFailurePolicy.AutomaticRetryStatusCodes = append([]operation_setting.StatusCodeRange(nil), policy.ChannelFailurePolicy.AutomaticRetryStatusCodes...)
	return policy
}
