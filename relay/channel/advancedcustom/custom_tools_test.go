package advancedcustom

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"eflow.local/new-api/constant"
	"eflow.local/new-api/dto"
	relayconstant "eflow.local/new-api/relay/constant"
	"eflow.local/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestAdaptorResponsesCustomToolRoundTrip(t *testing.T) {
	old := constant.StreamingTimeout
	constant.StreamingTimeout = 5
	t.Cleanup(func() { constant.StreamingTimeout = old })
	for _, gemini := range []bool{false, true} {
		for _, stream := range []bool{false, true} {
			name := "chat"
			converter := dto.AdvancedCustomConverterOpenAIResponsesToOpenAIChatCompletions
			path := "/v1/chat/completions"
			if gemini {
				name = "gemini"
				converter = dto.AdvancedCustomConverterOpenAIResponsesToGeminiGenerateContent
				path = "/v1beta/models/{model}:generateContent"
			}
			if stream {
				name += "_stream"
			}
			t.Run(name, func(t *testing.T) {
				info := advancedCustomTestInfo(&dto.AdvancedCustomConfig{Routes: []dto.AdvancedCustomRoute{{IncomingPath: "/v1/responses", UpstreamPath: path, Converter: converter}}})
				info.RelayFormat = types.RelayFormatOpenAIResponses
				info.RelayMode = relayconstant.RelayModeResponses
				info.IsStream = stream
				info.DisablePing = true
				recorder := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(recorder)
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
				req := dto.OpenAIResponsesRequest{Model: "gpt-test", Input: json.RawMessage(`"run"`), Tools: json.RawMessage(`[{"type":"custom","name":"exec"}]`), ToolChoice: json.RawMessage(`{"type":"custom","name":"exec"}`), PromptCacheKey: json.RawMessage(`"keep-cache"`)}
				adaptor := &Adaptor{}
				converted, err := adaptor.ConvertOpenAIResponsesRequest(c, info, req)
				require.NoError(t, err)
				require.True(t, info.ResponsesTools.IsCustomTool("exec"))
				if !gemini {
					require.Equal(t, "keep-cache", converted.(*dto.GeneralOpenAIRequest).PromptCacheKey)
				}
				body := `{"choices":[{"message":{"role":"assistant","tool_calls":[{"id":"c1","type":"function","function":{"name":"exec","arguments":"{\"input\":\"echo hi\"}"}}]},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5}}`
				if stream {
					body = `{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"c1","type":"function","function":{"name":"exec","arguments":"{\"input\":\"echo hi\"}"}}]},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5}}`
				}
				if gemini {
					body = `{"candidates":[{"content":{"role":"model","parts":[{"functionCall":{"name":"exec","args":{"input":"echo hi"}}}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":3,"candidatesTokenCount":2,"totalTokenCount":5}}`
				}
				if stream {
					body = "data: " + body + "\n\ndata: [DONE]\n\n"
				}
				got, apiErr := adaptor.DoResponse(c, &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}, info)
				require.Nil(t, apiErr)
				require.Equal(t, 5, got.(*dto.Usage).TotalTokens)
				var response dto.OpenAIResponsesResponse
				if stream {
					for line := range strings.SplitSeq(recorder.Body.String(), "\n") {
						if !strings.HasPrefix(line, "data: {") {
							continue
						}
						var event dto.ResponsesStreamResponse
						require.NoError(t, json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &event))
						if event.Type == "response.completed" {
							response = *event.Response
						}
					}
				} else {
					require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
				}
				require.Len(t, response.Output, 1)
				require.Equal(t, "custom_tool_call", response.Output[0].Type)
				require.JSONEq(t, `"echo hi"`, string(response.Output[0].Input))
			})
		}
	}
}
