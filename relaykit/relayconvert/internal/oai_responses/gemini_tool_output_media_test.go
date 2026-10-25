package oairesponses

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"eflow.local/new-api/relaykit/dto"
	"eflow.local/new-api/relaykit/relayconvert/convmeta"
	relaymedia "eflow.local/new-api/relaykit/relayconvert/internal/media"
	"eflow.local/new-api/relaykit/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func useFakeGeminiMediaResolver(t *testing.T) {
	t.Helper()
	relaymedia.SetMediaResolver(relaymedia.MediaResolver{
		GetBase64Data: func(_ context.Context, source types.FileSource, _ ...string) (string, string, error) {
			raw := source.GetRawData()
			switch {
			case strings.Contains(raw, "fail"):
				return "", "", errors.New("blocked")
			case strings.HasSuffix(raw, ".pdf"):
				return "UERG", "application/pdf", nil
			case strings.HasSuffix(raw, ".wav"):
				return "V0FW", "audio/wav", nil
			default:
				return "UE5H", "image/png", nil
			}
		},
	})
	t.Cleanup(func() { relaymedia.SetMediaResolver(relaymedia.MediaResolver{}) })
}

func geminiToolOutputRequest(t *testing.T, model string, outputs ...any) *dto.OpenAIResponsesRequest {
	t.Helper()
	input := []map[string]any{
		{"type": "message", "role": "user", "content": "look"},
		{"type": "function_call", "call_id": "call_1", "name": "shot", "arguments": "{}"},
		{"type": "function_call", "call_id": "call_2", "name": "shot", "arguments": "{}"},
	}
	for i, output := range outputs {
		callID := "call_1"
		if i == 1 {
			callID = "call_2"
		}
		input = append(input, map[string]any{"type": "function_call_output", "call_id": callID, "output": output})
	}
	return &dto.OpenAIResponsesRequest{Model: model, Input: mustRawMessage(t, input)}
}

// TestResponsesToolOutputMediaOnGemini covers Responses tool outputs holding
// content parts: text stays in functionResponse.response and media becomes
// inlineData instead of base64 stringified into the response map. Gemini 3
// takes images and documents in functionResponse.parts; Gemini 2.x gets them
// as parts after every function response of the turn.
func TestResponsesToolOutputMediaOnGemini(t *testing.T) {
	useFakeGeminiMediaResolver(t)
	imageOutput := []any{
		map[string]any{"type": "input_text", "text": "screenshot"},
		map[string]any{"type": "input_image", "image_url": "https://example.com/a.png"},
	}
	textOutput := "done"

	t.Run("gemini 3 uses functionResponse.parts", func(t *testing.T) {
		got, err := OpenAIResponsesRequestToGeminiChat(context.Background(), geminiToolOutputRequest(t, "gemini-3.7-flash", imageOutput, textOutput), &convmeta.Values{})
		require.NoError(t, err)
		turn := got.Contents[len(got.Contents)-1]
		require.Equal(t, "user", turn.Role)
		require.Len(t, turn.Parts, 2)
		first := turn.Parts[0].FunctionResponse
		require.NotNil(t, first)
		assert.NotContains(t, mustJSON(t, first.Response), "UE5H")
		assert.Equal(t, "screenshot", first.Response["content"])
		assert.Equal(t, "image/png", gjson.GetBytes(first.Parts, "0.inlineData.mimeType").String())
		assert.Equal(t, "UE5H", gjson.GetBytes(first.Parts, "0.inlineData.data").String())
		require.NotNil(t, turn.Parts[1].FunctionResponse)
	})

	t.Run("gemini 2.5 sends media after the function responses", func(t *testing.T) {
		got, err := OpenAIResponsesRequestToGeminiChat(context.Background(), geminiToolOutputRequest(t, "gemini-2.5-flash", imageOutput, textOutput), &convmeta.Values{})
		require.NoError(t, err)
		turn := got.Contents[len(got.Contents)-1]
		require.Len(t, turn.Parts, 3)
		require.NotNil(t, turn.Parts[0].FunctionResponse)
		assert.Empty(t, turn.Parts[0].FunctionResponse.Parts)
		require.NotNil(t, turn.Parts[1].FunctionResponse, "a later function response goes before the tool media")
		require.NotNil(t, turn.Parts[2].InlineData)
		assert.Equal(t, "image/png", turn.Parts[2].InlineData.MimeType)
	})

	t.Run("media that cannot be carried is omitted", func(t *testing.T) {
		output := []any{
			map[string]any{"type": "input_audio", "input_audio": map[string]any{"data": "https://example.com/a.wav"}},
			map[string]any{"type": "input_image", "image_url": "https://example.com/fail.png"},
		}
		got, err := OpenAIResponsesRequestToGeminiChat(context.Background(), geminiToolOutputRequest(t, "gemini-3.7-flash", output), &convmeta.Values{})
		require.NoError(t, err)
		response := got.Contents[len(got.Contents)-1].Parts[0].FunctionResponse
		require.NotNil(t, response)
		assert.Empty(t, response.Parts)
		assert.Equal(t, "[audio] [image]", response.Response["content"])
	})

	t.Run("other payload shapes keep the response map", func(t *testing.T) {
		output := []any{map[string]any{"type": "custom", "value": 1}}
		got, err := OpenAIResponsesRequestToGeminiChat(context.Background(), geminiToolOutputRequest(t, "gemini-3.7-flash", output), &convmeta.Values{})
		require.NoError(t, err)
		response := got.Contents[len(got.Contents)-1].Parts[0].FunctionResponse
		assert.JSONEq(t, `{"result":[{"type":"custom","value":1}]}`, mustJSON(t, response.Response))
	})
}

func mustJSON(t *testing.T, value any) string {
	t.Helper()
	data, err := json.Marshal(value)
	require.NoError(t, err)
	return string(data)
}
