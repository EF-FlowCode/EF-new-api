package aws

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"eflow.local/new-api/common"
	relaycommon "eflow.local/new-api/relay/common"
	"eflow.local/new-api/types"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestNewAwsInvokeContextInheritsParentCancellation(t *testing.T) {
	parent, cancelParent := context.WithCancel(context.Background())
	ctx, cancel := newAwsInvokeContext(parent)
	defer cancel()
	cancelParent()

	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("AWS invoke context did not inherit parent cancellation")
	}
}

func TestNewAwsInvokeErrorSkipsRetryAfterClientCancellation(t *testing.T) {
	parent, cancel := context.WithCancel(context.Background())
	cancel()
	err := newAwsInvokeError(parent, errors.New("transport closed"), "InvokeModel")
	require.NotNil(t, err)
	require.True(t, err.IsSkipRetry())
	require.Equal(t, types.ErrorCodeAwsInvokeError, err.GetErrorCode())
}

func TestDoAwsClientRequest_AppliesRuntimeHeaderOverrideToAnthropicBeta(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)

	info := &relaycommon.RelayInfo{
		OriginModelName:           "claude-3-5-sonnet-20240620",
		IsStream:                  false,
		UseRuntimeHeadersOverride: true,
		RuntimeHeadersOverride: map[string]any{
			"anthropic-beta": "computer-use-2025-01-24",
		},
		ChannelMeta: &relaycommon.ChannelMeta{
			ApiKey:            "access-key|secret-key|us-east-1",
			UpstreamModelName: "claude-3-5-sonnet-20240620",
		},
	}

	requestBody := bytes.NewBufferString(`{"messages":[{"role":"user","content":"hello"}],"max_tokens":128}`)
	adaptor := &Adaptor{}

	_, err := doAwsClientRequest(ctx, info, adaptor, requestBody)
	require.NoError(t, err)

	awsReq, ok := adaptor.AwsReq.(*bedrockruntime.InvokeModelInput)
	require.True(t, ok)

	var payload map[string]any
	require.NoError(t, common.Unmarshal(awsReq.Body, &payload))

	anthropicBeta, exists := payload["anthropic_beta"]
	require.True(t, exists)

	values, ok := anthropicBeta.([]any)
	require.True(t, ok)
	require.Equal(t, []any{"computer-use-2025-01-24"}, values)
}

func TestFormatRequestPreservesExplicitZeroValuesAndContextManagement(t *testing.T) {
	body := strings.NewReader(`{
		"messages": [{"role": "user", "content": "hello"}],
		"max_tokens": 0,
		"temperature": 0,
		"top_p": 0,
		"top_k": 0,
		"context_management": {"edits": []}
	}`)

	request, err := formatRequest(body, make(http.Header))
	require.NoError(t, err)
	require.NotNil(t, request.MaxTokens)
	require.Zero(t, *request.MaxTokens)
	require.NotNil(t, request.Temperature)
	require.Zero(t, *request.Temperature)
	require.NotNil(t, request.TopP)
	require.Zero(t, *request.TopP)
	require.NotNil(t, request.TopK)
	require.Zero(t, *request.TopK)
	require.JSONEq(t, `{"edits": []}`, string(request.ContextManagement))

	encoded, err := common.Marshal(request)
	require.NoError(t, err)
	var payload map[string]any
	require.NoError(t, common.Unmarshal(encoded, &payload))
	for _, key := range []string{"max_tokens", "temperature", "top_p", "top_k", "context_management"} {
		require.Contains(t, payload, key)
	}
}

func TestDoAwsClientRequestForwardsThinkingBlockBindingAndBetaList(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	ctx.Request.Header.Add("anthropic-beta", "fast-mode-2026-02-01, ")
	ctx.Request.Header.Add("anthropic-beta", "thinking-binding-controls-2026-08-01")

	info := &relaycommon.RelayInfo{
		OriginModelName: "claude-opus-5-5",
		ChannelMeta: &relaycommon.ChannelMeta{
			ApiKey:            "access-key|secret-key|us-east-1",
			UpstreamModelName: "claude-opus-5-5",
		},
	}
	requestBody := bytes.NewBufferString(`{"max_tokens":128,"messages":[{"role":"user","content":"hello"}],` +
		`"thinking":{"type":"adaptive","display":"summarized","block_binding":{"prefix_mismatch_behavior":"drop_block"}}}`)
	adaptor := &Adaptor{}

	_, err := doAwsClientRequest(ctx, info, adaptor, requestBody)
	require.NoError(t, err)
	awsReq, ok := adaptor.AwsReq.(*bedrockruntime.InvokeModelInput)
	require.True(t, ok)

	var payload struct {
		AnthropicBeta []string       `json:"anthropic_beta"`
		Thinking      map[string]any `json:"thinking"`
	}
	require.NoError(t, common.Unmarshal(awsReq.Body, &payload))
	require.Equal(t, []string{"fast-mode-2026-02-01", "thinking-binding-controls-2026-08-01"}, payload.AnthropicBeta)
	require.Equal(t, map[string]any{"prefix_mismatch_behavior": "drop_block"}, payload.Thinking["block_binding"])
}
