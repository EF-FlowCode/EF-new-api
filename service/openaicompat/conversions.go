package openaicompat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"eflow.local/new-api/common"
	"eflow.local/new-api/dto"
	relaycommon "eflow.local/new-api/relay/common"
	"eflow.local/new-api/relaykit/relayconvert"
	"eflow.local/new-api/relaykit/relayconvert/convmeta"
	relaytypes "eflow.local/new-api/relaykit/types"
)

func ChatCompletionsRequestToResponsesRequest(req *dto.GeneralOpenAIRequest) (*dto.OpenAIResponsesRequest, error) {
	if req == nil {
		return nil, errors.New("request is nil")
	}
	result, err := relayconvert.ConvertRequest(context.Background(), nil, relaytypes.RelayFormatOpenAIResponses, req)
	if err != nil {
		return nil, err
	}
	converted, ok := result.Value.(*dto.OpenAIResponsesRequest)
	if !ok {
		return nil, fmt.Errorf("expected OpenAI responses request, got %T", result.Value)
	}
	return converted, nil
}

func ResponsesRequestToChatCompletionsRequest(req *dto.OpenAIResponsesRequest) (*dto.GeneralOpenAIRequest, error) {
	return ResponsesRequestToChatCompletionsRequestWithInfo(req, nil)
}
func ResponsesRequestToChatCompletionsRequestWithInfo(req *dto.OpenAIResponsesRequest, info *relaycommon.RelayInfo) (*dto.GeneralOpenAIRequest, error) {
	if req == nil {
		return nil, errors.New("request is nil")
	}
	meta := &convmeta.Values{}
	if info != nil {
		info.ResponsesTools = nil
	}
	result, err := relayconvert.ConvertRequest(context.Background(), meta, relaytypes.RelayFormatOpenAI, req)
	if err == nil && info != nil {
		info.ResponsesTools = meta.ResponsesTools
	}
	if err != nil {
		return nil, err
	}
	chatReq, ok := result.Value.(*dto.GeneralOpenAIRequest)
	if !ok {
		return nil, fmt.Errorf("expected OpenAI chat request, got %T", result.Value)
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
	return ChatCompletionsResponseToResponsesResponseWithTools(resp, id, model, nil)
}
func ChatCompletionsResponseToResponsesResponseWithTools(resp *dto.OpenAITextResponse, id string, model string, tools *convmeta.ResponsesToolState) ([]byte, *dto.Usage, error) {
	if resp == nil {
		return nil, nil, errors.New("response is nil")
	}
	relayResp := *resp
	relayResp.Id = id
	relayResp.Model = model
	result, err := relayconvert.ConvertResponse(context.Background(), &convmeta.Values{ResponsesTools: tools}, relaytypes.RelayFormatOpenAIResponses, &relayResp)
	if err != nil {
		return nil, nil, err
	}
	converted, ok := result.Value.(*dto.OpenAIResponsesResponse)
	if !ok {
		return nil, nil, fmt.Errorf("expected OpenAI responses response, got %T", result.Value)
	}
	usage := result.Usage
	if usage == nil {
		usage = &dto.Usage{}
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
	relayResp := *resp
	relayResp.ID = id
	result, err := relayconvert.ConvertResponse(context.Background(), nil, relaytypes.RelayFormatOpenAI, &relayResp)
	if err != nil {
		return nil, nil, err
	}
	chatResp, ok := result.Value.(*dto.OpenAITextResponse)
	if !ok {
		return nil, nil, fmt.Errorf("expected OpenAI chat response, got %T", result.Value)
	}
	usage := result.Usage
	if usage == nil {
		usage = &dto.Usage{}
	}
	chatResp.Usage = *usage
	return chatResp, usage, nil
}

func ExtractOutputTextFromResponses(resp *dto.OpenAIResponsesResponse) string {
	if resp == nil {
		return ""
	}
	return relayconvert.ExtractOutputTextFromResponses(resp)
}

func responseStatus(raw json.RawMessage) string {
	var status string
	if err := common.Unmarshal(raw, &status); err != nil || status == "" {
		return "completed"
	}
	return status
}

func responseOutput(outputs []dto.ResponsesOutput) []map[string]any {
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

func responseOutputItem(output dto.ResponsesOutput) map[string]any {
	item := map[string]any{
		"type":   output.Type,
		"id":     output.ID,
		"status": output.Status,
	}
	switch output.Type {
	case "reasoning":
		if output.Summary != nil {
			item["summary"] = output.Summary
		} else {
			item["summary"] = output.Content
		}
	case "message":
		item["role"] = output.Role
		item["content"] = output.Content
	case "custom_tool_call":
		item["call_id"] = output.CallId
		item["name"] = output.Name
		item["input"] = output.Input
	case "function_call":
		item["call_id"] = output.CallId
		item["name"] = output.Name
		item["arguments"] = output.Arguments
	default:
		item["content"] = output.Content
	}
	return item
}
