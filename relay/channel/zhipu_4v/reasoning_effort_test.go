package zhipu_4v

import (
	"encoding/json"
	"testing"

	"eflow.local/new-api/dto"
	"github.com/stretchr/testify/require"
)

func TestZhipuReasoningEffortPreservesRequestNormalization(t *testing.T) {
	for _, effort := range []string{"", "low", "high"} {
		t.Run(effort, func(t *testing.T) {
			maxTokens, maxCompletion := uint(10), uint(20)
			req := dto.GeneralOpenAIRequest{Model: "glm-4.5", ReasoningEffort: effort,
				Stop: "stop", MaxTokens: &maxTokens, MaxCompletionTokens: &maxCompletion,
				THINKING: json.RawMessage(`{"type":"enabled"}`),
				Messages: []dto.Message{{Role: "user", Content: []dto.MediaContent{{Type: dto.ContentTypeImageURL, ImageUrl: &dto.MessageImageUrl{Url: "data:image/png;base64,aW1n"}}}}},
			}
			// Match the wire-decoded content shape used by the real request path.
			input, err := json.Marshal(req)
			require.NoError(t, err)
			require.NoError(t, json.Unmarshal(input, &req))
			result, err := (&Adaptor{}).ConvertOpenAIRequest(nil, nil, &req)
			require.NoError(t, err)
			out := result.(*dto.GeneralOpenAIRequest)
			require.Equal(t, effort, out.ReasoningEffort)
			require.Equal(t, []string{"stop"}, out.Stop)
			require.Equal(t, maxCompletion, *out.MaxTokens)
			require.JSONEq(t, string(req.THINKING), string(out.THINKING))
			data, err := json.Marshal(out)
			require.NoError(t, err)
			var wire dto.GeneralOpenAIRequest
			require.NoError(t, json.Unmarshal(data, &wire))
			media := wire.Messages[0].ParseContent()
			require.Len(t, media, 1)
			require.Equal(t, "aW1n", media[0].GetImageMedia().Url)
			if effort == "" {
				require.NotContains(t, string(data), "reasoning_effort")
			} else {
				require.Contains(t, string(data), `"reasoning_effort":"`+effort+`"`)
			}
		})
	}
}
