package openaicompat

import (
	"encoding/json"
	"testing"

	"github.com/QuantumNous/new-api/dto"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestOpenAIRequestConversionsPreserveZeroAndAliFiltering(t *testing.T) {
	zeroUint := uint(0)
	zeroFloat := 0.0
	stream := false
	responsesReq, err := ChatCompletionsRequestToResponsesRequest(&dto.GeneralOpenAIRequest{
		Model:          "qwen-plus",
		Messages:       []dto.Message{{Role: "user", Content: "hello"}},
		MaxTokens:      &zeroUint,
		Temperature:    &zeroFloat,
		Stream:         &stream,
		ThinkingBudget: json.RawMessage(`0`),
	})
	require.NoError(t, err)
	require.NotNil(t, responsesReq.MaxOutputTokens)
	require.Zero(t, *responsesReq.MaxOutputTokens)
	require.NotNil(t, responsesReq.Temperature)
	require.Zero(t, *responsesReq.Temperature)
	require.NotNil(t, responsesReq.Stream)
	require.False(t, *responsesReq.Stream)
	require.JSONEq(t, `0`, string(responsesReq.ThinkingBudget))

	chatReq, err := ResponsesRequestToChatCompletionsRequest(&dto.OpenAIResponsesRequest{
		Model:                "qwen-plus",
		Input:                json.RawMessage(`"hello"`),
		MaxOutputTokens:      &zeroUint,
		TopLogProbs:          new(int),
		ServiceTier:          "priority",
		SafetyIdentifier:     json.RawMessage(`"user-1"`),
		PromptCacheKey:       json.RawMessage(`"cache-1"`),
		PromptCacheRetention: json.RawMessage(`"24h"`),
		ThinkingBudget:       json.RawMessage(`0`),
	})
	require.NoError(t, err)
	require.NotNil(t, chatReq.MaxTokens)
	require.Zero(t, *chatReq.MaxTokens)
	require.Nil(t, chatReq.MaxCompletionTokens)
	require.Nil(t, chatReq.TopLogProbs)
	require.Empty(t, chatReq.ServiceTier)
	require.Empty(t, chatReq.SafetyIdentifier)
	require.Empty(t, chatReq.PromptCacheKey)
	require.Empty(t, chatReq.PromptCacheRetention)
	require.JSONEq(t, `0`, string(chatReq.ThinkingBudget))
}

func TestChatResponseConversionUsesRelayKitAndKeepsWireShape(t *testing.T) {
	reasoning := "thought"
	resp := &dto.OpenAITextResponse{
		Id:    "chatcmpl-upstream",
		Model: "mapped-model",
		Choices: []dto.OpenAITextResponseChoice{{
			Message: dto.Message{
				Role:             "assistant",
				Content:          "answer",
				ReasoningContent: &reasoning,
			},
			FinishReason: "stop",
		}},
		Usage: dto.Usage{
			PromptTokens:     10,
			CompletionTokens: 2,
			TotalTokens:      12,
			PromptTokensDetails: dto.InputTokenDetails{
				VideoTokens: 9,
			},
		},
	}

	body, usage, err := ChatCompletionsResponseToResponsesResponse(resp, "resp-local", "client-model")
	require.NoError(t, err)
	require.Equal(t, 9, usage.InputTokensDetails.VideoTokens)
	require.Equal(t, "client-model", gjson.GetBytes(body, "model").String())
	require.Equal(t, "reasoning", gjson.GetBytes(body, "output.0.type").String())
	require.Equal(t, "message", gjson.GetBytes(body, "output.1.type").String())
	require.False(t, gjson.GetBytes(body, "output.1.quality").Exists())
}
