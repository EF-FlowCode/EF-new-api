package constant

// eflow: inbound endpoint capability by channel type.
//
// Channel selection used to treat every ordinary channel as able to serve every
// inbound API surface; a request whose protocol the adaptor cannot convert
// (for example /v1/responses on an Anthropic channel) then failed with a
// skip-retry conversion error instead of going to a channel that supports it.
//
// The table lists only channel types whose relay adaptor rejects some inbound
// surfaces. Types without an entry keep the legacy allow-all behavior, and
// endpoint types outside requestEndpointCapabilityGoverned are never filtered.
// relay/channel_endpoint_capability_test.go keeps the table aligned with the
// adaptors' Convert* implementations.

var requestEndpointCapabilityGoverned = map[EndpointType]struct{}{
	EndpointTypeOpenAI:                {},
	EndpointTypeOpenAIResponse:        {},
	EndpointTypeOpenAIResponseCompact: {},
	EndpointTypeAnthropic:             {},
	EndpointTypeGemini:                {},
	EndpointTypeJinaRerank:            {},
	EndpointTypeImageGeneration:       {},
	EndpointTypeEmbeddings:            {},
}

var channelTypeRequestEndpoints = map[int][]EndpointType{
	ChannelTypeAnthropic: {EndpointTypeOpenAI, EndpointTypeAnthropic},
	ChannelTypeAws:       {EndpointTypeOpenAI, EndpointTypeAnthropic},
	ChannelTypeGemini: {
		EndpointTypeOpenAI, EndpointTypeAnthropic, EndpointTypeGemini,
		EndpointTypeEmbeddings, EndpointTypeImageGeneration,
	},
	ChannelTypeVertexAi: {
		EndpointTypeOpenAI, EndpointTypeAnthropic, EndpointTypeGemini,
		EndpointTypeImageGeneration,
	},
	ChannelTypeAli: {
		EndpointTypeOpenAI, EndpointTypeOpenAIResponse, EndpointTypeOpenAIResponseCompact,
		EndpointTypeAnthropic, EndpointTypeEmbeddings, EndpointTypeJinaRerank,
		EndpointTypeImageGeneration,
	},
	// Codex serves Chat and Messages only through the global Chat→Responses
	// policy; model.ChannelSupportsRequestPath applies that policy.
	ChannelTypeCodex: {EndpointTypeOpenAIResponse, EndpointTypeOpenAIResponseCompact},
}

// ChannelTypeRequestEndpoints returns the inbound endpoint types the channel
// type's adaptor can serve, and false when the type has no capability entry.
func ChannelTypeRequestEndpoints(channelType int) ([]EndpointType, bool) {
	endpoints, ok := channelTypeRequestEndpoints[channelType]
	return endpoints, ok
}

// IsCapabilityGovernedEndpoint reports whether automatic capability filtering
// applies to endpointType.
func IsCapabilityGovernedEndpoint(endpointType EndpointType) bool {
	_, ok := requestEndpointCapabilityGoverned[endpointType]
	return ok
}

// ChannelTypeSupportsRequestEndpoint reports whether the channel type's adaptor
// can serve endpointType. Ungoverned endpoints and types without an entry are
// always supported.
func ChannelTypeSupportsRequestEndpoint(channelType int, endpointType EndpointType) bool {
	if !IsCapabilityGovernedEndpoint(endpointType) {
		return true
	}
	endpoints, ok := channelTypeRequestEndpoints[channelType]
	if !ok {
		return true
	}
	for _, endpoint := range endpoints {
		if endpoint == endpointType {
			return true
		}
	}
	return false
}
