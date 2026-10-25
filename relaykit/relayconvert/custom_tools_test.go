package relayconvert

import (
	"context"
	"encoding/json"
	"testing"

	"eflow.local/new-api/relaykit/dto"
	"eflow.local/new-api/relaykit/relayconvert/convmeta"
	"eflow.local/new-api/relaykit/types"
	"github.com/stretchr/testify/require"
)

func TestResponsesCustomToolsAcrossProviders(t *testing.T) {
	for _, target := range []types.RelayFormat{types.RelayFormatOpenAI, types.RelayFormatClaude, types.RelayFormatGemini} {
		t.Run(string(target), func(t *testing.T) {
			meta := &convmeta.Values{Options: &convmeta.Options{Claude: convmeta.ClaudeOptions{DefaultMaxTokens: func(string) int { return 1024 }}}}
			req := &dto.OpenAIResponsesRequest{Model: "test-model", Tools: json.RawMessage(`[{"type":"custom","name":"exec","format":{"type":"grammar","syntax":"lark","definition":"start: /.+/"}},{"type":"function","name":"lookup","parameters":{"type":"object"}}]`), ToolChoice: json.RawMessage(`{"type":"custom","name":"exec"}`), Input: json.RawMessage(`[{"role":"user","content":"run"},{"type":"custom_tool_call","call_id":"call_1","name":"exec","input":"{\"literal\":42}\nnext"},{"type":"custom_tool_call_output","call_id":"call_1","output":"ok"}]`)}
			before, err := json.Marshal(req)
			require.NoError(t, err)
			result, err := ConvertRequest(context.Background(), meta, target, req)
			require.NoError(t, err)
			encoded, err := json.Marshal(result.Value)
			require.NoError(t, err)
			require.Contains(t, string(encoded), `start: /.+/`)
			require.Contains(t, string(encoded), `input`)
			require.Contains(t, string(encoded), `literal`)
			require.True(t, meta.ResponsesTools.IsCustomTool("exec"))
			require.False(t, meta.ResponsesTools.IsCustomTool("lookup"))
			switch converted := result.Value.(type) {
			case *dto.GeneralOpenAIRequest:
				choice, err := json.Marshal(converted.ToolChoice)
				require.NoError(t, err)
				require.JSONEq(t, `{"type":"function","function":{"name":"exec"}}`, string(choice))
				require.JSONEq(t, `{"input":"{\"literal\":42}\nnext"}`, converted.Messages[1].ParseToolCalls()[0].Function.Arguments)
				require.Equal(t, "tool", converted.Messages[2].Role)
			case *dto.ClaudeRequest:
				choice, err := json.Marshal(converted.ToolChoice)
				require.NoError(t, err)
				require.JSONEq(t, `{"type":"tool","name":"exec"}`, string(choice))
				content, err := converted.Messages[1].ParseContent()
				require.NoError(t, err)
				require.Equal(t, map[string]any{"input": "{\"literal\":42}\nnext"}, content[0].Input)
			case *dto.GeminiChatRequest:
				require.NotNil(t, converted.ToolConfig)
				require.NotNil(t, converted.ToolConfig.FunctionCallingConfig)
				require.EqualValues(t, "ANY", converted.ToolConfig.FunctionCallingConfig.Mode)
				require.Equal(t, []string{"exec"}, converted.ToolConfig.FunctionCallingConfig.AllowedFunctionNames)
				require.Equal(t, map[string]any{"input": "{\"literal\":42}\nnext"}, converted.Contents[1].Parts[0].FunctionCall.Arguments)
			}
			after, err := json.Marshal(req)
			require.NoError(t, err)
			require.Equal(t, string(before), string(after))
			// Actual provider response travels through its native converter and then
			// Chat -> Responses; all targets must restore the same raw tool input.
			var native any
			switch target {
			case types.RelayFormatOpenAI:
				v := &dto.OpenAITextResponse{}
				require.NoError(t, json.Unmarshal([]byte(`{"choices":[{"message":{"role":"assistant","tool_calls":[{"id":"call_2","type":"function","function":{"name":"exec","arguments":"{\"input\":\"echo hi\"}"}}]},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5}}`), v))
				native = v
			case types.RelayFormatClaude:
				v := &dto.ClaudeResponse{}
				require.NoError(t, json.Unmarshal([]byte(`{"type":"message","id":"m1","model":"test-model","role":"assistant","content":[{"type":"tool_use","id":"call_2","name":"exec","input":{"input":"echo hi"}}],"stop_reason":"tool_use","usage":{"input_tokens":3,"output_tokens":2}}`), v))
				native = v
			case types.RelayFormatGemini:
				v := &dto.GeminiChatResponse{}
				require.NoError(t, json.Unmarshal([]byte(`{"candidates":[{"content":{"role":"model","parts":[{"functionCall":{"name":"exec","args":{"input":"echo hi"}}}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":3,"candidatesTokenCount":2,"totalTokenCount":5}}`), v))
				native = v
			}
			response, err := ConvertResponse(context.Background(), meta, types.RelayFormatOpenAIResponses, native)
			require.NoError(t, err)
			out := response.Value.(*dto.OpenAIResponsesResponse)
			require.Len(t, out.Output, 1)
			require.Equal(t, "custom_tool_call", out.Output[0].Type)
			require.JSONEq(t, `"echo hi"`, string(out.Output[0].Input))
			require.Equal(t, 5, response.Usage.TotalTokens)
			_, err = ConvertRequest(context.Background(), meta, target, &dto.OpenAIResponsesRequest{Model: "test-model", Input: json.RawMessage(`"plain"`)})
			require.NoError(t, err)
			require.Nil(t, meta.ResponsesTools, "next attempt must clear custom tool state")
		})
	}
}

func TestResponsesCustomToolsRejectAmbiguity(t *testing.T) {
	for _, tools := range []string{`[{"type":"function","name":"exec"},{"type":"custom","name":"exec"}]`, `[{"type":"custom","name":"exec"},{"type":"custom","name":"exec"}]`, `[{"type":"custom","name":""}]`} {
		_, err := ConvertRequest(context.Background(), &convmeta.Values{}, types.RelayFormatOpenAI, &dto.OpenAIResponsesRequest{Model: "test", Tools: json.RawMessage(tools)})
		require.Error(t, err)
	}
}

func TestResponsesCustomToolsExplicitMultiHop(t *testing.T) {
	meta := &convmeta.Values{}
	req := &dto.OpenAIResponsesRequest{Model: "gemini-test", Tools: json.RawMessage(`[{"type":"custom","name":"exec"}]`), Input: json.RawMessage(`[{"type":"custom_tool_call","name":"exec","call_id":"c1","input":"{\"raw\":true}"},{"type":"custom_tool_call_output","call_id":"c1","output":"ok"}]`)}
	result, err := ConvertRequestVia(context.Background(), meta, req, types.RelayFormatOpenAI, types.RelayFormatGemini)
	require.NoError(t, err)
	require.True(t, meta.ResponsesTools.IsCustomTool("exec"))
	encoded, err := json.Marshal(result.Value)
	require.NoError(t, err)
	require.Contains(t, string(encoded), `raw`)
}
