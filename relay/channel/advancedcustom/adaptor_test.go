package advancedcustom

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"eflow.local/new-api/constant"
	"eflow.local/new-api/dto"
	relaycommon "eflow.local/new-api/relay/common"
	relayconstant "eflow.local/new-api/relay/constant"
	"eflow.local/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAdaptorMatchesAdvancedRouteByModel(t *testing.T) {
	info := advancedCustomTestInfo(&dto.AdvancedCustomConfig{
		Routes: []dto.AdvancedCustomRoute{
			{IncomingPath: "/v1/chat/completions", UpstreamPath: "/provider/gpt", Models: []string{"re:^gpt-"}},
			{IncomingPath: "/v1/chat/completions", UpstreamPath: "/provider/fallback"},
		},
	})
	info.OriginModelName = "gpt-4o"
	info.UpstreamModelName = "gpt-4o"

	requestURL, err := (&Adaptor{}).GetRequestURL(info)
	require.NoError(t, err)
	require.Equal(t, "https://gateway.example/provider/gpt", requestURL)
}

func TestAdaptorUsesCanonicalPathForPlaygroundRequests(t *testing.T) {
	info := advancedCustomTestInfo(&dto.AdvancedCustomConfig{Routes: []dto.AdvancedCustomRoute{{
		IncomingPath: "/v1/chat/completions",
		UpstreamPath: "/provider/chat",
	}}})
	info.RequestURLPath = "/v1/chat/completions"
	context := advancedCustomTestContext("/pg/chat/completions")

	require.Equal(t, "/v1/chat/completions", incomingRequestPath(context, info))
	info.RequestURLPath = "/pg/chat/completions?stream=true"
	require.Equal(t, "/v1/chat/completions", incomingRequestPath(context, info))

	request := &dto.GeneralOpenAIRequest{Model: "gpt-test"}
	_, err := (&Adaptor{}).ConvertOpenAIRequest(context, info, request)
	require.NoError(t, err)
}

func TestAdaptorConvertsResponsesRequestToGemini(t *testing.T) {
	info := advancedCustomTestInfo(&dto.AdvancedCustomConfig{Routes: []dto.AdvancedCustomRoute{{
		IncomingPath: "/v1/responses",
		UpstreamPath: "/v1beta/models/{model}:generateContent",
		Converter:    dto.AdvancedCustomConverterOpenAIResponsesToGeminiGenerateContent,
	}}})
	info.OriginModelName = "gemini-2.5-flash"
	info.UpstreamModelName = "gemini-2.5-flash"
	info.RelayFormat = types.RelayFormatOpenAIResponses
	info.RelayMode = relayconstant.RelayModeResponses
	request := dto.OpenAIResponsesRequest{Model: "gemini-2.5-flash", Input: []byte(`"hello"`)}

	converted, err := (&Adaptor{}).ConvertOpenAIResponsesRequest(advancedCustomTestContext("/v1/responses"), info, request)
	require.NoError(t, err)
	geminiRequest, ok := converted.(*dto.GeminiChatRequest)
	require.True(t, ok)
	require.Len(t, geminiRequest.Contents, 1)
	require.Equal(t, "hello", geminiRequest.Contents[0].Parts[0].Text)
}

func TestAdaptorConvertsGeminiResponseToResponses(t *testing.T) {
	info := advancedCustomTestInfo(&dto.AdvancedCustomConfig{Routes: []dto.AdvancedCustomRoute{{
		IncomingPath: "/v1/responses",
		UpstreamPath: "/v1beta/models/{model}:generateContent",
		Converter:    dto.AdvancedCustomConverterOpenAIResponsesToGeminiGenerateContent,
	}}})
	info.OriginModelName = "gemini-2.5-flash"
	info.UpstreamModelName = "gemini-2.5-flash"
	info.RelayFormat = types.RelayFormatOpenAIResponses
	info.RelayMode = relayconstant.RelayModeResponses
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	c.Request.Header.Set("Content-Type", "application/json")
	body := `{"candidates":[{"content":{"role":"model","parts":[{"text":"hello"}]}}],"usageMetadata":{"promptTokenCount":2,"candidatesTokenCount":3,"totalTokenCount":5}}`

	usage, apiErr := (&Adaptor{}).DoResponse(c, &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}, info)
	require.Nil(t, apiErr)
	require.NotNil(t, usage)
	require.Contains(t, recorder.Body.String(), `"object":"response"`)
	require.Contains(t, recorder.Body.String(), `"hello"`)
}

func TestAdaptorGeminiResponseUsesCanonicalUsageWithoutTotalTokens(t *testing.T) {
	info := advancedCustomTestInfo(&dto.AdvancedCustomConfig{Routes: []dto.AdvancedCustomRoute{{
		IncomingPath: "/v1/responses",
		UpstreamPath: "/v1beta/models/{model}:generateContent",
		Converter:    dto.AdvancedCustomConverterOpenAIResponsesToGeminiGenerateContent,
	}}})
	info.OriginModelName = "gemini-2.5-flash"
	info.UpstreamModelName = "gemini-2.5-flash"
	info.RelayFormat = types.RelayFormatOpenAIResponses
	info.RelayMode = relayconstant.RelayModeResponses
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	c.Request.Header.Set("Content-Type", "application/json")
	body := `{"candidates":[{"content":{"role":"model","parts":[{"text":"hello"}]}}],"usageMetadata":{"cachedContentTokenCount":4,"promptTokensDetails":[{"modality":"IMAGE","tokenCount":4}]}}`

	got, apiErr := (&Adaptor{}).DoResponse(c, &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}, info)
	require.Nil(t, apiErr)
	usage, ok := got.(*dto.Usage)
	require.True(t, ok)
	require.NotNil(t, usage.BillingUsage)
	require.NotNil(t, usage.BillingUsage.GeminiUsageMetadata)
	assert.Equal(t, 4, usage.BillingUsage.GeminiUsageMetadata.CachedContentTokenCount)
	require.NotNil(t, usage.InputTokensDetails)
	assert.Equal(t, 4, usage.InputTokensDetails.CachedTokens)
	assert.Equal(t, 4, usage.InputTokensDetails.ImageTokens)
	assert.NotContains(t, recorder.Body.String(), `"billing_usage"`)
}

func TestAdaptorConvertsGeminiStreamToResponses(t *testing.T) {
	oldStreamingTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 60
	t.Cleanup(func() { constant.StreamingTimeout = oldStreamingTimeout })
	info := advancedCustomTestInfo(&dto.AdvancedCustomConfig{Routes: []dto.AdvancedCustomRoute{{
		IncomingPath: "/v1/responses",
		UpstreamPath: "/v1beta/models/{model}:streamGenerateContent",
		Converter:    dto.AdvancedCustomConverterOpenAIResponsesToGeminiGenerateContent,
	}}})
	info.OriginModelName = "gemini-2.5-flash"
	info.UpstreamModelName = "gemini-2.5-flash"
	info.RelayFormat = types.RelayFormatOpenAIResponses
	info.RelayMode = relayconstant.RelayModeResponses
	info.IsStream = true
	info.DisablePing = true
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	c.Request.Header.Set("Accept", "text/event-stream")
	body := "data: {\"candidates\":[{\"content\":{\"role\":\"model\",\"parts\":[{\"text\":\"hello\"}]}}]}\n\n"
	response := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}

	usage, apiErr := (&Adaptor{}).DoResponse(c, response, info)
	require.Nil(t, apiErr)
	require.NotNil(t, usage)
	require.Contains(t, recorder.Body.String(), "response.output_text")
	require.Contains(t, recorder.Body.String(), "[DONE]")
}

func TestAdaptorGeminiStreamUsesCanonicalUsageWithoutTotalTokens(t *testing.T) {
	oldStreamingTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 60
	t.Cleanup(func() { constant.StreamingTimeout = oldStreamingTimeout })
	info := advancedCustomTestInfo(&dto.AdvancedCustomConfig{Routes: []dto.AdvancedCustomRoute{{
		IncomingPath: "/v1/responses",
		UpstreamPath: "/v1beta/models/{model}:streamGenerateContent",
		Converter:    dto.AdvancedCustomConverterOpenAIResponsesToGeminiGenerateContent,
	}}})
	info.OriginModelName = "gemini-2.5-flash"
	info.UpstreamModelName = "gemini-2.5-flash"
	info.RelayFormat = types.RelayFormatOpenAIResponses
	info.RelayMode = relayconstant.RelayModeResponses
	info.IsStream = true
	info.DisablePing = true
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	c.Request.Header.Set("Accept", "text/event-stream")
	body := "data: {\"candidates\":[{\"content\":{\"role\":\"model\",\"parts\":[{\"text\":\"hello\"}]}}],\"usageMetadata\":{\"promptTokenCount\":7,\"toolUsePromptTokenCount\":3,\"candidatesTokenCount\":2,\"cachedContentTokenCount\":4,\"promptTokensDetails\":[{\"modality\":\"IMAGE\",\"tokenCount\":7}],\"toolUsePromptTokensDetails\":[{\"modality\":\"AUDIO\",\"tokenCount\":3}],\"candidatesTokensDetails\":[{\"modality\":\"TEXT\",\"tokenCount\":2}]}}\n\ndata: [DONE]\n\n"
	response := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}

	got, apiErr := (&Adaptor{}).DoResponse(c, response, info)
	require.Nil(t, apiErr)
	usage, ok := got.(*dto.Usage)
	require.True(t, ok)
	assert.Equal(t, 10, usage.PromptTokens)
	assert.Equal(t, 2, usage.CompletionTokens)
	assert.Equal(t, 4, usage.PromptTokensDetails.CachedTokens)
	assert.Equal(t, 7, usage.PromptTokensDetails.ImageTokens)
	assert.Equal(t, 3, usage.PromptTokensDetails.AudioTokens)
	assert.Equal(t, 2, usage.CompletionTokenDetails.TextTokens)
	require.NotNil(t, usage.BillingUsage)
	require.NotNil(t, usage.BillingUsage.GeminiUsageMetadata)
	assert.NotContains(t, recorder.Body.String(), `"billing_usage"`)
}

func TestAdaptorConvertsGeminiRequestToOpenAIChatUpstream(t *testing.T) {
	adaptor := &Adaptor{}
	info := advancedCustomTestInfo(&dto.AdvancedCustomConfig{
		Routes: []dto.AdvancedCustomRoute{
			{
				IncomingPath: "/v1beta/models/{model}:generateContent",
				UpstreamPath: "/v1/chat/completions",
				Converter:    dto.AdvancedCustomConverterGeminiGenerateContentToOpenAIChatCompletions,
			},
		},
	})
	info.RelayFormat = types.RelayFormatGemini
	info.RequestURLPath = "/v1beta/models/gemini-2.5-flash:generateContent"
	info.UpstreamModelName = "gpt-test"
	c := advancedCustomTestContext("/v1beta/models/gemini-2.5-flash:generateContent")

	converted, err := adaptor.ConvertGeminiRequest(c, info, &dto.GeminiChatRequest{
		Contents: []dto.GeminiChatContent{
			{
				Role: "user",
				Parts: []dto.GeminiPart{
					{Text: "hello"},
				},
			},
		},
	})
	require.NoError(t, err)

	chatReq, ok := converted.(*dto.GeneralOpenAIRequest)
	require.True(t, ok)
	assert.Equal(t, "gpt-test", chatReq.Model)
	require.Len(t, chatReq.Messages, 1)
	assert.Equal(t, "user", chatReq.Messages[0].Role)
}

func TestAdaptorCrossProtocolChatUpstreamRequestsStreamUsage(t *testing.T) {
	claudeReq := &dto.ClaudeRequest{
		Model:    "gpt-test",
		Messages: []dto.ClaudeMessage{{Role: "user", Content: "hello"}},
	}
	geminiReq := &dto.GeminiChatRequest{
		Contents: []dto.GeminiChatContent{{Role: "user", Parts: []dto.GeminiPart{{Text: "hello"}}}},
	}
	responsesReq := dto.OpenAIResponsesRequest{
		Model: "gpt-test",
		Input: []byte(`"hello"`),
	}

	tests := []struct {
		name         string
		route        dto.AdvancedCustomRoute
		relayFormat  types.RelayFormat
		relayMode    int
		requestPath  string
		convert      func(*Adaptor, *gin.Context, *relaycommon.RelayInfo) (any, error)
		isStream     bool
		supportUsage bool
		wantUsage    bool
	}{
		{
			name: "claude stream requests usage",
			route: dto.AdvancedCustomRoute{
				IncomingPath: "/v1/messages",
				UpstreamPath: "/v1/chat/completions",
				Converter:    dto.AdvancedCustomConverterAnthropicMessagesToOpenAIChatCompletions,
			},
			relayFormat: types.RelayFormatClaude,
			relayMode:   relayconstant.RelayModeChatCompletions,
			requestPath: "/v1/messages",
			convert: func(a *Adaptor, c *gin.Context, info *relaycommon.RelayInfo) (any, error) {
				return a.ConvertClaudeRequest(c, info, claudeReq)
			},
			isStream:     true,
			supportUsage: true,
			wantUsage:    true,
		},
		{
			name: "gemini stream requests usage",
			route: dto.AdvancedCustomRoute{
				IncomingPath: "/v1beta/models/{model}:generateContent",
				UpstreamPath: "/v1/chat/completions",
				Converter:    dto.AdvancedCustomConverterGeminiGenerateContentToOpenAIChatCompletions,
			},
			relayFormat: types.RelayFormatGemini,
			relayMode:   relayconstant.RelayModeGemini,
			requestPath: "/v1beta/models/gpt-test:generateContent",
			convert: func(a *Adaptor, c *gin.Context, info *relaycommon.RelayInfo) (any, error) {
				return a.ConvertGeminiRequest(c, info, geminiReq)
			},
			isStream:     true,
			supportUsage: true,
			wantUsage:    true,
		},
		{
			name: "responses stream requests usage",
			route: dto.AdvancedCustomRoute{
				IncomingPath: "/v1/responses",
				UpstreamPath: "/v1/chat/completions",
				Converter:    dto.AdvancedCustomConverterOpenAIResponsesToOpenAIChatCompletions,
			},
			relayFormat: types.RelayFormatOpenAIResponses,
			relayMode:   relayconstant.RelayModeResponses,
			requestPath: "/v1/responses",
			convert: func(a *Adaptor, c *gin.Context, info *relaycommon.RelayInfo) (any, error) {
				return a.ConvertOpenAIResponsesRequest(c, info, responsesReq)
			},
			isStream:     true,
			supportUsage: true,
			wantUsage:    true,
		},
		{
			name: "claude non-stream leaves stream options unset",
			route: dto.AdvancedCustomRoute{
				IncomingPath: "/v1/messages",
				UpstreamPath: "/v1/chat/completions",
				Converter:    dto.AdvancedCustomConverterAnthropicMessagesToOpenAIChatCompletions,
			},
			relayFormat: types.RelayFormatClaude,
			relayMode:   relayconstant.RelayModeChatCompletions,
			requestPath: "/v1/messages",
			convert: func(a *Adaptor, c *gin.Context, info *relaycommon.RelayInfo) (any, error) {
				return a.ConvertClaudeRequest(c, info, claudeReq)
			},
			isStream:     false,
			supportUsage: true,
			wantUsage:    false,
		},
		{
			name: "responses non-stream leaves stream options unset",
			route: dto.AdvancedCustomRoute{
				IncomingPath: "/v1/responses", UpstreamPath: "/v1/chat/completions",
				Converter: dto.AdvancedCustomConverterOpenAIResponsesToOpenAIChatCompletions,
			},
			relayFormat: types.RelayFormatOpenAIResponses, relayMode: relayconstant.RelayModeResponses,
			requestPath: "/v1/responses",
			convert: func(a *Adaptor, c *gin.Context, info *relaycommon.RelayInfo) (any, error) {
				return a.ConvertOpenAIResponsesRequest(c, info, responsesReq)
			},
			supportUsage: true,
		},
		{
			name: "responses stream without stream options support",
			route: dto.AdvancedCustomRoute{
				IncomingPath: "/v1/responses", UpstreamPath: "/v1/chat/completions",
				Converter: dto.AdvancedCustomConverterOpenAIResponsesToOpenAIChatCompletions,
			},
			relayFormat: types.RelayFormatOpenAIResponses, relayMode: relayconstant.RelayModeResponses,
			requestPath: "/v1/responses",
			convert: func(a *Adaptor, c *gin.Context, info *relaycommon.RelayInfo) (any, error) {
				return a.ConvertOpenAIResponsesRequest(c, info, responsesReq)
			},
			isStream: true,
		},
		{
			name: "claude stream without stream options support",
			route: dto.AdvancedCustomRoute{
				IncomingPath: "/v1/messages",
				UpstreamPath: "/v1/chat/completions",
				Converter:    dto.AdvancedCustomConverterAnthropicMessagesToOpenAIChatCompletions,
			},
			relayFormat: types.RelayFormatClaude,
			relayMode:   relayconstant.RelayModeChatCompletions,
			requestPath: "/v1/messages",
			convert: func(a *Adaptor, c *gin.Context, info *relaycommon.RelayInfo) (any, error) {
				return a.ConvertClaudeRequest(c, info, claudeReq)
			},
			isStream:     true,
			supportUsage: false,
			wantUsage:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			adaptor := &Adaptor{}
			info := advancedCustomTestInfo(&dto.AdvancedCustomConfig{Routes: []dto.AdvancedCustomRoute{tt.route}})
			info.RelayFormat = tt.relayFormat
			info.RelayMode = tt.relayMode
			info.RequestURLPath = tt.requestPath
			info.IsStream = tt.isStream
			info.SupportStreamOptions = tt.supportUsage
			c := advancedCustomTestContext(tt.requestPath)

			converted, err := tt.convert(adaptor, c, info)
			require.NoError(t, err)
			chatReq, ok := converted.(*dto.GeneralOpenAIRequest)
			require.True(t, ok)

			if !tt.wantUsage {
				assert.Nil(t, chatReq.StreamOptions)
				return
			}
			require.NotNil(t, chatReq.StreamOptions)
			assert.True(t, chatReq.StreamOptions.IncludeUsage)
		})
	}
}

func advancedCustomTestInfo(config *dto.AdvancedCustomConfig) *relaycommon.RelayInfo {
	return &relaycommon.RelayInfo{
		RelayFormat:     types.RelayFormatOpenAI,
		RelayMode:       relayconstant.RelayModeChatCompletions,
		RequestURLPath:  "/v1/chat/completions",
		OriginModelName: "gpt-test",
		ChannelMeta: &relaycommon.ChannelMeta{
			ApiKey:            "sk-test",
			ChannelBaseUrl:    "https://gateway.example",
			ChannelType:       constant.ChannelTypeCustom,
			UpstreamModelName: "gpt-test",
			ChannelOtherSettings: dto.ChannelOtherSettings{
				AdvancedCustom: config,
			},
		},
	}
}

func advancedCustomTestContext(path string) *gin.Context {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, path, nil)
	c.Request.Header.Set("Content-Type", "application/json")
	return c
}
