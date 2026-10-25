package dto

import (
	"encoding/json"
	"testing"

	relaydto "eflow.local/new-api/relaykit/dto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRelayKitDTOAliases(t *testing.T) {
	var chatRequest *GeneralOpenAIRequest = &relaydto.GeneralOpenAIRequest{}
	var responsesRequest *OpenAIResponsesRequest = &relaydto.OpenAIResponsesRequest{}
	var chatResponse *OpenAITextResponse = &relaydto.OpenAITextResponse{}
	var responsesResponse *OpenAIResponsesResponse = &relaydto.OpenAIResponsesResponse{}
	var geminiRequest *GeminiChatRequest = &relaydto.GeminiChatRequest{}
	var geminiResponse *GeminiChatResponse = &relaydto.GeminiChatResponse{}
	var claudeRequest *ClaudeRequest = &relaydto.ClaudeRequest{}
	var claudeResponse *ClaudeResponse = &relaydto.ClaudeResponse{}

	assert.NotNil(t, chatRequest)
	assert.NotNil(t, responsesRequest)
	assert.NotNil(t, chatResponse)
	assert.NotNil(t, responsesResponse)
	assert.NotNil(t, geminiRequest)
	assert.NotNil(t, geminiResponse)
	assert.NotNil(t, claudeRequest)
	assert.NotNil(t, claudeResponse)
}

func TestGeminiChatResponseUsageMetadataPresence(t *testing.T) {
	var missing GeminiChatResponse
	require.NoError(t, json.Unmarshal([]byte(`{"candidates":[]}`), &missing))
	assert.False(t, missing.HasUsageMetadata)
	assert.Nil(t, missing.GetUsageMetadata())

	var empty GeminiChatResponse
	require.NoError(t, json.Unmarshal([]byte(`{"candidates":[],"usageMetadata":{}}`), &empty))
	assert.True(t, empty.HasUsageMetadata)
	require.NotNil(t, empty.GetUsageMetadata())
	assert.False(t, HasGeminiUsageMetadataTokens(empty.GetUsageMetadata()))
}

func TestResponsesRequestPreservesExtendedMetadataAndReasoning(t *testing.T) {
	var request OpenAIResponsesRequest
	require.NoError(t, json.Unmarshal([]byte(`{
		"model":"gpt-test",
		"client_metadata":{"session":"abc"},
		"reasoning":{"effort":"medium","mode":"enabled","context":{"window":128}}
	}`), &request))

	require.JSONEq(t, `{"session":"abc"}`, string(request.ClientMetadata))
	require.NotNil(t, request.Reasoning)
	require.JSONEq(t, `"enabled"`, string(request.Reasoning.Mode))
	require.JSONEq(t, `{"window":128}`, string(request.Reasoning.Context))
}

func TestProtocolUsageKeepsBillingUsageInternal(t *testing.T) {
	billing := NewOpenAIChatBillingUsage(&Usage{PromptTokens: 3})
	require.NotNil(t, billing)

	usage := Usage{BillingUsage: billing}
	claudeUsage := ClaudeUsage{BillingUsage: billing}
	geminiUsage := GeminiUsageMetadata{BillingUsage: billing}

	assert.Same(t, billing, usage.BillingUsage)
	assert.Same(t, billing, claudeUsage.BillingUsage)
	assert.Same(t, billing, geminiUsage.BillingUsage)

	for name, value := range map[string]any{
		"OpenAI": usage,
		"Claude": claudeUsage,
		"Gemini": geminiUsage,
	} {
		t.Run(name, func(t *testing.T) {
			encoded, err := json.Marshal(value)
			require.NoError(t, err)
			assert.NotContains(t, string(encoded), `"billing_usage"`)
		})
	}
}
