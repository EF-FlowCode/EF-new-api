package dto

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestResponsesOutputMarshalKeepsExplicitEmptyContentAndSummary(t *testing.T) {
	data, err := json.Marshal(ResponsesOutput{Type: "message", ID: "msg_1", Status: "in_progress", Role: "assistant", Content: []ResponsesOutputContent{}})
	require.NoError(t, err)
	require.JSONEq(t, `{"type":"message","id":"msg_1","status":"in_progress","role":"assistant","content":[]}`, string(data))

	data, err = json.Marshal(ResponsesOutput{Type: "reasoning", ID: "rs_1", Status: "in_progress", Summary: []ResponsesReasoningSummaryPart{}})
	require.NoError(t, err)
	require.JSONEq(t, `{"type":"reasoning","id":"rs_1","status":"in_progress","summary":[]}`, string(data))

	// summary is required on a reasoning item, so a nil summary is still sent as [].
	data, err = json.Marshal(ResponsesOutput{Type: "reasoning", ID: "rs_2", Status: "in_progress"})
	require.NoError(t, err)
	require.JSONEq(t, `{"type":"reasoning","id":"rs_2","status":"in_progress","summary":[]}`, string(data))

	// nil slices stay omitted so tool call items do not grow content/summary keys.
	data, err = json.Marshal(ResponsesOutput{Type: "function_call", ID: "fc_1", Status: "completed", CallId: "call_1", Name: "f", Arguments: json.RawMessage(`"{}"`)})
	require.NoError(t, err)
	require.Equal(t, `{"type":"function_call","id":"fc_1","status":"completed","call_id":"call_1","name":"f","arguments":"{}"}`, string(data))
}

// TestResponsesOutputJSONMirrorsResponsesOutput guards the custom marshaller
// against drift: every ResponsesOutput field must exist in the wire mirror
// with the same name, order and tag.
func TestResponsesOutputJSONMirrorsResponsesOutput(t *testing.T) {
	src := reflect.TypeOf(ResponsesOutput{})
	wire := reflect.TypeOf(responsesOutputJSON{})
	require.Equal(t, src.NumField(), wire.NumField())
	for i := 0; i < src.NumField(); i++ {
		require.Equal(t, src.Field(i).Name, wire.Field(i).Name)
		require.Equal(t, src.Field(i).Tag, wire.Field(i).Tag)
	}
}
