package relayconvert

import (
	"testing"

	"eflow.local/new-api/relaykit/dto"
	"eflow.local/new-api/relaykit/relayconvert/convmeta"
	"eflow.local/new-api/relaykit/types"
	"github.com/stretchr/testify/require"
)

func TestGeminiLevelConversionUsesSourceModel(t *testing.T) {
	for _, tc := range []struct {
		model, level, want, wantError string
	}{
		{"gemini-3.7-flash", " MeDiUm ", "medium", ""},
		{"gemini-3.7-flash", "MINIMAL", "minimal", ""},
		{"gemini-3.7-flash", "low", "low", ""},
		{"gemini-3.7-flash", "HIGH", "high", ""},
		{"gemini-3.7-flash", "ULTRA", "", "unsupported reasoning effort"},
		{"gemini-3-pro-preview", "MEDIUM", "", "not supported by model"},
		{"gemini-3-pro-preview", "LOW", "low", ""},
		{"gemini-3.1-pro-preview", "MINIMAL", "", "not supported by model"},
		{"gemini-pro-latest", "MEDIUM", "medium", ""},
		{"gemini-3.1-flash-image-preview", "LOW", "", "not supported by model"},
		{"gemini-3.1-flash-image-preview", "MINIMAL", "minimal", ""},
		{"gemini-3-pro-image-preview", "HIGH", "", "does not support configurable thinking"},
		{"gemini-2.5-pro", "HIGH", "", "requires thinkingBudget"},
		{"gemini-2.5-flash-preview-tts", "HIGH", "", "does not support configurable thinking"},
		{"custom-gemini", "MEDIUM", "medium", ""},
		{"gemini-3.7-flash", " ", "", "unsupported reasoning effort"},
	} {
		t.Run(tc.model+"/"+tc.level, func(t *testing.T) {
			info := &convmeta.Values{OriginModelName: tc.model, UpstreamModelName: "gpt-target", ChannelMetaAttached: true, ReasoningEffort: "existing"}
			req := &dto.GeminiChatRequest{GenerationConfig: dto.GeminiChatGenerationConfig{ThinkingConfig: &dto.GeminiThinkingConfig{ThinkingLevel: tc.level}}}
			out, err := GeminiGenerateContentRequestToOpenAIChat(req, info)
			require.Equal(t, tc.level, req.GenerationConfig.ThinkingConfig.ThinkingLevel)
			if tc.wantError != "" {
				require.ErrorContains(t, err, tc.wantError)
				require.Nil(t, out)
				require.Equal(t, "existing", info.ReasoningEffort)
				return
			}
			require.NoError(t, err)
			require.Equal(t, "gpt-target", out.Model)
			require.Equal(t, tc.want, out.ReasoningEffort)
			if tc.want != "" {
				require.Equal(t, tc.want, info.ReasoningEffort)
			}
		})
	}
}

func TestGeminiLevelConversionBoundaries(t *testing.T) {
	budget := 1000
	req := &dto.GeminiChatRequest{GenerationConfig: dto.GeminiChatGenerationConfig{ThinkingConfig: &dto.GeminiThinkingConfig{ThinkingLevel: "HIGH", ThinkingBudget: &budget}}}
	_, err := GeminiGenerateContentRequestToOpenAIChat(req, nil)
	require.ErrorContains(t, err, "cannot both be set")
	req.GenerationConfig.ThinkingConfig.ThinkingBudget = nil
	out, err := GeminiGenerateContentRequestToOpenAIChat(req, nil)
	require.NoError(t, err)
	require.Equal(t, "high", out.ReasoningEffort)
	var nilInfo *convmeta.Values
	out, err = GeminiGenerateContentRequestToOpenAIChat(req, nilInfo)
	require.NoError(t, err)
	require.Equal(t, "high", out.ReasoningEffort)

	info := &convmeta.Values{OriginModelName: "gemini-3.7-flash", UpstreamModelName: "gpt-target", ChannelMetaAttached: true, ConversionChain: []types.RelayFormat{types.RelayFormatGemini}}
	result, err := ConvertRequest(nil, info, types.RelayFormatOpenAIResponses, req)
	require.NoError(t, err)
	responses := result.Value.(*dto.OpenAIResponsesRequest)
	require.NotNil(t, responses.Reasoning)
	require.Equal(t, "high", responses.Reasoning.Effort)
	require.Equal(t, "HIGH", req.GenerationConfig.ThinkingConfig.ThinkingLevel)
}

func TestGeminiNativeLevelAndSuffixMetadata(t *testing.T) {
	info := &convmeta.Values{UpstreamModelName: "gemini-3.7-flash", ChannelMetaAttached: true, ReasoningConversion: &dto.ReasoningConversionState{Mode: "enabled", Effort: "medium"}}
	req := &dto.GeminiChatRequest{GenerationConfig: dto.GeminiChatGenerationConfig{ThinkingConfig: &dto.GeminiThinkingConfig{ThinkingLevel: "HIGH"}}}
	require.ErrorContains(t, ApplyGeminiThinkingConfigChecked(req, info), "conflict")
	req.GenerationConfig.ThinkingConfig.ThinkingLevel = "MEDIUM"
	require.NoError(t, ApplyGeminiThinkingConfigChecked(req, info))
	require.Equal(t, "MEDIUM", req.GenerationConfig.ThinkingConfig.ThinkingLevel)
	require.Equal(t, "medium", info.ReasoningEffort)
	require.NoError(t, ApplyGeminiThinkingConfigChecked(nil, info))
	require.NoError(t, ApplyGeminiThinkingConfigChecked(req, nil))
	var nilInfo *convmeta.Values
	require.NoError(t, ApplyGeminiThinkingConfigChecked(req, nilInfo))
}
