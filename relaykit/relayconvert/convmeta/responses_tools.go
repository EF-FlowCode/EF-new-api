package convmeta

// ResponsesToolState records how Responses-only tool definitions were encoded
// for the upstream protocol, so the matching response can be restored.
type ResponsesToolState struct {
	// CustomToolNames lists Responses custom (freeform) tools that were sent
	// upstream as function tools taking one string "input" argument. Function
	// calls with these names are custom tool calls.
	CustomToolNames map[string]struct{}
}

// CustomToolInputArgument is the single function argument that carries a
// Responses custom tool input through an upstream function call.
const CustomToolInputArgument = "input"

// IsCustomTool reports whether name was encoded from a Responses custom tool.
func (s *ResponsesToolState) IsCustomTool(name string) bool {
	if s == nil {
		return false
	}
	_, ok := s.CustomToolNames[name]
	return ok
}

// ResponsesToolStateCarrier is optional for embedders that do not convert custom tools.
type ResponsesToolStateCarrier interface {
	GetResponsesToolState() *ResponsesToolState
	SetResponsesToolState(*ResponsesToolState)
}

func ResponsesToolsOf(meta Meta) *ResponsesToolState {
	if carrier, ok := meta.(ResponsesToolStateCarrier); ok {
		return carrier.GetResponsesToolState()
	}
	return nil
}
func SetResponsesTools(meta Meta, state *ResponsesToolState) {
	if carrier, ok := meta.(ResponsesToolStateCarrier); ok {
		carrier.SetResponsesToolState(state)
	}
}
func (v *Values) GetResponsesToolState() *ResponsesToolState {
	if v == nil {
		return nil
	}
	return v.ResponsesTools
}
func (v *Values) SetResponsesToolState(state *ResponsesToolState) {
	if v != nil {
		v.ResponsesTools = state
	}
}
