package gemini

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"eflow.local/new-api/common"
	"eflow.local/new-api/constant"
	"eflow.local/new-api/dto"
	"eflow.local/new-api/logger"
	relaycommon "eflow.local/new-api/relay/common"
	"eflow.local/new-api/relay/helper"
	relaydto "eflow.local/new-api/relaykit/dto"
	"eflow.local/new-api/service"
	"eflow.local/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

// ThinkingAdaptor uses the same official renderer as cross-protocol requests.
func ThinkingAdaptor(request *dto.GeminiChatRequest, info *relaycommon.RelayInfo, oaiRequest ...dto.GeneralOpenAIRequest) error {
	return service.ApplyGeminiThinking(request, info, oaiRequest...)
}

func geminiStreamHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response, callback func(data string, geminiResponse *dto.GeminiChatResponse) bool) (*dto.Usage, *types.NewAPIError) {
	var usage = &dto.Usage{}
	hasUsageMetadata := false
	var imageCount int
	responseText := strings.Builder{}

	helper.StreamScannerHandler(c, resp, info, func(data string, sr *helper.StreamResult) {
		var geminiResponse dto.GeminiChatResponse
		if err := common.UnmarshalJsonStr(data, &geminiResponse); err != nil {
			sr.Stop(fmt.Errorf("unmarshal: %w", err))
			return
		}
		info.ObserveResponseModel(gjson.Get(data, "modelVersion").Str)

		if len(geminiResponse.Candidates) == 0 && geminiResponse.PromptFeedback != nil && geminiResponse.PromptFeedback.BlockReason != nil {
			common.SetContextKey(c, constant.ContextKeyAdminRejectReason, fmt.Sprintf("gemini_block_reason=%s", *geminiResponse.PromptFeedback.BlockReason))
		}

		// 统计图片数量
		for _, candidate := range geminiResponse.Candidates {
			for _, part := range candidate.Content.Parts {
				if part.InlineData != nil && part.InlineData.MimeType != "" {
					imageCount++
				}
				if part.Text != "" {
					responseText.WriteString(part.Text)
				}
			}
		}

		// 更新使用量统计
		if metadata := geminiResponse.GetUsageMetadata(); metadata != nil {
			mappedUsage, err := service.UsageFromGeminiMetadata(metadata, info.GetEstimatePromptTokens())
			if err != nil {
				sr.Stop(fmt.Errorf("map usage: %w", err))
				return
			}
			if mappedUsage != nil {
				*usage = *mappedUsage
			}
			hasUsageMetadata = true
		}

		if !callback(data, &geminiResponse) {
			sr.Stop(fmt.Errorf("gemini callback stopped"))
		}
	})

	if !hasUsageMetadata && imageCount != 0 {
		if usage.CompletionTokens == 0 {
			usage.CompletionTokens = imageCount * 1400
		}
	}

	if !hasUsageMetadata && usage.CompletionTokens <= 0 {
		if info.ReceivedResponseCount > 0 {
			usage = service.ResponseText2Usage(c, responseText.String(), info.UpstreamModelName, info.GetEstimatePromptTokens())
		} else {
			usage = &dto.Usage{}
		}
	}

	return usage, nil
}

func GeminiChatStreamHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	id := helper.GetResponseID(c)
	created := common.GetTimestamp()
	responseStream, err := service.NewRelayResponseStream(info, types.RelayFormatGemini, info.RelayFormat, service.RelayResponseStreamOptions{
		ID:           id,
		Model:        info.UpstreamModelName,
		Created:      created,
		IncludeUsage: info.ShouldIncludeUsage,
	})
	if err != nil {
		return nil, types.NewError(err, types.ErrorCodeBadResponseBody)
	}

	var conversionErr error
	usage, relayErr := geminiStreamHandler(c, info, resp, func(data string, geminiResponse *dto.GeminiChatResponse) bool {
		switch info.RelayFormat {
		case types.RelayFormatOpenAI:
			var responses []*dto.ChatCompletionsStreamResponse
			responses, conversionErr = service.ConvertRelayStreamChunk[relaydto.GeminiChatResponse, dto.ChatCompletionsStreamResponse](c, info, responseStream, geminiResponse)
			for _, response := range responses {
				response.Usage = nil
				if conversionErr = helper.ObjectData(c, response); conversionErr != nil {
					break
				}
			}
		case types.RelayFormatClaude:
			var responses []*dto.ClaudeResponse
			responses, conversionErr = service.ConvertRelayStreamChunk[relaydto.GeminiChatResponse, dto.ClaudeResponse](c, info, responseStream, geminiResponse)
			for _, response := range responses {
				if conversionErr = helper.ClaudeData(c, *response); conversionErr != nil {
					break
				}
			}
		case types.RelayFormatGemini:
			c.Render(-1, common.CustomEvent{Data: "data: " + data})
			conversionErr = helper.FlushWriter(c)
		default:
			conversionErr = fmt.Errorf("unsupported Gemini stream target: %s", info.RelayFormat)
		}
		return conversionErr == nil
	})
	if relayErr != nil {
		return usage, relayErr
	}
	if conversionErr != nil {
		return usage, types.NewError(conversionErr, types.ErrorCodeBadResponse)
	}
	if err = service.SetRelayResponseStreamUsage(responseStream, usage); err != nil {
		return usage, types.NewError(err, types.ErrorCodeBadResponseBody)
	}

	switch info.RelayFormat {
	case types.RelayFormatOpenAI:
		finalResponses, finalErr := service.FinalizeRelayResponseStream[dto.ChatCompletionsStreamResponse](c, info, responseStream)
		if finalErr != nil {
			return usage, types.NewError(finalErr, types.ErrorCodeBadResponseBody)
		}
		for _, response := range finalResponses {
			response.Usage = nil
			if finalErr = helper.ObjectData(c, response); finalErr != nil {
				return usage, types.NewError(finalErr, types.ErrorCodeBadResponse)
			}
		}
		if info.ShouldIncludeUsage && usage != nil {
			if finalErr = helper.ObjectData(c, helper.GenerateFinalUsageResponse(id, created, info.UpstreamModelName, *usage)); finalErr != nil {
				return usage, types.NewError(finalErr, types.ErrorCodeBadResponse)
			}
		}
		helper.Done(c)
	case types.RelayFormatClaude:
		finalResponses, finalErr := service.FinalizeRelayResponseStream[dto.ClaudeResponse](c, info, responseStream)
		if finalErr != nil {
			return usage, types.NewError(finalErr, types.ErrorCodeBadResponseBody)
		}
		for _, response := range finalResponses {
			if finalErr = helper.ClaudeData(c, *response); finalErr != nil {
				return usage, types.NewError(finalErr, types.ErrorCodeBadResponse)
			}
		}
	}

	return usage, nil
}

func GeminiChatHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}
	service.CloseResponseBodyGracefully(resp)
	logger.LogDebug(c, "Gemini response body: %s", responseBody)
	var geminiResponse dto.GeminiChatResponse
	err = common.Unmarshal(responseBody, &geminiResponse)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}
	info.ObserveResponseModel(gjson.GetBytes(responseBody, "modelVersion").Str)
	if len(geminiResponse.Candidates) == 0 {
		usage, usageErr := service.UsageFromGeminiMetadata(geminiResponse.GetUsageMetadata(), info.GetEstimatePromptTokens())
		if usageErr != nil {
			return nil, types.NewError(usageErr, types.ErrorCodeBadResponseBody)
		}

		var newAPIError *types.NewAPIError
		if geminiResponse.PromptFeedback != nil && geminiResponse.PromptFeedback.BlockReason != nil {
			common.SetContextKey(c, constant.ContextKeyAdminRejectReason, fmt.Sprintf("gemini_block_reason=%s", *geminiResponse.PromptFeedback.BlockReason))
			newAPIError = types.NewOpenAIError(
				errors.New("request blocked by Gemini API: "+*geminiResponse.PromptFeedback.BlockReason),
				types.ErrorCodePromptBlocked,
				http.StatusBadRequest,
			)
		} else {
			common.SetContextKey(c, constant.ContextKeyAdminRejectReason, "gemini_empty_candidates")
			newAPIError = types.NewOpenAIError(
				errors.New("empty response from Gemini API"),
				types.ErrorCodeEmptyResponse,
				http.StatusInternalServerError,
			)
		}

		service.ResetStatusCode(newAPIError, c.GetString("status_code_mapping"))

		switch info.RelayFormat {
		case types.RelayFormatClaude:
			c.JSON(newAPIError.StatusCode, gin.H{
				"type":  "error",
				"error": newAPIError.ToClaudeError(),
			})
		default:
			c.JSON(newAPIError.StatusCode, gin.H{
				"error": newAPIError.ToOpenAIError(),
			})
		}
		return usage, nil
	}
	fullTextResponse, convErr := service.ConvertGeminiResponseToOpenAI(c, info, &geminiResponse)
	if convErr != nil {
		return nil, types.NewError(convErr, types.ErrorCodeBadResponseBody)
	}
	fullTextResponse.Model = info.UpstreamModelName
	usage := fullTextResponse.Usage

	switch info.RelayFormat {
	case types.RelayFormatOpenAI:
		responseBody, err = common.Marshal(fullTextResponse)
		if err != nil {
			return nil, types.NewError(err, types.ErrorCodeBadResponseBody)
		}
	case types.RelayFormatClaude:
		claudeResp, convErr := service.ConvertOpenAIResponseToClaude(c, info, fullTextResponse)
		if convErr != nil {
			return nil, types.NewError(convErr, types.ErrorCodeBadResponseBody)
		}
		claudeRespStr, err := common.Marshal(claudeResp)
		if err != nil {
			return nil, types.NewError(err, types.ErrorCodeBadResponseBody)
		}
		responseBody = claudeRespStr
	case types.RelayFormatGemini:
		break
	}

	service.IOCopyBytesGracefully(c, resp, responseBody)

	return &usage, nil
}

func GeminiEmbeddingHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	defer service.CloseResponseBodyGracefully(resp)

	responseBody, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		return nil, types.NewOpenAIError(readErr, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}

	var geminiResponse dto.GeminiBatchEmbeddingResponse
	if jsonErr := common.Unmarshal(responseBody, &geminiResponse); jsonErr != nil {
		return nil, types.NewOpenAIError(jsonErr, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}

	// convert to openai format response
	openAIResponse := dto.OpenAIEmbeddingResponse{
		Object: "list",
		Data:   make([]dto.OpenAIEmbeddingResponseItem, 0, len(geminiResponse.Embeddings)),
		Model:  info.UpstreamModelName,
	}

	for i, embedding := range geminiResponse.Embeddings {
		openAIResponse.Data = append(openAIResponse.Data, dto.OpenAIEmbeddingResponseItem{
			Object:    "embedding",
			Embedding: embedding.Values,
			Index:     i,
		})
	}

	// calculate usage
	// https://ai.google.dev/gemini-api/docs/pricing?hl=zh-cn#text-embedding-004
	// Google has not yet clarified how embedding models will be billed
	// refer to openai billing method to use input tokens billing
	// https://platform.openai.com/docs/guides/embeddings#what-are-embeddings
	usage := service.ResponseText2Usage(c, "", info.UpstreamModelName, info.GetEstimatePromptTokens())
	openAIResponse.Usage = *usage

	jsonResponse, jsonErr := common.Marshal(openAIResponse)
	if jsonErr != nil {
		return nil, types.NewOpenAIError(jsonErr, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}

	service.IOCopyBytesGracefully(c, resp, jsonResponse)
	return usage, nil
}

func GeminiImageHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	responseBody, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		return nil, types.NewOpenAIError(readErr, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}
	_ = resp.Body.Close()

	var geminiResponse dto.GeminiImageResponse
	if jsonErr := common.Unmarshal(responseBody, &geminiResponse); jsonErr != nil {
		return nil, types.NewOpenAIError(jsonErr, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}

	if len(geminiResponse.Predictions) == 0 {
		return nil, types.NewOpenAIError(errors.New("no images generated"), types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}

	// convert to openai format response
	openAIResponse := dto.ImageResponse{
		Created: common.GetTimestamp(),
		Data:    make([]dto.ImageData, 0, len(geminiResponse.Predictions)),
	}

	for _, prediction := range geminiResponse.Predictions {
		if prediction.RaiFilteredReason != "" {
			continue // skip filtered image
		}
		openAIResponse.Data = append(openAIResponse.Data, dto.ImageData{
			B64Json: prediction.BytesBase64Encoded,
		})
	}

	jsonResponse, jsonErr := json.Marshal(openAIResponse)
	if jsonErr != nil {
		return nil, types.NewError(jsonErr, types.ErrorCodeBadResponseBody)
	}

	c.Writer.Header().Set("Content-Type", "application/json")
	c.Writer.WriteHeader(resp.StatusCode)
	_, _ = c.Writer.Write(jsonResponse)

	// https://github.com/google-gemini/cookbook/blob/719a27d752aac33f39de18a8d3cb42a70874917e/quickstarts/Counting_Tokens.ipynb
	// each image has fixed 258 tokens
	const imageTokens = 258
	generatedImages := len(openAIResponse.Data)

	usage := &dto.Usage{
		PromptTokens:     imageTokens * generatedImages, // each generated image has fixed 258 tokens
		CompletionTokens: 0,                             // image generation does not calculate completion tokens
		TotalTokens:      imageTokens * generatedImages,
	}

	return usage, nil
}

type GeminiModelsResponse struct {
	Models        []dto.GeminiModel `json:"models"`
	NextPageToken string            `json:"nextPageToken"`
}

func FetchGeminiModels(baseURL, apiKey, proxyURL string) ([]string, error) {
	client, err := service.GetHttpClientWithProxy(proxyURL)
	if err != nil {
		return nil, fmt.Errorf("创建HTTP客户端失败: %v", err)
	}
	// net/http keeps x-goog-api-key on cross-host redirects; do not follow them.
	client = service.NewRedirectlessHTTPClient(client)

	allModels := make([]string, 0)
	nextPageToken := ""
	maxPages := 100 // Safety limit to prevent infinite loops

	for range maxPages {
		url := fmt.Sprintf("%s/v1beta/models", baseURL)
		if nextPageToken != "" {
			url = fmt.Sprintf("%s?pageToken=%s", url, nextPageToken)
		}

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		request, err := http.NewRequestWithContext(ctx, "GET", url, nil)
		if err != nil {
			cancel()
			return nil, fmt.Errorf("创建请求失败: %v", err)
		}

		request.Header.Set("x-goog-api-key", apiKey)

		response, err := client.Do(request)
		if err != nil {
			cancel()
			return nil, fmt.Errorf("请求失败: %v", err)
		}

		if response.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(response.Body)
			response.Body.Close()
			cancel()
			return nil, fmt.Errorf("服务器返回错误 %d: %s", response.StatusCode, string(body))
		}

		body, err := io.ReadAll(response.Body)
		response.Body.Close()
		cancel()
		if err != nil {
			return nil, fmt.Errorf("读取响应失败: %v", err)
		}

		var modelsResponse GeminiModelsResponse
		if err = common.Unmarshal(body, &modelsResponse); err != nil {
			return nil, fmt.Errorf("解析响应失败: %v", err)
		}

		for _, model := range modelsResponse.Models {
			modelNameValue, ok := model.Name.(string)
			if !ok {
				continue
			}
			modelName := strings.TrimPrefix(modelNameValue, "models/")
			allModels = append(allModels, modelName)
		}

		nextPageToken = modelsResponse.NextPageToken
		if nextPageToken == "" {
			break
		}
	}

	return allModels, nil
}

// convertToolChoiceToGeminiConfig converts OpenAI tool_choice to Gemini toolConfig
// OpenAI tool_choice values:
//   - "auto": Let the model decide (default)
//   - "none": Don't call any tools
//   - "required": Must call at least one tool
//   - {"type": "function", "function": {"name": "xxx"}}: Call specific function
//
// Gemini functionCallingConfig.mode values:
//   - "AUTO": Model decides whether to call functions
//   - "NONE": Model won't call functions
//   - "ANY": Model must call at least one function
func convertToolChoiceToGeminiConfig(toolChoice any) *dto.ToolConfig {
	if toolChoice == nil {
		return nil
	}

	// Handle string values: "auto", "none", "required"
	if toolChoiceStr, ok := toolChoice.(string); ok {
		config := &dto.ToolConfig{
			FunctionCallingConfig: &dto.FunctionCallingConfig{},
		}
		switch toolChoiceStr {
		case "auto":
			config.FunctionCallingConfig.Mode = "AUTO"
		case "none":
			config.FunctionCallingConfig.Mode = "NONE"
		case "required":
			config.FunctionCallingConfig.Mode = "ANY"
		default:
			// Unknown string value, default to AUTO
			config.FunctionCallingConfig.Mode = "AUTO"
		}
		return config
	}

	// Handle object value: {"type": "function", "function": {"name": "xxx"}}
	if toolChoiceMap, ok := toolChoice.(map[string]any); ok {
		if toolChoiceMap["type"] == "function" {
			config := &dto.ToolConfig{
				FunctionCallingConfig: &dto.FunctionCallingConfig{
					Mode: "ANY",
				},
			}
			// Extract function name if specified
			if function, ok := toolChoiceMap["function"].(map[string]any); ok {
				if name, ok := function["name"].(string); ok && name != "" {
					config.FunctionCallingConfig.AllowedFunctionNames = []string{name}
				}
			}
			return config
		}
		// Unsupported map structure (type is not "function"), return nil
		return nil
	}

	// Unsupported type, return nil
	return nil
}
