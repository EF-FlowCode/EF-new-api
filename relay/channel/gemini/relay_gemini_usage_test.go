package gemini

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"eflow.local/new-api/common"
	"eflow.local/new-api/constant"
	"eflow.local/new-api/dto"
	relaycommon "eflow.local/new-api/relay/common"
	"eflow.local/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGeminiChatHandlerCompletionTokensExcludeToolUsePromptTokens(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

	info := &relaycommon.RelayInfo{
		RelayFormat:     types.RelayFormatGemini,
		OriginModelName: "gemini-3-flash-preview",
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "gemini-3-flash-preview",
		},
	}

	payload := dto.GeminiChatResponse{
		Candidates: []dto.GeminiChatCandidate{
			{
				Content: dto.GeminiChatContent{
					Role: "model",
					Parts: []dto.GeminiPart{
						{Text: "ok"},
					},
				},
			},
		},
		UsageMetadata: dto.GeminiUsageMetadata{
			PromptTokenCount:        151,
			ToolUsePromptTokenCount: 18329,
			CandidatesTokenCount:    1089,
			ThoughtsTokenCount:      1120,
			TotalTokenCount:         20689,
		},
	}

	body, err := common.Marshal(payload)
	require.NoError(t, err)

	resp := &http.Response{
		Body: io.NopCloser(bytes.NewReader(body)),
	}

	usage, newAPIError := GeminiChatHandler(c, info, resp)
	require.Nil(t, newAPIError)
	require.NotNil(t, usage)
	require.Equal(t, 18480, usage.PromptTokens)
	require.Equal(t, 2209, usage.CompletionTokens)
	require.Equal(t, 20689, usage.TotalTokens)
	require.Equal(t, 1120, usage.CompletionTokenDetails.ReasoningTokens)
}

func TestGeminiChatHandlerKeepsCanonicalUsageFromRelayKit(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

	info := &relaycommon.RelayInfo{
		RelayFormat:     types.RelayFormatOpenAI,
		OriginModelName: "gemini-test",
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "gemini-test",
		},
	}
	payload := dto.GeminiChatResponse{
		Candidates: []dto.GeminiChatCandidate{{
			Content: dto.GeminiChatContent{Role: "model", Parts: []dto.GeminiPart{{Text: "ok"}}},
		}},
		UsageMetadata: dto.GeminiUsageMetadata{
			PromptTokenCount:        12,
			ToolUsePromptTokenCount: 9,
			CandidatesTokenCount:    4,
			TotalTokenCount:         25,
			PromptTokensDetails: []dto.GeminiPromptTokensDetails{
				{Modality: "TEXT", TokenCount: 2},
				{Modality: "AUDIO", TokenCount: 3},
				{Modality: "IMAGE", TokenCount: 7},
			},
			ToolUsePromptTokensDetails: []dto.GeminiPromptTokensDetails{
				{Modality: "TEXT", TokenCount: 1},
				{Modality: "AUDIO", TokenCount: 2},
				{Modality: "IMAGE", TokenCount: 6},
			},
		},
	}
	body, err := common.Marshal(payload)
	require.NoError(t, err)

	usage, relayErr := GeminiChatHandler(c, info, &http.Response{
		Body: io.NopCloser(bytes.NewReader(body)),
	})

	require.Nil(t, relayErr)
	require.NotNil(t, usage)
	assert.Equal(t, 3, usage.PromptTokensDetails.TextTokens)
	assert.Equal(t, 5, usage.PromptTokensDetails.AudioTokens)
	assert.Equal(t, 13, usage.PromptTokensDetails.ImageTokens)

	var converted dto.OpenAITextResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &converted))
	assert.Equal(t, 3, converted.Usage.PromptTokensDetails.TextTokens)
	assert.Equal(t, 5, converted.Usage.PromptTokensDetails.AudioTokens)
	assert.Equal(t, 13, converted.Usage.PromptTokensDetails.ImageTokens)
}

func TestGeminiChatHandlerEmptyCandidatesUsesCanonicalUsage(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

	info := &relaycommon.RelayInfo{
		RelayFormat:     types.RelayFormatOpenAI,
		OriginModelName: "gemini-test",
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "gemini-test",
		},
	}
	payload := dto.GeminiChatResponse{
		UsageMetadata: dto.GeminiUsageMetadata{
			PromptTokenCount:        7,
			ToolUsePromptTokenCount: 3,
			TotalTokenCount:         10,
			PromptTokensDetails: []dto.GeminiPromptTokensDetails{
				{Modality: "IMAGE", TokenCount: 7},
			},
			ToolUsePromptTokensDetails: []dto.GeminiPromptTokensDetails{
				{Modality: "IMAGE", TokenCount: 3},
			},
		},
	}
	body, err := common.Marshal(payload)
	require.NoError(t, err)

	usage, relayErr := GeminiChatHandler(c, info, &http.Response{
		Body: io.NopCloser(bytes.NewReader(body)),
	})

	require.Nil(t, relayErr)
	require.NotNil(t, usage)
	assert.Equal(t, 10, usage.PromptTokensDetails.ImageTokens)
	assert.Zero(t, usage.PromptTokensDetails.TextTokens)
}

func TestGeminiStreamHandlerCompletionTokensExcludeToolUsePromptTokens(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

	oldStreamingTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 300
	t.Cleanup(func() {
		constant.StreamingTimeout = oldStreamingTimeout
	})

	info := &relaycommon.RelayInfo{
		OriginModelName: "gemini-3-flash-preview",
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "gemini-3-flash-preview",
		},
	}

	chunk := dto.GeminiChatResponse{
		Candidates: []dto.GeminiChatCandidate{
			{
				Content: dto.GeminiChatContent{
					Role: "model",
					Parts: []dto.GeminiPart{
						{Text: "partial"},
					},
				},
			},
		},
		UsageMetadata: dto.GeminiUsageMetadata{
			PromptTokenCount:        151,
			ToolUsePromptTokenCount: 18329,
			CandidatesTokenCount:    1089,
			ThoughtsTokenCount:      1120,
			TotalTokenCount:         20689,
		},
	}

	chunkData, err := common.Marshal(chunk)
	require.NoError(t, err)

	streamBody := []byte("data: " + string(chunkData) + "\n" + "data: [DONE]\n")
	resp := &http.Response{
		Body: io.NopCloser(bytes.NewReader(streamBody)),
	}

	usage, newAPIError := geminiStreamHandler(c, info, resp, func(_ string, _ *dto.GeminiChatResponse) bool {
		return true
	})
	require.Nil(t, newAPIError)
	require.NotNil(t, usage)
	require.Equal(t, 18480, usage.PromptTokens)
	require.Equal(t, 2209, usage.CompletionTokens)
	require.Equal(t, 20689, usage.TotalTokens)
	require.Equal(t, 1120, usage.CompletionTokenDetails.ReasoningTokens)
}

func TestGeminiStreamHandlerUsesCanonicalPromptModalityDetails(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

	oldStreamingTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 300
	t.Cleanup(func() {
		constant.StreamingTimeout = oldStreamingTimeout
	})

	info := &relaycommon.RelayInfo{
		OriginModelName: "gemini-test",
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "gemini-test",
		},
	}
	chunk := dto.GeminiChatResponse{
		Candidates: []dto.GeminiChatCandidate{{
			Content: dto.GeminiChatContent{Role: "model", Parts: []dto.GeminiPart{{Text: "ok"}}},
		}},
		UsageMetadata: dto.GeminiUsageMetadata{
			PromptTokenCount:        12,
			ToolUsePromptTokenCount: 9,
			CandidatesTokenCount:    4,
			TotalTokenCount:         25,
			PromptTokensDetails: []dto.GeminiPromptTokensDetails{
				{Modality: "TEXT", TokenCount: 2},
				{Modality: "AUDIO", TokenCount: 3},
				{Modality: "IMAGE", TokenCount: 7},
			},
			ToolUsePromptTokensDetails: []dto.GeminiPromptTokensDetails{
				{Modality: "TEXT", TokenCount: 1},
				{Modality: "AUDIO", TokenCount: 2},
				{Modality: "IMAGE", TokenCount: 6},
			},
		},
	}
	chunkData, err := common.Marshal(chunk)
	require.NoError(t, err)

	usage, relayErr := geminiStreamHandler(c, info, &http.Response{
		Body: io.NopCloser(strings.NewReader("data: " + string(chunkData) + "\n\ndata: [DONE]\n\n")),
	}, func(_ string, _ *dto.GeminiChatResponse) bool {
		return true
	})

	require.Nil(t, relayErr)
	require.NotNil(t, usage)
	assert.Equal(t, 21, usage.PromptTokens)
	assert.Equal(t, 3, usage.PromptTokensDetails.TextTokens)
	assert.Equal(t, 5, usage.PromptTokensDetails.AudioTokens)
	assert.Equal(t, 13, usage.PromptTokensDetails.ImageTokens)
}

func TestGeminiStreamHandlerUsesMetadataWhenTotalTokenCountIsZero(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

	oldStreamingTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 300
	t.Cleanup(func() {
		constant.StreamingTimeout = oldStreamingTimeout
	})

	info := &relaycommon.RelayInfo{
		OriginModelName: "gemini-test",
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "gemini-test",
		},
	}
	chunk := dto.GeminiChatResponse{
		Candidates: []dto.GeminiChatCandidate{{
			Content: dto.GeminiChatContent{Role: "model", Parts: []dto.GeminiPart{{Text: "ok"}}},
		}},
		UsageMetadata: dto.GeminiUsageMetadata{
			PromptTokenCount:        7,
			ToolUsePromptTokenCount: 3,
			CandidatesTokenCount:    2,
			PromptTokensDetails: []dto.GeminiPromptTokensDetails{
				{Modality: "IMAGE", TokenCount: 7},
			},
			ToolUsePromptTokensDetails: []dto.GeminiPromptTokensDetails{
				{Modality: "IMAGE", TokenCount: 3},
			},
		},
	}
	chunkData, err := common.Marshal(chunk)
	require.NoError(t, err)

	usage, relayErr := geminiStreamHandler(c, info, &http.Response{
		Body: io.NopCloser(strings.NewReader("data: " + string(chunkData) + "\n\ndata: [DONE]\n\n")),
	}, func(_ string, _ *dto.GeminiChatResponse) bool {
		return true
	})

	require.Nil(t, relayErr)
	require.NotNil(t, usage)
	assert.Equal(t, 10, usage.PromptTokens)
	assert.Equal(t, 2, usage.CompletionTokens)
	assert.Equal(t, 10, usage.PromptTokensDetails.ImageTokens)
	assert.Zero(t, usage.TotalTokens)
}

func TestGeminiChatStreamHandlerOpenAIUsageChunkShape(t *testing.T) {
	gin.SetMode(gin.TestMode)

	oldStreamingTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 300
	t.Cleanup(func() {
		constant.StreamingTimeout = oldStreamingTimeout
	})

	for _, includeUsage := range []bool{false, true} {
		t.Run(map[bool]string{false: "without usage", true: "with usage"}[includeUsage], func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

			info := &relaycommon.RelayInfo{
				IsStream:           true,
				RelayFormat:        types.RelayFormatOpenAI,
				OriginModelName:    "gemini-3-flash-preview",
				ShouldIncludeUsage: includeUsage,
				ChannelMeta: &relaycommon.ChannelMeta{
					UpstreamModelName: "gemini-3-flash-preview",
				},
			}
			stop := "STOP"
			chunk := dto.GeminiChatResponse{
				Candidates: []dto.GeminiChatCandidate{{
					Content:      dto.GeminiChatContent{Role: "model", Parts: []dto.GeminiPart{{Text: "done"}}},
					FinishReason: &stop,
				}},
				UsageMetadata: dto.GeminiUsageMetadata{
					PromptTokenCount:     4,
					CandidatesTokenCount: 2,
					TotalTokenCount:      6,
				},
			}
			chunkData, err := common.Marshal(chunk)
			require.NoError(t, err)
			resp := &http.Response{Body: io.NopCloser(strings.NewReader("data: " + string(chunkData) + "\n\ndata: [DONE]\n\n"))}

			usage, relayErr := GeminiChatStreamHandler(c, info, resp)
			require.Nil(t, relayErr)
			require.Equal(t, 6, usage.TotalTokens)

			usageChunks := 0
			for line := range strings.SplitSeq(recorder.Body.String(), "\n") {
				data := strings.TrimPrefix(line, "data: ")
				if data == line || data == "[DONE]" || data == "" {
					continue
				}
				var response dto.ChatCompletionsStreamResponse
				require.NoError(t, common.UnmarshalJsonStr(data, &response))
				if response.Usage != nil && response.Usage.TotalTokens > 0 {
					usageChunks++
					assert.Empty(t, response.Choices)
				}
			}
			if includeUsage {
				assert.Equal(t, 1, usageChunks)
			} else {
				assert.Zero(t, usageChunks)
			}
		})
	}
}

func TestGeminiTextGenerationHandlerPromptTokensIncludeToolUsePromptTokens(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1beta/models/gemini-3-flash-preview:generateContent", nil)

	info := &relaycommon.RelayInfo{
		OriginModelName: "gemini-3-flash-preview",
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "gemini-3-flash-preview",
		},
	}

	payload := dto.GeminiChatResponse{
		Candidates: []dto.GeminiChatCandidate{
			{
				Content: dto.GeminiChatContent{
					Role: "model",
					Parts: []dto.GeminiPart{
						{Text: "ok"},
					},
				},
			},
		},
		UsageMetadata: dto.GeminiUsageMetadata{
			PromptTokenCount:        151,
			ToolUsePromptTokenCount: 18329,
			CandidatesTokenCount:    1089,
			ThoughtsTokenCount:      1120,
			TotalTokenCount:         20689,
		},
	}

	body, err := common.Marshal(payload)
	require.NoError(t, err)

	resp := &http.Response{
		Body: io.NopCloser(bytes.NewReader(body)),
	}

	usage, newAPIError := GeminiTextGenerationHandler(c, info, resp)
	require.Nil(t, newAPIError)
	require.NotNil(t, usage)
	require.Equal(t, 18480, usage.PromptTokens)
	require.Equal(t, 2209, usage.CompletionTokens)
	require.Equal(t, 20689, usage.TotalTokens)
	require.Equal(t, 1120, usage.CompletionTokenDetails.ReasoningTokens)
}

func TestGeminiTextGenerationHandlerUsesCanonicalPromptModalityDetails(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1beta/models/gemini-test:generateContent", nil)

	info := &relaycommon.RelayInfo{
		OriginModelName: "gemini-test",
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "gemini-test",
		},
	}
	payload := dto.GeminiChatResponse{
		Candidates: []dto.GeminiChatCandidate{{
			Content: dto.GeminiChatContent{Role: "model", Parts: []dto.GeminiPart{{Text: "ok"}}},
		}},
		UsageMetadata: dto.GeminiUsageMetadata{
			PromptTokenCount:        12,
			ToolUsePromptTokenCount: 9,
			TotalTokenCount:         21,
			PromptTokensDetails: []dto.GeminiPromptTokensDetails{
				{Modality: "TEXT", TokenCount: 2},
				{Modality: "AUDIO", TokenCount: 3},
				{Modality: "IMAGE", TokenCount: 7},
			},
			ToolUsePromptTokensDetails: []dto.GeminiPromptTokensDetails{
				{Modality: "TEXT", TokenCount: 1},
				{Modality: "AUDIO", TokenCount: 2},
				{Modality: "IMAGE", TokenCount: 6},
			},
		},
	}
	body, err := common.Marshal(payload)
	require.NoError(t, err)

	usage, relayErr := GeminiTextGenerationHandler(c, info, &http.Response{
		Body: io.NopCloser(bytes.NewReader(body)),
	})

	require.Nil(t, relayErr)
	require.NotNil(t, usage)
	assert.Equal(t, 21, usage.PromptTokens)
	assert.Equal(t, 3, usage.PromptTokensDetails.TextTokens)
	assert.Equal(t, 5, usage.PromptTokensDetails.AudioTokens)
	assert.Equal(t, 13, usage.PromptTokensDetails.ImageTokens)
}

func TestGeminiTextGenerationHandlerPreservesUsageMetadataPresence(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		metadata  string
		wantUsage bool
	}{
		{name: "missing", metadata: "", wantUsage: false},
		{name: "empty object", metadata: `,"usageMetadata":{}`, wantUsage: true},
		{name: "explicit zero values", metadata: `,"usageMetadata":{"promptTokenCount":0,"candidatesTokenCount":0,"totalTokenCount":0}`, wantUsage: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1beta/models/gemini-test:generateContent", nil)
			info := &relaycommon.RelayInfo{
				OriginModelName: "gemini-test",
				ChannelMeta: &relaycommon.ChannelMeta{
					UpstreamModelName: "gemini-test",
				},
			}
			body := `{"candidates":[{"content":{"role":"model","parts":[{"text":"ok"}]}}]` + tt.metadata + `}`

			usage, relayErr := GeminiTextGenerationHandler(c, info, &http.Response{
				Body: io.NopCloser(strings.NewReader(body)),
			})

			require.Nil(t, relayErr)
			if tt.wantUsage {
				require.NotNil(t, usage)
				assert.Zero(t, usage.TotalTokens)
			} else {
				assert.Nil(t, usage)
			}
		})
	}
}

func TestGeminiChatHandlerEmptyCandidatesPreservesUsageMetadataPresence(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		metadata  string
		wantUsage bool
	}{
		{name: "missing", metadata: "", wantUsage: false},
		{name: "empty object", metadata: `,"usageMetadata":{}`, wantUsage: true},
		{name: "explicit zero values", metadata: `,"usageMetadata":{"promptTokenCount":0,"candidatesTokenCount":0,"totalTokenCount":0}`, wantUsage: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
			info := &relaycommon.RelayInfo{
				RelayFormat:     types.RelayFormatOpenAI,
				OriginModelName: "gemini-test",
				ChannelMeta: &relaycommon.ChannelMeta{
					UpstreamModelName: "gemini-test",
				},
			}
			body := `{"candidates":[]` + tt.metadata + `}`

			usage, relayErr := GeminiChatHandler(c, info, &http.Response{
				Body: io.NopCloser(strings.NewReader(body)),
			})

			require.Nil(t, relayErr)
			if tt.wantUsage {
				require.NotNil(t, usage)
				assert.Zero(t, usage.TotalTokens)
			} else {
				assert.Nil(t, usage)
			}
		})
	}
}

func TestGeminiStreamHandlerOnlyEstimatesWhenUsageMetadataIsMissing(t *testing.T) {
	oldStreamingTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 300
	t.Cleanup(func() { constant.StreamingTimeout = oldStreamingTimeout })

	tests := []struct {
		name           string
		metadata       string
		wantCompletion bool
	}{
		{name: "missing", metadata: "", wantCompletion: true},
		{name: "empty object", metadata: `,"usageMetadata":{}`, wantCompletion: false},
		{name: "explicit zero values", metadata: `,"usageMetadata":{"promptTokenCount":0,"candidatesTokenCount":0,"totalTokenCount":0}`, wantCompletion: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
			info := &relaycommon.RelayInfo{
				OriginModelName: "gemini-test",
				ChannelMeta: &relaycommon.ChannelMeta{
					UpstreamModelName: "gemini-test",
				},
			}
			body := `{"candidates":[{"content":{"role":"model","parts":[{"text":"hello"}]}}]` + tt.metadata + `}`
			stream := "data: " + body + "\n\ndata: [DONE]\n\n"

			usage, relayErr := geminiStreamHandler(c, info, &http.Response{
				Body: io.NopCloser(strings.NewReader(stream)),
			}, func(_ string, _ *dto.GeminiChatResponse) bool { return true })

			require.Nil(t, relayErr)
			require.NotNil(t, usage)
			if tt.wantCompletion {
				assert.Positive(t, usage.CompletionTokens)
			} else {
				assert.Zero(t, usage.CompletionTokens)
				assert.Zero(t, usage.TotalTokens)
			}
		})
	}
}

func TestGeminiChatHandlerUsesEstimatedPromptTokensWhenUsagePromptMissing(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

	info := &relaycommon.RelayInfo{
		RelayFormat:     types.RelayFormatGemini,
		OriginModelName: "gemini-3-flash-preview",
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "gemini-3-flash-preview",
		},
	}
	info.SetEstimatePromptTokens(20)

	payload := dto.GeminiChatResponse{
		Candidates: []dto.GeminiChatCandidate{
			{
				Content: dto.GeminiChatContent{
					Role: "model",
					Parts: []dto.GeminiPart{
						{Text: "ok"},
					},
				},
			},
		},
		UsageMetadata: dto.GeminiUsageMetadata{
			PromptTokenCount:        0,
			ToolUsePromptTokenCount: 0,
			CandidatesTokenCount:    90,
			ThoughtsTokenCount:      10,
			TotalTokenCount:         110,
		},
	}

	body, err := common.Marshal(payload)
	require.NoError(t, err)

	resp := &http.Response{
		Body: io.NopCloser(bytes.NewReader(body)),
	}

	usage, newAPIError := GeminiChatHandler(c, info, resp)
	require.Nil(t, newAPIError)
	require.NotNil(t, usage)
	require.Equal(t, 20, usage.PromptTokens)
	require.Equal(t, 100, usage.CompletionTokens)
	require.Equal(t, 110, usage.TotalTokens)
	require.Equal(t, 20, usage.PromptTokensDetails.TextTokens)
}

func TestGeminiStreamHandlerUsesEstimatedPromptTokensWhenUsagePromptMissing(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

	oldStreamingTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 300
	t.Cleanup(func() {
		constant.StreamingTimeout = oldStreamingTimeout
	})

	info := &relaycommon.RelayInfo{
		OriginModelName: "gemini-3-flash-preview",
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "gemini-3-flash-preview",
		},
	}
	info.SetEstimatePromptTokens(20)

	chunk := dto.GeminiChatResponse{
		Candidates: []dto.GeminiChatCandidate{
			{
				Content: dto.GeminiChatContent{
					Role: "model",
					Parts: []dto.GeminiPart{
						{Text: "partial"},
					},
				},
			},
		},
		UsageMetadata: dto.GeminiUsageMetadata{
			PromptTokenCount:        0,
			ToolUsePromptTokenCount: 0,
			CandidatesTokenCount:    90,
			ThoughtsTokenCount:      10,
			TotalTokenCount:         110,
		},
	}

	chunkData, err := common.Marshal(chunk)
	require.NoError(t, err)

	streamBody := []byte("data: " + string(chunkData) + "\n" + "data: [DONE]\n")
	resp := &http.Response{
		Body: io.NopCloser(bytes.NewReader(streamBody)),
	}

	usage, newAPIError := geminiStreamHandler(c, info, resp, func(_ string, _ *dto.GeminiChatResponse) bool {
		return true
	})
	require.Nil(t, newAPIError)
	require.NotNil(t, usage)
	require.Equal(t, 20, usage.PromptTokens)
	require.Equal(t, 100, usage.CompletionTokens)
	require.Equal(t, 110, usage.TotalTokens)
	require.Equal(t, 20, usage.PromptTokensDetails.TextTokens)
}

func TestGeminiTextGenerationHandlerUsesEstimatedPromptTokensWhenUsagePromptMissing(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1beta/models/gemini-3-flash-preview:generateContent", nil)

	info := &relaycommon.RelayInfo{
		OriginModelName: "gemini-3-flash-preview",
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "gemini-3-flash-preview",
		},
	}
	info.SetEstimatePromptTokens(20)

	payload := dto.GeminiChatResponse{
		Candidates: []dto.GeminiChatCandidate{
			{
				Content: dto.GeminiChatContent{
					Role: "model",
					Parts: []dto.GeminiPart{
						{Text: "ok"},
					},
				},
			},
		},
		UsageMetadata: dto.GeminiUsageMetadata{
			PromptTokenCount:        0,
			ToolUsePromptTokenCount: 0,
			CandidatesTokenCount:    90,
			ThoughtsTokenCount:      10,
			TotalTokenCount:         110,
		},
	}

	body, err := common.Marshal(payload)
	require.NoError(t, err)

	resp := &http.Response{
		Body: io.NopCloser(bytes.NewReader(body)),
	}

	usage, newAPIError := GeminiTextGenerationHandler(c, info, resp)
	require.Nil(t, newAPIError)
	require.NotNil(t, usage)
	require.Equal(t, 20, usage.PromptTokens)
	require.Equal(t, 100, usage.CompletionTokens)
	require.Equal(t, 110, usage.TotalTokens)
	require.Equal(t, 20, usage.PromptTokensDetails.TextTokens)
}
