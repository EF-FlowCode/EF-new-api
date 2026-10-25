package claude

import (
	"net/http/httptest"
	"strings"
	"testing"

	"eflow.local/new-api/common"
	"eflow.local/new-api/constant"
	"eflow.local/new-api/dto"
	relaycommon "eflow.local/new-api/relay/common"
	"eflow.local/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func newStreamFinalTestContext(estimatePromptTokens int) (*gin.Context, *relaycommon.RelayInfo) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/messages", nil)
	info := &relaycommon.RelayInfo{
		RelayFormat: types.RelayFormatClaude,
		ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "claude-opus-5-5"},
	}
	info.SetEstimatePromptTokens(estimatePromptTokens)
	return c, info
}

// 首字前客户端断开：上游一个事件都没给，不应把本地估算的 prompt 当作计费用量。
func TestHandleStreamFinalResponse_NoUpstreamEventBillsNothing(t *testing.T) {
	c, info := newStreamFinalTestContext(7_515_890)
	claudeInfo := &ClaudeResponseInfo{Usage: &dto.Usage{}}

	HandleStreamFinalResponse(c, info, claudeInfo)

	require.Zero(t, claudeInfo.Usage.PromptTokens)
	require.Zero(t, claudeInfo.Usage.CompletionTokens)
	require.Zero(t, claudeInfo.Usage.TotalTokens)
	requireBillingAnomaly(t, c, constant.BillingAnomalyNoUpstreamUsage)
}

func requireBillingAnomaly(t *testing.T, c *gin.Context, want int) {
	t.Helper()
	v, ok := common.GetContextKey(c, constant.ContextKeyBillingAnomaly)
	require.True(t, ok)
	require.Equal(t, want, v)
}

// ping 事件不算上游交付。
func TestHandleStreamFinalResponse_PingOnlyBillsNothing(t *testing.T) {
	c, info := newStreamFinalTestContext(100_000)
	claudeInfo := &ClaudeResponseInfo{Usage: &dto.Usage{}}

	require.False(t, FormatClaudeResponseInfo(&dto.ClaudeResponse{Type: "ping"}, nil, claudeInfo))
	HandleStreamFinalResponse(c, info, claudeInfo)

	require.Zero(t, claudeInfo.Usage.TotalTokens)
}

// 收到 message_start 后中断：输入与缓存用上游真实值，输出按已收到文本估算。
func TestHandleStreamFinalResponse_InterruptedAfterMessageStartKeepsUpstreamUsage(t *testing.T) {
	c, info := newStreamFinalTestContext(7_515_890)
	claudeInfo := &ClaudeResponseInfo{Usage: &dto.Usage{}}

	FormatClaudeResponseInfo(&dto.ClaudeResponse{
		Type: "message_start",
		Message: &dto.ClaudeMediaMessage{
			Id: "msg_1",
			Usage: &dto.ClaudeUsage{
				InputTokens:          4,
				CacheReadInputTokens: 200_000,
			},
		},
	}, nil, claudeInfo)
	text := "hello world"
	FormatClaudeResponseInfo(&dto.ClaudeResponse{
		Type:  "content_block_delta",
		Delta: &dto.ClaudeMediaMessage{Text: &text},
	}, nil, claudeInfo)

	HandleStreamFinalResponse(c, info, claudeInfo)

	require.Equal(t, 4, claudeInfo.Usage.PromptTokens)
	require.Equal(t, 200_000, claudeInfo.Usage.PromptTokensDetails.CachedTokens)
	require.Greater(t, claudeInfo.Usage.CompletionTokens, 0)
	requireBillingAnomaly(t, c, constant.BillingAnomalyOutputEstimated)
}

// 全缓存命中时 input_tokens=0 是合法的，不能再叠加整份估算。
func TestHandleStreamFinalResponse_FullCacheHitDoesNotAddEstimate(t *testing.T) {
	c, info := newStreamFinalTestContext(7_515_890)
	claudeInfo := &ClaudeResponseInfo{Usage: &dto.Usage{}}

	FormatClaudeResponseInfo(&dto.ClaudeResponse{
		Type: "message_start",
		Message: &dto.ClaudeMediaMessage{
			Id:    "msg_1",
			Usage: &dto.ClaudeUsage{CacheReadInputTokens: 300_000},
		},
	}, nil, claudeInfo)

	HandleStreamFinalResponse(c, info, claudeInfo)

	require.Zero(t, claudeInfo.Usage.PromptTokens)
	require.Equal(t, 300_000, claudeInfo.Usage.PromptTokensDetails.CachedTokens)
	requireBillingAnomaly(t, c, constant.BillingAnomalyOutputEstimated)
}

// 收到 message_start 但缺 input_tokens 时，仍沿用估算兜底（上游确实交付了内容）。
func TestHandleStreamFinalResponse_MissingPromptAfterEventFallsBackToEstimate(t *testing.T) {
	c, info := newStreamFinalTestContext(1234)
	claudeInfo := &ClaudeResponseInfo{Usage: &dto.Usage{}}

	text := strings.Repeat("abc ", 10)
	FormatClaudeResponseInfo(&dto.ClaudeResponse{
		Type:  "content_block_delta",
		Delta: &dto.ClaudeMediaMessage{Text: &text},
	}, nil, claudeInfo)

	HandleStreamFinalResponse(c, info, claudeInfo)

	require.Equal(t, 1234, claudeInfo.Usage.PromptTokens)
	require.Greater(t, claudeInfo.Usage.CompletionTokens, 0)
	requireBillingAnomaly(t, c, constant.BillingAnomalyInputEstimated)
}

// 按上游 1b63c28dc：只在用量确实缺失时本地估算。全缓存命中且拒答的请求保留上游的 0，
// 不打本地计数标记；message_delta 里显式的 0 输入不能抹掉 message_start 的输入。
func TestHandleStreamFinalResponse_EstimatesOnlyUncountedUsage(t *testing.T) {
	for _, tc := range []struct {
		name, startUsage, deltaUsage, stopReason string // deltaUsage 为空：流在 message_delta 前断开
		streamText                               bool
		wantInput                                int
		wantOutput                               int // -1 表示按文本估算，只要求 >0
		wantLocalCount                           bool
	}{
		{name: "cache-only refusal keeps zeros", startUsage: `{"input_tokens":0,"output_tokens":0,"cache_creation_input_tokens":20000,"cache_creation":{"ephemeral_5m_input_tokens":0,"ephemeral_1h_input_tokens":20000}}`, deltaUsage: `{"output_tokens":0}`, stopReason: "refusal"},
		{name: "refusal before output keeps zero output", startUsage: `{"input_tokens":800,"output_tokens":0}`, deltaUsage: `{"output_tokens":0}`, stopReason: "refusal", wantInput: 800},
		{name: "broken stream keeps cache-only input", startUsage: `{"input_tokens":0,"output_tokens":1,"cache_read_input_tokens":50000}`, streamText: true, wantOutput: -1, wantLocalCount: true},
		{name: "zeros after streamed text are estimated", startUsage: `{"input_tokens":0,"output_tokens":0}`, deltaUsage: `{"output_tokens":0}`, stopReason: "end_turn", streamText: true, wantInput: 1200, wantOutput: -1, wantLocalCount: true},
		{name: "zero delta input keeps start input", startUsage: `{"input_tokens":1000,"output_tokens":1}`, deltaUsage: `{"input_tokens":0,"output_tokens":50}`, stopReason: "end_turn", streamText: true, wantInput: 1000, wantOutput: 50},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, info := newStreamFinalTestContext(1200)
			state := &ClaudeResponseInfo{Usage: &dto.Usage{}}
			start := `{"type":"message_start","message":{"id":"msg_1","model":"claude-opus-5-5","usage":` + tc.startUsage + `}}`
			require.Nil(t, HandleStreamResponseData(c, info, state, nil, start))
			if tc.streamText {
				require.Nil(t, HandleStreamResponseData(c, info, state, nil, `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hello there, this is streamed text"}}`))
			}
			if tc.deltaUsage != "" {
				delta := `{"type":"message_delta","delta":{"stop_reason":"` + tc.stopReason + `"},"usage":` + tc.deltaUsage + `}`
				require.Nil(t, HandleStreamResponseData(c, info, state, nil, delta))
				require.Nil(t, HandleStreamResponseData(c, info, state, nil, `{"type":"message_stop"}`))
			}

			HandleStreamFinalResponse(c, info, state)

			require.Equal(t, tc.wantInput, state.Usage.PromptTokens)
			if tc.wantOutput < 0 {
				require.Greater(t, state.Usage.CompletionTokens, 0)
			} else {
				require.Equal(t, tc.wantOutput, state.Usage.CompletionTokens)
			}
			require.Equal(t, tc.wantLocalCount, common.GetContextKeyBool(c, constant.ContextKeyLocalCountTokens))
		})
	}
}
