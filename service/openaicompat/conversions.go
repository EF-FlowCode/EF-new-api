package openaicompat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	relaydto "github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert"
	relaytypes "github.com/QuantumNous/new-api/relaykit/types"
)

func ChatCompletionsRequestToResponsesRequest(req *dto.GeneralOpenAIRequest) (*dto.OpenAIResponsesRequest, error) {
	if req == nil {
		return nil, errors.New("request is nil")
	}
	relayReq, err := bridgeDTO[relaydto.GeneralOpenAIRequest](req)
	if err != nil {
		return nil, err
	}
	result, err := relayconvert.ConvertRequest(context.Background(), nil, relaytypes.RelayFormatOpenAIResponses, relayReq)
	if err != nil {
		return nil, err
	}
	converted, ok := result.Value.(*relaydto.OpenAIResponsesRequest)
	if !ok {
		return nil, fmt.Errorf("expected OpenAI responses request, got %T", result.Value)
	}
	return bridgeDTO[dto.OpenAIResponsesRequest](converted)
}

func ResponsesRequestToChatCompletionsRequest(req *dto.OpenAIResponsesRequest) (*dto.GeneralOpenAIRequest, error) {
	if req == nil {
		return nil, errors.New("request is nil")
	}
	relayReq, err := bridgeDTO[relaydto.OpenAIResponsesRequest](req)
	if err != nil {
		return nil, err
	}
	result, err := relayconvert.ConvertRequest(context.Background(), nil, relaytypes.RelayFormatOpenAI, relayReq)
	if err != nil {
		return nil, err
	}
	converted, ok := result.Value.(*relaydto.GeneralOpenAIRequest)
	if !ok {
		return nil, fmt.Errorf("expected OpenAI chat request, got %T", result.Value)
	}
	chatReq, err := bridgeDTO[dto.GeneralOpenAIRequest](converted)
	if err != nil {
		return nil, err
	}

	// DashScope chat/completions rejects these Responses-only fields. This
	// bridge is currently used only by the Ali Responses fallback path.
	chatReq.TopLogProbs = nil
	chatReq.ServiceTier = nil
	chatReq.SafetyIdentifier = nil
	chatReq.PromptCacheKey = ""
	chatReq.PromptCacheRetention = nil
	if chatReq.MaxCompletionTokens != nil {
		chatReq.MaxTokens = chatReq.MaxCompletionTokens
		chatReq.MaxCompletionTokens = nil
	}
	return chatReq, nil
}

func ChatCompletionsResponseToResponsesResponse(resp *dto.OpenAITextResponse, id string, model string) ([]byte, *dto.Usage, error) {
	if resp == nil {
		return nil, nil, errors.New("response is nil")
	}
	relayResp, err := bridgeDTO[relaydto.OpenAITextResponse](resp)
	if err != nil {
		return nil, nil, err
	}
	relayResp.Id = id
	relayResp.Model = model
	result, err := relayconvert.ConvertResponse(context.Background(), nil, relaytypes.RelayFormatOpenAIResponses, relayResp)
	if err != nil {
		return nil, nil, err
	}
	converted, ok := result.Value.(*relaydto.OpenAIResponsesResponse)
	if !ok {
		return nil, nil, fmt.Errorf("expected OpenAI responses response, got %T", result.Value)
	}
	usage, err := bridgeDTO[dto.Usage](result.Usage)
	if err != nil {
		return nil, nil, err
	}
	if usage.InputTokensDetails != nil {
		usage.PromptTokensDetails = *usage.InputTokensDetails
	}

	body := map[string]any{
		"id":         id,
		"object":     "response",
		"created_at": time.Now().Unix(),
		"status":     responseStatus(converted.Status),
		"model":      model,
		"output":     responseOutput(converted.Output),
		"usage":      usage,
	}
	if converted.IncompleteDetails != nil {
		body["incomplete_details"] = converted.IncompleteDetails
	}
	bodyBytes, err := common.Marshal(body)
	if err != nil {
		return nil, nil, err
	}
	return bodyBytes, usage, nil
}

func ResponsesResponseToChatCompletionsResponse(resp *dto.OpenAIResponsesResponse, id string) (*dto.OpenAITextResponse, *dto.Usage, error) {
	if resp == nil {
		return nil, nil, errors.New("response is nil")
	}
	relayResp, err := bridgeDTO[relaydto.OpenAIResponsesResponse](resp)
	if err != nil {
		return nil, nil, err
	}
	relayResp.ID = id
	result, err := relayconvert.ConvertResponse(context.Background(), nil, relaytypes.RelayFormatOpenAI, relayResp)
	if err != nil {
		return nil, nil, err
	}
	converted, ok := result.Value.(*relaydto.OpenAITextResponse)
	if !ok {
		return nil, nil, fmt.Errorf("expected OpenAI chat response, got %T", result.Value)
	}
	chatResp, err := bridgeDTO[dto.OpenAITextResponse](converted)
	if err != nil {
		return nil, nil, err
	}
	usage, err := bridgeDTO[dto.Usage](result.Usage)
	if err != nil {
		return nil, nil, err
	}
	chatResp.Usage = *usage
	return chatResp, usage, nil
}

func ExtractOutputTextFromResponses(resp *dto.OpenAIResponsesResponse) string {
	if resp == nil {
		return ""
	}
	relayResp, err := bridgeDTO[relaydto.OpenAIResponsesResponse](resp)
	if err != nil {
		return ""
	}
	return relayconvert.ExtractOutputTextFromResponses(relayResp)
}

// BuildResponsesUsage maps chat usage onto the Responses usage shape used by
// the existing streaming bridge.
func BuildResponsesUsage(u *dto.Usage) *dto.Usage {
	if u == nil {
		return &dto.Usage{}
	}
	out := &dto.Usage{
		PromptTokens:           u.PromptTokens,
		CompletionTokens:       u.CompletionTokens,
		TotalTokens:            u.TotalTokens,
		InputTokens:            u.PromptTokens,
		OutputTokens:           u.CompletionTokens,
		PromptTokensDetails:    u.PromptTokensDetails,
		CompletionTokenDetails: u.CompletionTokenDetails,
	}
	if u.PromptTokensDetails.CachedTokens > 0 ||
		u.PromptTokensDetails.CachedCreationTokens > 0 ||
		u.PromptTokensDetails.CacheWriteTokens > 0 ||
		u.PromptTokensDetails.TextTokens > 0 ||
		u.PromptTokensDetails.AudioTokens > 0 ||
		u.PromptTokensDetails.ImageTokens > 0 ||
		u.PromptTokensDetails.VideoTokens > 0 {
		details := u.PromptTokensDetails
		out.InputTokensDetails = &details
	}
	if out.TotalTokens == 0 {
		out.TotalTokens = out.PromptTokens + out.CompletionTokens
	}
	return out
}

func bridgeDTO[T any](value any) (*T, error) {
	data, err := common.Marshal(value)
	if err != nil {
		return nil, err
	}
	var out T
	if err := common.Unmarshal(data, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func responseStatus(raw json.RawMessage) string {
	var status string
	if err := common.Unmarshal(raw, &status); err != nil || status == "" {
		return "completed"
	}
	return status
}

func responseOutput(outputs []relaydto.ResponsesOutput) []map[string]any {
	result := make([]map[string]any, 0, len(outputs))
	for _, output := range outputs {
		if output.Type == "reasoning" {
			result = append(result, responseOutputItem(output))
		}
	}
	for _, output := range outputs {
		if output.Type != "reasoning" {
			result = append(result, responseOutputItem(output))
		}
	}
	return result
}

func responseOutputItem(output relaydto.ResponsesOutput) map[string]any {
	item := map[string]any{
		"type":   output.Type,
		"id":     output.ID,
		"status": output.Status,
	}
	switch output.Type {
	case "reasoning":
		item["summary"] = output.Content
	case "message":
		item["role"] = output.Role
		item["content"] = output.Content
	case "function_call":
		item["call_id"] = output.CallId
		item["name"] = output.Name
		item["arguments"] = output.Arguments
	default:
		item["content"] = output.Content
	}
	return item
}
