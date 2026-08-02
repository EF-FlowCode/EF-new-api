package service

import (
	"context"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relaydto "github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/convmeta"
	relaytypes "github.com/QuantumNous/new-api/relaykit/types"
	hosttypes "github.com/QuantumNous/new-api/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBridgeRelayDTOPreservesExplicitZeroValuesAndVideoTokens(t *testing.T) {
	zeroFloat := 0.0
	zeroInt := 0
	falseValue := false
	host := &dto.GeneralOpenAIRequest{
		Stream:           &falseValue,
		Temperature:      &zeroFloat,
		TopP:             &zeroFloat,
		N:                &zeroInt,
		ParallelTooCalls: &falseValue,
		ReturnImages:     &falseValue,
		TopLogProbs:      &zeroInt,
		FrequencyPenalty: &zeroFloat,
		PresencePenalty:  &zeroFloat,
	}

	relayRequest, err := bridgeRelayDTO[relaydto.GeneralOpenAIRequest](host)
	require.NoError(t, err)
	require.NotNil(t, relayRequest.Stream)
	assert.False(t, *relayRequest.Stream)
	require.NotNil(t, relayRequest.Temperature)
	assert.Zero(t, *relayRequest.Temperature)
	require.NotNil(t, relayRequest.N)
	assert.Zero(t, *relayRequest.N)
	require.NotNil(t, relayRequest.ParallelTooCalls)
	assert.False(t, *relayRequest.ParallelTooCalls)

	relayResponse := &relaydto.OpenAITextResponse{
		Usage: relaydto.Usage{PromptTokensDetails: relaydto.InputTokenDetails{VideoTokens: 7}},
	}
	hostResponse, err := bridgeRelayDTO[dto.OpenAITextResponse](relayResponse)
	require.NoError(t, err)
	assert.Equal(t, 7, hostResponse.Usage.PromptTokensDetails.VideoTokens)
}

func TestSyncRelayConversionMetaReplacesExistingChain(t *testing.T) {
	info := &relaycommon.RelayInfo{RequestConversionChain: []hosttypes.RelayFormat{
		hosttypes.RelayFormatClaude,
		hosttypes.RelayFormatOpenAI,
	}}
	meta := &convmeta.Values{ConversionChain: []relaytypes.RelayFormat{
		relaytypes.RelayFormatClaude,
		relaytypes.RelayFormatOpenAI,
		relaytypes.RelayFormatOpenAIResponses,
	}}

	syncRelayConversionMeta(info, meta)

	assert.Equal(t, []hosttypes.RelayFormat{
		hosttypes.RelayFormatClaude,
		hosttypes.RelayFormatOpenAI,
		hosttypes.RelayFormatOpenAIResponses,
	}, info.RequestConversionChain)
}

func TestRelayConversionMetaAllowsMissingChannelMeta(t *testing.T) {
	meta := relayConversionMeta(&relaycommon.RelayInfo{OriginModelName: "client-model"})

	assert.Equal(t, "client-model", meta.OriginModelName)
	assert.False(t, meta.ChannelMetaAttached)
}

func TestRelayResponseStreamConvertsAndFinalizesOpenAIToClaude(t *testing.T) {
	info := &relaycommon.RelayInfo{
		IsStream:    true,
		RelayFormat: hosttypes.RelayFormatOpenAI,
		ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "claude-test"},
	}
	stream, err := NewRelayResponseStream(info, hosttypes.RelayFormatOpenAI, hosttypes.RelayFormatClaude, RelayResponseStreamOptions{
		ID:    "chatcmpl_1",
		Model: "claude-test",
	})
	require.NoError(t, err)
	content := "hello"
	chunk := &dto.ChatCompletionsStreamResponse{
		Id:      "chatcmpl_1",
		Created: 123,
		Model:   "claude-test",
		Choices: []dto.ChatCompletionsStreamResponseChoice{{
			Index: 0,
			Delta: dto.ChatCompletionsStreamResponseChoiceDelta{Content: &content},
		}},
		Usage: &dto.Usage{PromptTokens: 2, CompletionTokens: 1, TotalTokens: 3},
	}

	converted, err := ConvertRelayStreamChunk[relaydto.ChatCompletionsStreamResponse, dto.ClaudeResponse](context.Background(), info, stream, chunk)
	require.NoError(t, err)
	require.NotEmpty(t, converted)
	assert.Equal(t, "message_start", converted[0].Type)
	assert.Equal(t, 1, info.SendResponseCount)

	moreContent := " world"
	chunk.Choices[0].Delta.Content = &moreContent
	chunk.Usage = nil
	converted, err = ConvertRelayStreamChunk[relaydto.ChatCompletionsStreamResponse, dto.ClaudeResponse](context.Background(), info, stream, chunk)
	require.NoError(t, err)
	require.NotEmpty(t, converted)
	for _, response := range converted {
		assert.NotEqual(t, "message_start", response.Type)
	}
	assert.Equal(t, 2, info.SendResponseCount)

	final, err := FinalizeRelayResponseStream[dto.ClaudeResponse](context.Background(), info, stream)
	require.NoError(t, err)
	require.NotEmpty(t, final)
	assert.Equal(t, "message_stop", final[len(final)-1].Type)
	require.NotNil(t, info.ClaudeConvertInfo)
	assert.True(t, info.ClaudeConvertInfo.Done)

	repeatedFinal, err := FinalizeRelayResponseStream[dto.ClaudeResponse](context.Background(), info, stream)
	require.NoError(t, err)
	assert.Empty(t, repeatedFinal)

	usage := stream.state.Usage()
	require.NotNil(t, usage)
	assert.Equal(t, 3, usage.TotalTokens)
}

func TestConvertOpenAIRequestToClaudeUsesDefaultsAndResolvesPDF(t *testing.T) {
	request := &dto.GeneralOpenAIRequest{}
	require.NoError(t, common.Unmarshal([]byte(`{
		"model":"claude-test",
		"messages":[{"role":"user","content":[
			{"type":"text","text":"summarize"},
			{"type":"file","file":{"filename":"report.pdf","file_data":"JVBERi0xLjQ="}}
		]}]
	}`), request))

	converted, err := ConvertOpenAIRequestToClaude(context.Background(), &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "claude-test"},
	}, request)
	require.NoError(t, err)
	require.NotNil(t, converted.MaxTokens)
	assert.Equal(t, uint(8192), *converted.MaxTokens)
	require.Len(t, converted.Messages, 1)
	content, err := converted.Messages[0].ParseContent()
	require.NoError(t, err)
	require.Len(t, content, 2)
	assert.Equal(t, "document", content[1].Type)
	require.NotNil(t, content[1].Source)
	assert.Equal(t, "application/pdf", content[1].Source.MediaType)
	assert.Equal(t, "JVBERi0xLjQ=", content[1].Source.Data)
}

func TestConvertOpenAIRequestToGeminiAppliesHostSettings(t *testing.T) {
	request := &dto.GeneralOpenAIRequest{}
	require.NoError(t, common.Unmarshal([]byte(`{
		"model":"gemini-2.5-flash-image",
		"messages":[{"role":"user","content":"draw a cat"}],
		"extra_body":{"google":{"thinking_config":{"thinking_budget":0,"include_thoughts":false}}}
	}`), request))

	converted, err := ConvertOpenAIRequestToGemini(context.Background(), &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "gemini-2.5-flash-image"},
	}, request)
	require.NoError(t, err)
	assert.Equal(t, []string{"TEXT", "IMAGE"}, converted.GenerationConfig.ResponseModalities)
	require.NotEmpty(t, converted.SafetySettings)
	assert.Equal(t, "OFF", converted.SafetySettings[0].Threshold)
	require.NotNil(t, converted.GenerationConfig.ThinkingConfig)
	require.NotNil(t, converted.GenerationConfig.ThinkingConfig.ThinkingBudget)
	assert.Zero(t, *converted.GenerationConfig.ThinkingConfig.ThinkingBudget)
	assert.False(t, converted.GenerationConfig.ThinkingConfig.IncludeThoughts)
}

func TestConvertClaudeRequestToOpenAIUsesOpenRouterDialect(t *testing.T) {
	budget := 123
	maxTokens := uint(512)
	request := &dto.ClaudeRequest{
		Model:     "anthropic/claude-test",
		MaxTokens: &maxTokens,
		Messages:  []dto.ClaudeMessage{{Role: "user", Content: "hello"}},
		Thinking:  &dto.Thinking{Type: "enabled", BudgetTokens: &budget},
	}

	converted, err := ConvertClaudeRequestToOpenAI(context.Background(), &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:       constant.ChannelTypeOpenRouter,
			UpstreamModelName: request.Model,
		},
	}, request)
	require.NoError(t, err)

	var reasoning map[string]any
	require.NoError(t, common.Unmarshal(converted.Reasoning, &reasoning))
	assert.Equal(t, true, reasoning["enabled"])
	assert.Equal(t, float64(123), reasoning["max_tokens"])
}

func TestConvertClaudeResponseToOpenAIPreservesCacheUsage(t *testing.T) {
	response := &dto.ClaudeResponse{
		Id:         "msg_1",
		Type:       "message",
		Role:       "assistant",
		StopReason: "end_turn",
		Content:    []dto.ClaudeMediaMessage{{Type: "text", Text: common.GetPointer("hello")}},
		Usage: &dto.ClaudeUsage{
			InputTokens:              10,
			CacheCreationInputTokens: 4,
			CacheReadInputTokens:     3,
			OutputTokens:             5,
			CacheCreation: &dto.ClaudeCacheCreationUsage{
				Ephemeral5mInputTokens: 1,
				Ephemeral1hInputTokens: 3,
			},
		},
	}

	converted, err := ConvertClaudeResponseToOpenAI(context.Background(), nil, response)
	require.NoError(t, err)
	assert.Equal(t, 17, converted.Usage.PromptTokens)
	assert.Equal(t, 5, converted.Usage.CompletionTokens)
	assert.Equal(t, 22, converted.Usage.TotalTokens)
	assert.Equal(t, 3, converted.Usage.PromptTokensDetails.CachedTokens)
	assert.Equal(t, 4, converted.Usage.PromptTokensDetails.CachedCreationTokens)
	assert.Equal(t, 4, converted.Usage.PromptTokensDetails.CacheWriteTokens)
	assert.Equal(t, 1, converted.Usage.ClaudeCacheCreation5mTokens)
	assert.Equal(t, 3, converted.Usage.ClaudeCacheCreation1hTokens)
}
