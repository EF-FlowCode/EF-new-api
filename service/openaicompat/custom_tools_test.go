package openaicompat

import (
	"encoding/json"
	"testing"

	"eflow.local/new-api/dto"
	relaycommon "eflow.local/new-api/relay/common"
	"github.com/stretchr/testify/require"
)

func TestResponsesAliCustomToolStateAndFiltering(t *testing.T) {
	info := &relaycommon.RelayInfo{}
	req := &dto.OpenAIResponsesRequest{Model: "qwen-test", Input: json.RawMessage(`"run"`), Tools: json.RawMessage(`[{"type":"custom","name":"exec"}]`), PromptCacheKey: json.RawMessage(`"drop"`)}
	chat, err := ResponsesRequestToChatCompletionsRequestWithInfo(req, info)
	require.NoError(t, err)
	require.Empty(t, chat.PromptCacheKey)
	require.True(t, info.ResponsesTools.IsCustomTool("exec"))
	var response dto.OpenAITextResponse
	require.NoError(t, json.Unmarshal([]byte(`{"choices":[{"message":{"tool_calls":[{"id":"c1","type":"function","function":{"name":"exec","arguments":"{\"input\":\"echo hi\"}"}}]},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":0,"completion_tokens":0,"total_tokens":0,"prompt_tokens_details":{"video_tokens":2}}}`), &response))
	body, usage, err := ChatCompletionsResponseToResponsesResponseWithTools(&response, "r1", "client", info.ResponsesTools)
	require.NoError(t, err)
	require.Contains(t, string(body), `"type":"custom_tool_call"`)
	require.Contains(t, string(body), `"input":"echo hi"`)
	require.Equal(t, 2, usage.InputTokensDetails.VideoTokens)
	require.Zero(t, usage.TotalTokens)
	req.Tools = json.RawMessage(`[{"type":"custom","name":""}]`)
	_, err = ResponsesRequestToChatCompletionsRequestWithInfo(req, info)
	require.Error(t, err)
	require.Nil(t, info.ResponsesTools)
}
