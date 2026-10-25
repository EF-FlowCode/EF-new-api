package common

import (
	"net/http/httptest"
	"testing"

	"eflow.local/new-api/dto"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestGenRelayInfoRecordsRequestReasoningEffort(t *testing.T) {
	for _, tc := range []struct {
		name    string
		request dto.Request
		want    string
	}{
		{"chat", &dto.GeneralOpenAIRequest{ReasoningEffort: "high"}, "high"},
		{"chat reasoning fallback", &dto.GeneralOpenAIRequest{Reasoning: []byte(`{"effort":"medium"}`)}, "medium"},
		{"chat explicit precedence", &dto.GeneralOpenAIRequest{ReasoningEffort: "low", Reasoning: []byte(`{"effort":"high"}`)}, "low"},
		{"responses", &dto.OpenAIResponsesRequest{Reasoning: &dto.Reasoning{Effort: "high"}}, "high"},
		{"gemini canonical", &dto.GeminiChatRequest{GenerationConfig: dto.GeminiChatGenerationConfig{ThinkingConfig: &dto.GeminiThinkingConfig{ThinkingLevel: " MEDIUM "}}}, "medium"},
		{"gemini unknown", &dto.GeminiChatRequest{GenerationConfig: dto.GeminiChatGenerationConfig{ThinkingConfig: &dto.GeminiThinkingConfig{ThinkingLevel: "ULTRA"}}}, "ULTRA"},
		{"no request", nil, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			ctx.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
			info := genBaseRelayInfo(ctx, tc.request)
			require.Equal(t, tc.want, info.ReasoningEffort)
			if req, ok := tc.request.(*dto.GeminiChatRequest); ok && tc.name == "gemini canonical" {
				require.Equal(t, " MEDIUM ", req.GenerationConfig.ThinkingConfig.ThinkingLevel)
			}
		})
	}
}
