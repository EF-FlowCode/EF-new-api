package controller

import (
	"testing"

	"eflow.local/new-api/constant"
	"eflow.local/new-api/dto"
	"github.com/stretchr/testify/require"
)

func TestBuildTestRequestUsesNativeClaudeAndGeminiShapes(t *testing.T) {
	claude, ok := buildTestRequest("claude-test", string(constant.EndpointTypeAnthropic), nil, true).(*dto.ClaudeRequest)
	require.True(t, ok)
	require.Equal(t, "claude-test", claude.Model)
	require.NotNil(t, claude.Stream)
	require.True(t, *claude.Stream)
	require.NotNil(t, claude.MaxTokens)
	require.Len(t, claude.Messages, 1)
	require.Equal(t, "user", claude.Messages[0].Role)

	gemini, ok := buildTestRequest("gemini-test", string(constant.EndpointTypeGemini), nil, false).(*dto.GeminiChatRequest)
	require.True(t, ok)
	require.Len(t, gemini.Contents, 1)
	require.Equal(t, "user", gemini.Contents[0].Role)
	require.Len(t, gemini.Contents[0].Parts, 1)
	require.Equal(t, "hi", gemini.Contents[0].Parts[0].Text)
	require.NotNil(t, gemini.GenerationConfig.MaxOutputTokens)
}

func TestBuildTestRequestKeepsOpenAIChatShape(t *testing.T) {
	request, ok := buildTestRequest("gpt-test", string(constant.EndpointTypeOpenAI), nil, false).(*dto.GeneralOpenAIRequest)
	require.True(t, ok)
	require.Equal(t, "gpt-test", request.Model)
	require.Len(t, request.Messages, 1)
	require.Equal(t, "user", request.Messages[0].Role)
}

func TestBuildTestRequestUsesModelCapabilities(t *testing.T) {
	for _, model := range []string{"gpt-6-sol", "gpt-6-luna-2026-09-30", "gpt-6-astra", "gpt-5.2", "o3"} {
		req := buildTestRequest(model, string(constant.EndpointTypeOpenAI), nil, false).(*dto.GeneralOpenAIRequest)
		require.NotNil(t, req.MaxCompletionTokens, model)
		require.Nil(t, req.MaxTokens, model)
	}
}
