package service

import (
	"context"
	"encoding/json"
	"testing"

	"eflow.local/new-api/dto"
	relaycommon "eflow.local/new-api/relay/common"
	"eflow.local/new-api/relaykit/relayconvert"
	"eflow.local/new-api/relaykit/relayconvert/convmeta"
	"eflow.local/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestRelayKitResponsesToolsFailureAndRetry(t *testing.T) {
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "test"}}
	req := &dto.OpenAIResponsesRequest{Model: "test", Input: json.RawMessage(`"run"`), Tools: json.RawMessage(`[{"type":"custom","name":"exec"}]`)}
	_, err := ConvertResponsesRequestToChat(context.Background(), info, req)
	require.NoError(t, err)
	require.True(t, info.ResponsesTools.IsCustomTool("exec"))
	bad := *req
	bad.Tools = json.RawMessage(`[{"type":"custom","name":""}]`)
	_, err = ConvertResponsesRequestToChat(context.Background(), info, &bad)
	require.Error(t, err)
	require.Nil(t, info.ResponsesTools)
	_, err = ConvertResponsesRequestToChat(context.Background(), info, req)
	require.NoError(t, err)
	c, _ := gin.CreateTestContext(nil)
	info.InitChannelMeta(c)
	require.Nil(t, info.ResponsesTools)
}

func TestRelayKitResponsesCustomToolStreamsAcrossProviders(t *testing.T) {
	for _, format := range []types.RelayFormat{types.RelayFormatClaude, types.RelayFormatGemini} {
		t.Run(string(format), func(t *testing.T) {
			info := &relaycommon.RelayInfo{OriginModelName: "client", IsStream: true, ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "test"}, ResponsesTools: &convmeta.ResponsesToolState{CustomToolNames: map[string]struct{}{"exec": {}}}}
			stream, err := NewRelayResponseStream(info, format, types.RelayFormatOpenAIResponses, RelayResponseStreamOptions{ID: "resp_test", Model: "test", EmitSequenceNumber: true})
			require.NoError(t, err)
			var events []*dto.ResponsesStreamResponse
			if format == types.RelayFormatClaude {
				// Claude usage arrives in separate start/delta frames. The host owns
				// aggregation and supplies the complete usage before finalization.
				claudeInfo := &relayconvert.ClaudeResponseInfo{}
				for _, raw := range []string{`{"type":"message_start","message":{"id":"m1","model":"test","usage":{"input_tokens":3,"output_tokens":0}}}`, `{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"c1","name":"exec","input":{}}}`, `{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"input\":\"echo hi\"}"}}`, `{"type":"content_block_stop","index":0}`, `{"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":2}}`, `{"type":"message_stop"}`} {
					var chunk dto.ClaudeResponse
					require.NoError(t, json.Unmarshal([]byte(raw), &chunk))
					relayconvert.FormatClaudeResponseInfo(&chunk, nil, claudeInfo)
					out, err := ConvertRelayStreamChunk[dto.ClaudeResponse, dto.ResponsesStreamResponse](context.Background(), info, stream, &chunk)
					require.NoError(t, err)
					events = append(events, out...)
				}
				require.NoError(t, SetRelayResponseStreamUsage(stream, relayconvert.UsageFromClaudeUsage(claudeInfo.Usage)))
			} else {
				var chunk dto.GeminiChatResponse
				require.NoError(t, json.Unmarshal([]byte(`{"candidates":[{"content":{"role":"model","parts":[{"functionCall":{"name":"exec","args":{"input":"echo hi"}}}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":3,"candidatesTokenCount":2,"totalTokenCount":5}}`), &chunk))
				out, err := ConvertRelayStreamChunk[dto.GeminiChatResponse, dto.ResponsesStreamResponse](context.Background(), info, stream, &chunk)
				require.NoError(t, err)
				events = append(events, out...)
			}
			out, err := FinalizeRelayResponseStream[dto.ResponsesStreamResponse](context.Background(), info, stream)
			require.NoError(t, err)
			events = append(events, out...)
			require.NotEmpty(t, events)
			last := events[len(events)-1]
			require.Equal(t, "response.completed", last.Type)
			require.Len(t, last.Response.Output, 1)
			require.Equal(t, "custom_tool_call", last.Response.Output[0].Type)
			require.JSONEq(t, `"echo hi"`, string(last.Response.Output[0].Input))
			require.Equal(t, 5, last.Response.Usage.TotalTokens)
		})
	}
}
