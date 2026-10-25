package oaichat

import (
	"encoding/json"
	"testing"

	"eflow.local/new-api/relaykit/dto"
	kitutil "eflow.local/new-api/relaykit/relayconvert/kitutil"
	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestChatCompletionsRequestToResponsesRequestInstructionsAndTools(t *testing.T) {
	req := &dto.GeneralOpenAIRequest{
		Model: "gpt-test",
		N:     lo.ToPtr(1),
		Messages: []dto.Message{
			{Role: "system", Content: "system rules"},
			{Role: "developer", Content: "developer rules"},
			{Role: "user", Content: []any{
				map[string]any{"type": "text", "text": "look"},
				map[string]any{"type": "image_url", "image_url": map[string]any{"url": "https://example.test/a.png"}},
			}},
			assistantMessageWithTool("partial text", "call_1", "lookup", `{"q":"x"}`),
			{Role: "tool", ToolCallId: "call_1", Content: "tool result"},
		},
	}

	got, err := ChatCompletionsRequestToResponsesRequest(req)
	require.NoError(t, err)

	assert.Equal(t, "gpt-test", got.Model)
	assert.Equal(t, `"system rules\n\ndeveloper rules"`, string(got.Instructions))
	assert.Equal(t, "input_image", gjson.GetBytes(got.Input, "0.content.1.type").String())
	assert.Equal(t, "function_call", gjson.GetBytes(got.Input, "2.type").String())
	assert.Equal(t, "call_1", gjson.GetBytes(got.Input, "2.call_id").String())
	assert.Equal(t, "function_call_output", gjson.GetBytes(got.Input, "3.type").String())
}

func TestChatCompletionsRequestToResponsesRequestPreservesQwenThinkingBudget(t *testing.T) {
	tests := []struct {
		name   string
		budget json.RawMessage
		want   int64
	}{
		{name: "positive budget", budget: json.RawMessage(`128`), want: 128},
		{name: "zero budget", budget: json.RawMessage(`0`), want: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := &dto.GeneralOpenAIRequest{
				Model:          "qwen-plus",
				EnableThinking: json.RawMessage(`true`),
				ThinkingBudget: tt.budget,
				Messages: []dto.Message{
					{Role: "user", Content: "hello"},
				},
			}

			got, err := ChatCompletionsRequestToResponsesRequest(req)
			require.NoError(t, err)
			assert.Equal(t, tt.budget, got.ThinkingBudget)

			encoded, err := kitutil.Marshal(got)
			require.NoError(t, err)

			assert.True(t, gjson.GetBytes(encoded, "enable_thinking").Bool())
			value := gjson.GetBytes(encoded, "thinking_budget")
			assert.True(t, value.Exists())
			assert.Equal(t, tt.want, value.Int())
		})
	}
}

func TestChatCompletionsRequestToResponsesRequestRejectsMultipleChoices(t *testing.T) {
	_, err := ChatCompletionsRequestToResponsesRequest(&dto.GeneralOpenAIRequest{
		Model: "gpt-test",
		N:     lo.ToPtr(2),
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "n>1")
}

func TestChatCompletionsRequestToResponsesRequestPreservesCacheKeyAndPenalties(t *testing.T) {
	frequency := 0.0
	presence := 1.5
	key := "session-\"quoted\"\\path\n世界"

	got, err := ChatCompletionsRequestToResponsesRequest(&dto.GeneralOpenAIRequest{
		Model:            "gpt-test",
		Messages:         []dto.Message{{Role: "user", Content: "hello"}},
		PromptCacheKey:   key,
		FrequencyPenalty: &frequency,
		PresencePenalty:  &presence,
	})
	require.NoError(t, err)

	assert.Equal(t, json.RawMessage(`0`), got.FrequencyPenalty)
	assert.Equal(t, json.RawMessage(`1.5`), got.PresencePenalty)
	assert.Equal(t, json.RawMessage(`"session-\"quoted\"\\path\n世界"`), got.PromptCacheKey)

	encoded, err := kitutil.Marshal(got)
	require.NoError(t, err)
	assert.Equal(t, key, gjson.GetBytes(encoded, "prompt_cache_key").String())
	assert.True(t, gjson.GetBytes(encoded, "frequency_penalty").Exists())
}

func TestChatCompletionsRequestToResponsesRequestOmitsUnsetCacheKeyAndPenalties(t *testing.T) {
	got, err := ChatCompletionsRequestToResponsesRequest(&dto.GeneralOpenAIRequest{
		Model:    "gpt-test",
		Messages: []dto.Message{{Role: "user", Content: "hello"}},
	})
	require.NoError(t, err)
	assert.Nil(t, got.PromptCacheKey)
	assert.Nil(t, got.FrequencyPenalty)
	assert.Nil(t, got.PresencePenalty)
}

func assistantMessageWithTool(content string, id string, name string, args string) dto.Message {
	msg := dto.Message{Role: "assistant", Content: content}
	msg.SetToolCalls([]dto.ToolCallRequest{
		{
			ID:   id,
			Type: "function",
			Function: dto.FunctionRequest{
				Name:      name,
				Arguments: args,
			},
		},
	})
	return msg
}

func TestChatCompletionsRequestToResponsesRequestImageDetail(t *testing.T) {
	tests := []struct {
		name     string
		imageURL any
		want     string
	}{
		{name: "object without detail defaults to auto", imageURL: map[string]any{"url": "https://example.test/a.png"}, want: "auto"},
		{name: "low detail is preserved", imageURL: map[string]any{"url": "https://example.test/a.png", "detail": "low"}, want: "low"},
		// Parsing defaults a missing detail to "high"; the two are indistinguishable.
		{name: "high detail uses the default", imageURL: map[string]any{"url": "https://example.test/a.png", "detail": "high"}, want: "auto"},
		{name: "auto detail is preserved", imageURL: map[string]any{"url": "https://example.test/a.png", "detail": "auto"}, want: "auto"},
		{name: "original detail is preserved", imageURL: map[string]any{"url": "https://example.test/a.png", "detail": "original"}, want: "original"},
		{name: "unknown detail falls back to auto", imageURL: map[string]any{"url": "https://example.test/a.png", "detail": "bogus"}, want: "auto"},
		{name: "plain string url defaults to auto", imageURL: "https://example.test/a.png", want: "auto"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ChatCompletionsRequestToResponsesRequest(&dto.GeneralOpenAIRequest{
				Model: "gpt-test",
				Messages: []dto.Message{{Role: "user", Content: []any{
					map[string]any{"type": "image_url", "image_url": tt.imageURL},
				}}},
			})
			require.NoError(t, err)

			assert.Equal(t, "input_image", gjson.GetBytes(got.Input, "0.content.0.type").String())
			assert.Equal(t, tt.want, gjson.GetBytes(got.Input, "0.content.0.detail").String())
		})
	}
}
