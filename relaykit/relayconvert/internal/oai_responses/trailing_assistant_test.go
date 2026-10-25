package oairesponses

import (
	"context"
	"testing"

	"eflow.local/new-api/relaykit/dto"
	"eflow.local/new-api/relaykit/relayconvert/convmeta"
	"github.com/stretchr/testify/require"
)

func trailingAssistantRequest(t *testing.T, input []map[string]any) *dto.OpenAIResponsesRequest {
	t.Helper()
	maxOutputTokens := uint(256)
	return &dto.OpenAIResponsesRequest{Model: "test-model", Input: mustRawMessage(t, input), MaxOutputTokens: &maxOutputTokens}
}

var (
	userTurn          = map[string]any{"type": "message", "role": "user", "content": "hi"}
	assistantText     = map[string]any{"type": "message", "role": "assistant", "content": []map[string]any{{"type": "output_text", "text": "partial answer"}}}
	pendingToolCall   = map[string]any{"type": "function_call", "call_id": "call_1", "name": "lookup", "arguments": "{}"}
	toolCallOutput    = map[string]any{"type": "function_call_output", "call_id": "call_1", "output": "42"}
	secondAssistantTx = map[string]any{"type": "message", "role": "assistant", "content": []map[string]any{{"type": "output_text", "text": "more"}}}
)

func TestResponsesToClaudeDropsTrailingAssistantText(t *testing.T) {
	cases := []struct {
		name  string
		input []map[string]any
		roles []string
	}{
		{"retry after a completed answer", []map[string]any{userTurn, assistantText, secondAssistantTx}, []string{"user"}},
		{"answer after a tool result", []map[string]any{userTurn, pendingToolCall, toolCallOutput, assistantText}, []string{"user", "assistant", "user"}},
		{"pending tool call stays", []map[string]any{userTurn, pendingToolCall}, []string{"user", "assistant"}},
		{"assistant history before a new user turn stays", []map[string]any{userTurn, assistantText, userTurn}, []string{"user", "assistant", "user"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := OpenAIResponsesRequestToClaudeMessages(context.Background(), &convmeta.Values{}, trailingAssistantRequest(t, tc.input))
			require.NoError(t, err)
			roles := make([]string, 0, len(got.Messages))
			for _, message := range got.Messages {
				roles = append(roles, message.Role)
			}
			require.Equal(t, tc.roles, roles)
		})
	}
}

func TestResponsesToGeminiDropsTrailingAssistantText(t *testing.T) {
	got, err := OpenAIResponsesRequestToGeminiChat(context.Background(), trailingAssistantRequest(t, []map[string]any{userTurn, assistantText}), &convmeta.Values{})
	require.NoError(t, err)
	require.Len(t, got.Contents, 1)
	require.Equal(t, "user", got.Contents[0].Role)

	got, err = OpenAIResponsesRequestToGeminiChat(context.Background(), trailingAssistantRequest(t, []map[string]any{userTurn, pendingToolCall}), &convmeta.Values{})
	require.NoError(t, err)
	require.Len(t, got.Contents, 2, "a tool call waiting for its result is kept")
}
