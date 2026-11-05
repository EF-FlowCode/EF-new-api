package constant

// eflow: protocol routing.
//
// A channel may declare which upstream protocols the administrator allows it
// to use. Requests whose protocol is declared go upstream unchanged; other
// requests fall back to Chat Completions on the same channel. Undeclared
// channels keep the legacy adaptor behavior and only take part in ordering.

const (
	UpstreamProtocolMessages  = "messages"
	UpstreamProtocolChat      = "chat"
	UpstreamProtocolResponses = "responses"
)

// RequestPathUpstreamProtocol maps a canonical relay path to the protocol
// governed by protocol routing, or "" for paths outside its scope.
func RequestPathUpstreamProtocol(requestPath string) string {
	switch requestPath {
	case "/v1/messages":
		return UpstreamProtocolMessages
	case "/v1/chat/completions":
		return UpstreamProtocolChat
	case "/v1/responses":
		return UpstreamProtocolResponses
	default:
		return ""
	}
}

// IsUpstreamProtocol reports whether value names a governed protocol.
func IsUpstreamProtocol(value string) bool {
	switch value {
	case UpstreamProtocolMessages, UpstreamProtocolChat, UpstreamProtocolResponses:
		return true
	default:
		return false
	}
}

// SupportsProtocolRouting reports whether declared protocols apply to the
// channel type. Only types whose upstream endpoints sit at base_url/v1/... are
// in scope; the others keep their adaptor-specific URL and auth handling.
func SupportsProtocolRouting(channelType int) bool {
	switch channelType {
	case ChannelTypeOpenAI, ChannelTypeAnthropic:
		return true
	default:
		return false
	}
}

// legacyNativeUpstreamProtocols lists the protocols each type's legacy adaptor
// forwards without conversion. It orders undeclared channels and decides
// whether a declared native protocol can keep using the legacy adaptor.
var legacyNativeUpstreamProtocols = map[int][]string{
	ChannelTypeOpenAI:    {UpstreamProtocolChat, UpstreamProtocolResponses},
	ChannelTypeAnthropic: {UpstreamProtocolMessages},
}

// legacyConversionUpstreamProtocols is the protocol a type's legacy adaptor
// converts other inbound protocols into, where that is not Chat. A channel that
// declares it but not Chat keeps the adaptor's conversion as its fallback: an
// Anthropic channel that only speaks Messages still serves Chat requests.
var legacyConversionUpstreamProtocols = map[int]string{
	ChannelTypeAnthropic: UpstreamProtocolMessages,
}

// LegacyConversionUpstreamProtocol returns the non-Chat protocol the channel
// type's legacy adaptor converts requests into, or "".
func LegacyConversionUpstreamProtocol(channelType int) string {
	return legacyConversionUpstreamProtocols[channelType]
}

// LegacyNativeUpstreamProtocol reports whether the channel type's legacy
// adaptor forwards protocol unchanged. known is false for types without an
// entry, whose behavior is not reordered.
func LegacyNativeUpstreamProtocol(channelType int, protocol string) (native bool, known bool) {
	protocols, ok := legacyNativeUpstreamProtocols[channelType]
	if !ok {
		return false, false
	}
	for _, p := range protocols {
		if p == protocol {
			return true, true
		}
	}
	return false, true
}
