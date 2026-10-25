package gemini

import (
	"encoding/json"
	"testing"

	"eflow.local/new-api/relaykit/dto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFunctionParametersKeepsJSONSchemaConstraints(t *testing.T) {
	lossy := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"mode":  map[string]any{"const": "fast"},
			"level": map[string]any{"type": "integer", "enum": []any{1, 2}},
			"shape": map[string]any{"oneOf": []any{map[string]any{"$ref": "#/$defs/circle"}}},
		},
		"$defs": map[string]any{"circle": map[string]any{"type": "object"}},
	}

	field, value, misfits := FunctionParameters(lossy, true)
	assert.Equal(t, "parametersJsonSchema", field)
	assert.Equal(t, lossy, value, "the schema goes unchanged")
	assert.Empty(t, misfits)

	field, value, misfits = FunctionParameters(lossy, false)
	assert.Equal(t, "parameters", field)
	assert.Equal(t, []string{"$defs", "const", "non-string enum values", "oneOf"}, misfits)
	cleaned := value.(map[string]any)
	assert.NotContains(t, cleaned, "$defs")
	assert.Equal(t, "OBJECT", cleaned["type"])
}

func TestFunctionParametersTypedSchemaUnchangedByJSONSchemaSupport(t *testing.T) {
	// additionalProperties and $schema alone do not switch fields, so agent
	// tools and OpenAI strict schemas are sent as before.
	schema := map[string]any{
		"$schema":              "http://json-schema.org/draft-07/schema#",
		"type":                 "object",
		"additionalProperties": false,
		"properties":           map[string]any{"q": map[string]any{"type": "string"}},
		"required":             []any{"q"},
	}
	field, value, misfits := FunctionParameters(schema, true)
	assert.Equal(t, "parameters", field)
	assert.Empty(t, misfits)
	assert.Equal(t, CleanFunctionParameters(schema), value)

	field, value, _ = FunctionParameters(map[string]any{"type": "object", "properties": map[string]any{}}, true)
	assert.Equal(t, "parameters", field)
	assert.Nil(t, value, "empty object parameters are still left out")
}

func TestNewFunctionDeclarationEncoding(t *testing.T) {
	typed := NewFunctionDeclaration(dto.FunctionRequest{
		Name:       "lookup",
		Parameters: map[string]any{"type": "object", "properties": map[string]any{"q": map[string]any{"type": "string"}}},
	}, true)
	data, err := json.Marshal(typed)
	require.NoError(t, err)
	assert.JSONEq(t, `{"name":"lookup","parameters":{"type":"OBJECT","properties":{"q":{"type":"STRING"}}}}`, string(data))

	lossy := NewFunctionDeclaration(dto.FunctionRequest{
		Name:       "pick",
		Parameters: map[string]any{"type": "object", "properties": map[string]any{"v": map[string]any{"type": []any{"string", "number"}}}},
	}, true)
	data, err = json.Marshal(lossy)
	require.NoError(t, err)
	assert.JSONEq(t, `{"name":"pick","parametersJsonSchema":{"type":"object","properties":{"v":{"type":["string","number"]}}}}`, string(data))
}

func TestApplyResponseSchema(t *testing.T) {
	lossy := json.RawMessage(`{"name":"out","schema":{"type":"object","properties":{"v":{"type":["string","null"]},"n":{"enum":[1,2]}}}}`)

	var config dto.GeminiChatGenerationConfig
	ApplyResponseSchema(&config, lossy, true)
	assert.Nil(t, config.ResponseSchema)
	assert.JSONEq(t, `{"type":"object","properties":{"v":{"type":["string","null"]},"n":{"enum":[1,2]}}}`, string(config.ResponseJsonSchema))

	config = dto.GeminiChatGenerationConfig{}
	ApplyResponseSchema(&config, lossy, false)
	assert.Empty(t, config.ResponseJsonSchema)
	assert.NotNil(t, config.ResponseSchema)

	typed := json.RawMessage(`{"name":"out","strict":true,"schema":{"type":"object","additionalProperties":false,"properties":{"v":{"type":"string"}}}}`)
	config = dto.GeminiChatGenerationConfig{}
	ApplyResponseSchema(&config, typed, true)
	assert.Empty(t, config.ResponseJsonSchema, "a typed schema keeps responseSchema")
	assert.Equal(t, map[string]any{"type": "object", "properties": map[string]any{"v": map[string]any{"type": "string"}}}, config.ResponseSchema)
}
