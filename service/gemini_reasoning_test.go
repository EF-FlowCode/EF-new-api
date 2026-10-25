package service

import (
	"testing"

	"eflow.local/new-api/dto"
	"eflow.local/new-api/internal/testconfig"
	relaycommon "eflow.local/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"net/http/httptest"
)

func TestGeminiReasoningMappingPreservesBillingAndResetsOnRetry(t *testing.T) {
	testconfig.Set(t, "gemini", map[string]string{"thinking_adapter_enabled": "true"})
	info := &relaycommon.RelayInfo{OriginModelName: "gemini-3.7-flash-low", ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "gemini-3.7-flash-high"}}
	req := &dto.GeminiChatRequest{}
	require.NoError(t, ApplyGeminiThinking(req, info))
	require.Equal(t, "high", req.GenerationConfig.ThinkingConfig.ThinkingLevel)
	require.Equal(t, "gemini-3.7-flash", info.UpstreamModelName)
	require.Equal(t, "gemini-3.7-flash-low", info.OriginModelName)
	require.NoError(t, ApplyGeminiThinking(req, info))
	require.Equal(t, "high", info.ReasoningEffort)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	info.InitChannelMeta(ctx)
	require.Nil(t, info.GeminiReasoningState)
}

func TestGeminiReasoningSuffixExemption(t *testing.T) {
	testconfig.Set(t, "gemini", map[string]string{"thinking_adapter_enabled": "true"})
	testconfig.Set(t, "global", map[string]string{"thinking_model_blacklist": `["gemini-3.7-flash-high"]`})
	info := &relaycommon.RelayInfo{OriginModelName: "public-model", ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "gemini-3.7-flash-high"}}
	req := &dto.GeminiChatRequest{}
	require.NoError(t, ApplyGeminiThinking(req, info))
	require.Nil(t, req.GenerationConfig.ThinkingConfig)
	require.Nil(t, info.GeminiReasoningState)
	require.Equal(t, "gemini-3.7-flash-high", info.UpstreamModelName)
}

func TestGeminiReasoningDisabledAliasAndPassThrough(t *testing.T) {
	testconfig.Set(t, "gemini", map[string]string{"thinking_adapter_enabled": "false"})
	info := &relaycommon.RelayInfo{OriginModelName: "gemini-2.5-flash-thinking-4096", ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "gemini-2.5-flash-thinking-4096"}}
	req := &dto.GeminiChatRequest{}
	require.NoError(t, ApplyGeminiThinking(req, info))
	require.Nil(t, req.GenerationConfig.ThinkingConfig)
	require.Equal(t, info.OriginModelName, info.UpstreamModelName)
	testconfig.Set(t, "gemini", map[string]string{"thinking_adapter_enabled": "true"})
	testconfig.Set(t, "global", map[string]string{"pass_through_request_enabled": "true"})
	require.NoError(t, ApplyGeminiThinking(req, info))
	require.Nil(t, req.GenerationConfig.ThinkingConfig)
	require.Equal(t, info.OriginModelName, info.UpstreamModelName)
}
