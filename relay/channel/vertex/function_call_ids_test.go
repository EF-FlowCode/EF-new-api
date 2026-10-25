package vertex

import (
	"encoding/json"
	"testing"

	"eflow.local/new-api/common"
	"eflow.local/new-api/dto"
	"github.com/stretchr/testify/require"
)

func TestRemoveFunctionCallIDsStripsBothSides(t *testing.T) {
	var request dto.GeminiChatRequest
	require.NoError(t, common.Unmarshal([]byte(`{"contents":[
		{"role":"model","parts":[{"functionCall":{"id":"call_1","name":"lookup","args":{"q":"x"}}}]},
		{"role":"user","parts":[{"functionResponse":{"id":"call_1","name":"lookup","response":{"ok":true}}}]}
	]}`), &request))
	require.Equal(t, "call_1", request.Contents[0].Parts[0].FunctionCall.ID, "the Gemini DTO keeps functionCall.id")

	removeFunctionCallIDs(&request)

	out, err := json.Marshal(request.Contents)
	require.NoError(t, err)
	require.NotContains(t, string(out), "call_1")
	require.Contains(t, string(out), `"name":"lookup"`)
}
