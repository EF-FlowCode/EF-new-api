package relayconvert

import (
	"encoding/json"
	"testing"

	"eflow.local/new-api/relaykit/dto"
	"eflow.local/new-api/relaykit/relayconvert/convmeta"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func geminiJSONSchemaMeta(supported bool) *convmeta.Values {
	return &convmeta.Values{
		OriginModelName: "gemini-3.7-flash", UpstreamModelName: "gemini-3.7-flash", ChannelMetaAttached: true,
		Options: &convmeta.Options{Gemini: convmeta.GeminiOptions{
			SupportsJSONSchema: func(string) bool { return supported },
		}},
	}
}

// TestChatToGeminiKeepsJSONSchemaConstraints covers Chat tools and structured
// output converted for Gemini: a schema the typed Schema cannot hold goes in
// parametersJsonSchema / responseJsonSchema where the upstream accepts them,
// and is cleaned as before where it does not (Gemini API v1).
func TestChatToGeminiKeepsJSONSchemaConstraints(t *testing.T) {
	params := map[string]any{"type": "object", "properties": map[string]any{"mode": map[string]any{"const": "fast"}}}
	responseFormat := &dto.ResponseFormat{Type: "json_schema", JsonSchema: json.RawMessage(`{"name":"o","schema":{"type":["object","null"]}}`)}
	request := dto.GeneralOpenAIRequest{
		Model:          "gemini-3.7-flash",
		Messages:       []dto.Message{{Role: "user", Content: "hi"}},
		Tools:          []dto.ToolCallRequest{{Type: "function", Function: dto.FunctionRequest{Name: "f", Parameters: params}}},
		ResponseFormat: responseFormat,
	}

	out, err := OpenAIChatRequestToGeminiGenerateContent(t.Context(), request, geminiJSONSchemaMeta(true))
	require.NoError(t, err)
	encoded, err := json.Marshal(out)
	require.NoError(t, err)
	require.Equal(t, "fast", gjson.GetBytes(encoded, "tools.0.functionDeclarations.0.parametersJsonSchema.properties.mode.const").String(), string(encoded))
	require.False(t, gjson.GetBytes(encoded, "tools.0.functionDeclarations.0.parameters").Exists())
	require.Equal(t, `["object","null"]`, gjson.GetBytes(encoded, "generationConfig.responseJsonSchema.type").Raw)
	require.False(t, gjson.GetBytes(encoded, "generationConfig.responseSchema").Exists())

	out, err = OpenAIChatRequestToGeminiGenerateContent(t.Context(), request, geminiJSONSchemaMeta(false))
	require.NoError(t, err)
	encoded, err = json.Marshal(out)
	require.NoError(t, err)
	require.False(t, gjson.GetBytes(encoded, "tools.0.functionDeclarations.0.parametersJsonSchema").Exists())
	require.Equal(t, "OBJECT", gjson.GetBytes(encoded, "tools.0.functionDeclarations.0.parameters.type").String())
	require.False(t, gjson.GetBytes(encoded, "generationConfig.responseJsonSchema").Exists())
}

func TestResponsesToGeminiKeepsJSONSchemaConstraints(t *testing.T) {
	request := &dto.OpenAIResponsesRequest{
		Model: "gemini-3.7-flash",
		Input: json.RawMessage(`"hi"`),
		Tools: json.RawMessage(`[{"type":"function","name":"f","parameters":{"type":"object","properties":{"v":{"oneOf":[{"type":"string"},{"type":"integer"}]}}}}]`),
		Text:  json.RawMessage(`{"format":{"type":"json_schema","name":"o","schema":{"type":"object","properties":{"n":{"enum":[1,2]}}}}}`),
	}
	out, err := OpenAIResponsesRequestToGeminiChat(t.Context(), request, geminiJSONSchemaMeta(true))
	require.NoError(t, err)
	encoded, err := json.Marshal(out)
	require.NoError(t, err)
	require.True(t, gjson.GetBytes(encoded, "tools.0.functionDeclarations.0.parametersJsonSchema.properties.v.oneOf").Exists(), string(encoded))
	require.Equal(t, `[1,2]`, gjson.GetBytes(encoded, "generationConfig.responseJsonSchema.properties.n.enum").Raw)
}
