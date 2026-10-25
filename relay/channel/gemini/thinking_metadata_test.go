package gemini

import (
	"testing"

	"eflow.local/new-api/dto"
	"eflow.local/new-api/internal/testconfig"
	relaycommon "eflow.local/new-api/relay/common"
	"github.com/stretchr/testify/require"
)

func TestThinkingAdaptorRecordsFinalConfig(t *testing.T) {
	for _, tc := range []struct {
		name, enabled, model, input, wantLevel, wantEffort, wantError string
		budget                                                        int
	}{
		{"native", "false", "gemini-3.7-flash", " HIGH ", " HIGH ", "high", "", 0},
		{"native unknown", "false", "gemini-3.7-flash", "ULTRA", "ULTRA", "ULTRA", "", 0},
		{"level suffix", "true", "gemini-3.7-flash-medium", "", "medium", "medium", "", 0},
		{"matching suffix", "true", "gemini-3.7-flash-medium", " MEDIUM ", " MEDIUM ", "medium", "", 0},
		{"conflict", "true", "gemini-3.7-flash-medium", "HIGH", "", "", "conflict", 0},
		{"budget suffix", "true", "gemini-2.5-flash-thinking-1024", "", "", "low", "", 1024},
		{"cannot disable pro", "true", "gemini-2.5-pro-nothinking", "", "", "", "cannot be disabled", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			testconfig.Set(t, "gemini", map[string]string{"thinking_adapter_enabled": tc.enabled})
			info := &relaycommon.RelayInfo{OriginModelName: tc.model, ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: tc.model}}
			req := &dto.GeminiChatRequest{}
			if tc.input != "" {
				req.GenerationConfig.ThinkingConfig = &dto.GeminiThinkingConfig{ThinkingLevel: tc.input}
			}
			err := ThinkingAdaptor(req, info)
			if tc.wantError != "" {
				require.ErrorContains(t, err, tc.wantError)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.model, info.OriginModelName)
			require.Equal(t, tc.wantLevel, req.GenerationConfig.ThinkingConfig.ThinkingLevel)
			require.Equal(t, tc.wantEffort, info.ReasoningEffort)
			if tc.budget != 0 {
				require.Equal(t, tc.budget, *req.GenerationConfig.ThinkingConfig.ThinkingBudget)
			}
		})
	}
}
