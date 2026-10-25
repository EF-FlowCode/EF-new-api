package openai

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"eflow.local/new-api/constant"
	"eflow.local/new-api/dto"
	relaycommon "eflow.local/new-api/relay/common"
	"eflow.local/new-api/relaykit/relayconvert/convmeta"
	"github.com/gin-gonic/gin"
	"github.com/samber/lo"
	"github.com/stretchr/testify/require"
)

func TestOpenAICompatibilityCapabilities(t *testing.T) {
	for _, tc := range []struct {
		model, effort, finalModel, finalEffort string
		sampling, modern                       bool
	}{
		{"gpt-5", "", "gpt-5", "", false, true},
		{"gpt-5.2", "none", "gpt-5.2", "none", true, true},
		{"gpt-5.2", "high", "gpt-5.2", "high", false, true},
		{"gpt-5.4-pro", "none", "gpt-5.4-pro", "none", false, true},
		{"gpt-6-sol", "", "gpt-6-sol", "", true, true},
		{"gpt-6-luna-2026-09-30", "none", "gpt-6-luna-2026-09-30", "none", true, true},
		{"gpt-6-astra", "none", "gpt-6-astra", "none", false, true},
		{"gpt-6-sol-high", "none", "gpt-6-sol", "high", false, true},
		{"gpt-6-sol-2026-99-99", "", "gpt-6-sol-2026-99-99", "", true, false},
		{"gpt-6-sol-custom", "", "gpt-6-sol-custom", "", true, false},
		{"vendor-gpt-5", "", "vendor-gpt-5", "", true, false},
	} {
		t.Run(tc.model+tc.effort, func(t *testing.T) {
			info := &relaycommon.RelayInfo{OriginModelName: "mapped-alias", ChannelMeta: &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeOpenAI, UpstreamModelName: tc.model}}
			req := &dto.GeneralOpenAIRequest{Model: tc.model, ReasoningEffort: tc.effort, MaxTokens: lo.ToPtr(uint(17)), Temperature: lo.ToPtr(0.0), TopP: lo.ToPtr(0.0), LogProbs: lo.ToPtr(false), TopLogProbs: lo.ToPtr(0), Messages: []dto.Message{{Role: "system", Content: "policy"}}}
			_, err := (&Adaptor{}).ConvertOpenAIRequest(nil, info, req)
			require.NoError(t, err)
			require.Equal(t, tc.finalModel, req.Model)
			require.Equal(t, tc.finalEffort, info.ReasoningEffort)
			require.Equal(t, tc.sampling, req.Temperature != nil)
			require.Equal(t, tc.sampling, req.TopP != nil)
			require.Equal(t, tc.sampling, req.LogProbs != nil)
			require.Equal(t, tc.sampling, req.TopLogProbs != nil)
			require.Equal(t, tc.modern, req.MaxCompletionTokens != nil)
			require.Equal(t, !tc.modern, req.MaxTokens != nil)
			if tc.modern {
				require.Equal(t, "developer", req.Messages[0].Role)
			} else {
				require.Equal(t, "system", req.Messages[0].Role)
			}
		})
	}
}

func TestResponsesChatBridgeStreamContract(t *testing.T) {
	old := constant.StreamingTimeout
	constant.StreamingTimeout = 5
	t.Cleanup(func() { constant.StreamingTimeout = old })
	first := `{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"c1","function":{"arguments":"{\"input\":\"echo hi\"}"}}]}}]}`
	name := `{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"name":"exec"}}]},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":10,"completion_tokens":3,"total_tokens":13,"prompt_tokens_details":{"cached_tokens":4,"video_tokens":2}}}`
	for _, tc := range []struct {
		name, tail          string
		cut, wantIncomplete bool
	}{
		{"success", "data: " + name + "\n\ndata: [DONE]\n\n", false, false},
		{"upstream_error", "data: {\"error\":{\"type\":\"server_error\",\"message\":\"failed\"}}\n\n", false, true},
		{"malformed", "data: {invalid}\n\n", false, true},
		{"read_failure", "", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
			info := &relaycommon.RelayInfo{OriginModelName: "client", IsStream: true, DisablePing: true, ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "gpt-test"}, ResponsesTools: &convmeta.ResponsesToolState{CustomToolNames: map[string]struct{}{"exec": {}}}}
			var reader io.Reader = strings.NewReader("data: " + first + "\n\n" + tc.tail)
			if tc.cut {
				reader = io.MultiReader(reader, responsesCutReader{})
			}
			usage, apiErr := OaiChatToResponsesStreamHandler(c, info, &http.Response{StatusCode: 200, Body: io.NopCloser(reader)})
			require.Nil(t, apiErr)
			if tc.wantIncomplete {
				// response.created already reached the client: settle the delivered
				// part and close the stream as incomplete instead of failing.
				require.Positive(t, usage.CompletionTokens)
				require.Contains(t, recorder.Body.String(), "event: response.incomplete")
				require.Contains(t, recorder.Body.String(), `"incomplete_details":{"reason":"interrupted"}`)
				require.NotContains(t, recorder.Body.String(), "response.completed")
				return
			}
			require.Equal(t, 13, usage.TotalTokens)
			require.Equal(t, 4, usage.PromptTokensDetails.CachedTokens)
			require.Equal(t, 2, usage.PromptTokensDetails.VideoTokens)
			sequence := 0
			var completed *dto.OpenAIResponsesResponse
			for line := range strings.SplitSeq(recorder.Body.String(), "\n") {
				if !strings.HasPrefix(line, "data: ") {
					continue
				}
				var event dto.ResponsesStreamResponse
				require.NoError(t, json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &event))
				require.NotNil(t, event.SequenceNumber)
				require.Equal(t, sequence, *event.SequenceNumber)
				sequence++
				if event.Type == "response.completed" {
					completed = event.Response
				}
			}
			require.NotNil(t, completed)
			require.Len(t, completed.Output, 1)
			require.Equal(t, "custom_tool_call", completed.Output[0].Type)
			require.JSONEq(t, `"echo hi"`, string(completed.Output[0].Input))
			require.Equal(t, 2, completed.Usage.InputTokensDetails.VideoTokens)
		})
	}
}

func TestResponsesChatBridgeStreamIgnoresZeroUsage(t *testing.T) {
	old := constant.StreamingTimeout
	constant.StreamingTimeout = 5
	t.Cleanup(func() { constant.StreamingTimeout = old })
	zero := `"usage":{"prompt_tokens":0,"completion_tokens":0,"total_tokens":0}`
	body := "data: {\"choices\":[{\"delta\":{\"content\":\"hello world\"}}]," + zero + "}\n\n" +
		"data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]," + zero + "}\n\ndata: [DONE]\n\n"
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	info := &relaycommon.RelayInfo{OriginModelName: "client", IsStream: true, DisablePing: true, ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "gpt-test"}}
	info.SetEstimatePromptTokens(7)
	usage, apiErr := OaiChatToResponsesStreamHandler(c, info, &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))})
	require.Nil(t, apiErr)
	require.Equal(t, 7, usage.PromptTokens, "all-zero upstream usage falls back to the estimate")
	require.Positive(t, usage.CompletionTokens)
	require.Contains(t, recorder.Body.String(), "response.completed")
}

func TestResponsesChatBridgeStreamErrorBeforeOutputIsRetryable(t *testing.T) {
	old := constant.StreamingTimeout
	constant.StreamingTimeout = 5
	t.Cleanup(func() { constant.StreamingTimeout = old })
	for name, body := range map[string]string{
		"upstream_error": "data: {\"error\":{\"type\":\"server_error\",\"message\":\"failed\"}}\n\n",
		"malformed":      "data: {invalid}\n\n",
	} {
		t.Run(name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
			info := &relaycommon.RelayInfo{OriginModelName: "client", IsStream: true, DisablePing: true, ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "gpt-test"}}
			usage, apiErr := OaiChatToResponsesStreamHandler(c, info, &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))})
			require.NotNil(t, apiErr)
			require.Nil(t, usage)
			require.False(t, c.Writer.Written(), "nothing committed, so the relay may retry")
		})
	}
}
