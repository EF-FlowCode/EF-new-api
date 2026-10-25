package relayconvert

import (
	"encoding/json"
	"testing"

	"eflow.local/new-api/relaykit/dto"
	"eflow.local/new-api/relaykit/relayconvert/convmeta"
	"eflow.local/new-api/relaykit/types"
	"github.com/stretchr/testify/require"
)

func TestGeminiBudgetSurvivesChatResponsesRoundTrip(t *testing.T) {
	budget, visible := 4096, false
	req := &dto.GeminiChatRequest{GenerationConfig: dto.GeminiChatGenerationConfig{ThinkingConfig: &dto.GeminiThinkingConfig{ThinkingBudget: &budget, IncludeThoughts: &visible}}}
	meta := &convmeta.Values{OriginModelName: "gemini-2.5-flash", UpstreamModelName: "gemini-2.5-flash", ChannelMetaAttached: true}
	chat, err := GeminiGenerateContentRequestToOpenAIChat(req, meta)
	require.NoError(t, err)
	responses, err := ChatCompletionsRequestToResponsesRequest(chat)
	require.NoError(t, err)
	encoded, err := json.Marshal(responses)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "ReasoningConversion")
	require.NotContains(t, string(encoded), "BudgetTokens")
	require.Empty(t, responses.Reasoning.Summary)
	chat, err = ResponsesRequestToChatCompletionsRequest(responses)
	require.NoError(t, err)
	gemini, err := OpenAIChatRequestToGeminiGenerateContent(t.Context(), *chat, meta)
	require.NoError(t, err)
	require.Equal(t, budget, *gemini.GenerationConfig.ThinkingConfig.ThinkingBudget)
	require.NotNil(t, gemini.GenerationConfig.ThinkingConfig.IncludeThoughts)
	require.False(t, *gemini.GenerationConfig.ThinkingConfig.IncludeThoughts)
	gemini, err = OpenAIResponsesRequestToGeminiChat(t.Context(), responses, meta)
	require.NoError(t, err)
	require.Equal(t, budget, *gemini.GenerationConfig.ThinkingConfig.ThinkingBudget)
	require.False(t, *gemini.GenerationConfig.ThinkingConfig.IncludeThoughts)
}

func TestGeminiUpstreamDefaultsAndBudgetValidation(t *testing.T) {
	for _, tc := range []struct {
		model, effort string
		budget        int
	}{
		{"gemini-2.5-flash", "high", -1},
		{"gemini-2.5-flash-lite", "none", 0},
		{"gemini-3.5-flash", "medium", 0},
	} {
		t.Run(tc.model, func(t *testing.T) {
			meta := &convmeta.Values{OriginModelName: tc.model, UpstreamModelName: "gpt-target", ChannelMetaAttached: true}
			out, err := GeminiGenerateContentRequestToOpenAIChat(&dto.GeminiChatRequest{}, meta)
			require.NoError(t, err)
			require.Equal(t, tc.effort, out.ReasoningEffort)
			if tc.budget == -1 {
				require.Equal(t, -1, *out.ReasoningConversion.BudgetTokens)
			}
		})
	}
	meta := &convmeta.Values{OriginModelName: "gemini-2.5-flash", UpstreamModelName: "gemini-2.5-flash", ChannelMetaAttached: true, ConversionChain: []types.RelayFormat{types.RelayFormatOpenAI}}
	for _, body := range []string{
		`{"google":{"thinking_config":{"thinking_budget":3.5}}}`,
		`{"google":{"thinking_config":{"thinking_budget":-2}}}`,
		`{"google":{"thinking_config":{"thinking_budget":999999}}}`,
	} {
		_, err := OpenAIChatRequestToGeminiGenerateContent(t.Context(), dto.GeneralOpenAIRequest{Model: meta.UpstreamModelName, ExtraBody: []byte(body)}, meta)
		require.Error(t, err)
	}
}

func TestGeminiExplicitFalseSurvivesJSON(t *testing.T) {
	for _, input := range []string{`{"includeThoughts":false}`, `{"include_thoughts":false}`} {
		var config dto.GeminiThinkingConfig
		require.NoError(t, json.Unmarshal([]byte(input), &config))
		require.NotNil(t, config.IncludeThoughts)
		encoded, err := json.Marshal(config)
		require.NoError(t, err)
		require.JSONEq(t, `{"includeThoughts":false}`, string(encoded))
	}
}
