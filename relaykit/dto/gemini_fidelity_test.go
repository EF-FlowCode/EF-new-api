package dto

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGeminiDTOKeepsToolsAndGroundingFields(t *testing.T) {
	tools := `[{"googleMaps":{"enableWidget":true}},{"fileSearch":{"fileSearchStoreNames":["s1"]}},{"computerUse":{"environment":"ENVIRONMENT_BROWSER"}}]`
	var decodedTools []GeminiChatTool
	require.NoError(t, json.Unmarshal([]byte(tools), &decodedTools))
	out, err := json.Marshal(decodedTools)
	require.NoError(t, err)
	require.JSONEq(t, tools, string(out))

	grounding := `{"webSearchQueries":["q"],"groundingChunks":[{"web":{"uri":"https://example.com"}}],"groundingSupports":[{"segment":{"text":"a"}}],"searchEntryPoint":{"renderedContent":"<div/>"}}`
	var metadata GeminiGroundingMetadata
	require.NoError(t, json.Unmarshal([]byte(grounding), &metadata))
	out, err = json.Marshal(metadata)
	require.NoError(t, err)
	require.JSONEq(t, grounding, string(out))

	call := `{"id":"fc_1","name":"lookup","args":{"q":"x"},"partialArgs":[{"jsonPath":"$.q","stringValue":"x","willContinue":true}],"willContinue":true}`
	var functionCall FunctionCall
	require.NoError(t, json.Unmarshal([]byte(call), &functionCall))
	out, err = json.Marshal(functionCall)
	require.NoError(t, err)
	require.JSONEq(t, call, string(out))
}
