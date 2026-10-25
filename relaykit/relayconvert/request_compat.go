package relayconvert

import (
	"context"
	"eflow.local/new-api/relaykit/dto"
	"eflow.local/new-api/relaykit/relayconvert/convmeta"
	claudemessages "eflow.local/new-api/relaykit/relayconvert/internal/claude_messages"
	geminichat "eflow.local/new-api/relaykit/relayconvert/internal/gemini_chat"
	oaichat "eflow.local/new-api/relaykit/relayconvert/internal/oai_chat"
	sharedgemini "eflow.local/new-api/relaykit/relayconvert/internal/shared/gemini"
	"eflow.local/new-api/relaykit/types"
)

func ClaudeMessagesRequestToOpenAIChat(claudeRequest dto.ClaudeRequest, info convmeta.Meta) (*dto.GeneralOpenAIRequest, error) {
	return claudemessages.ClaudeMessagesRequestToOpenAIChat(claudeRequest, info)
}

func OpenAIChatRequestToClaudeMessages(c context.Context, info convmeta.Meta, textRequest dto.GeneralOpenAIRequest) (*dto.ClaudeRequest, error) {
	return oaichat.OpenAIChatRequestToClaudeMessages(c, info, textRequest)
}

func GeminiGenerateContentRequestToOpenAIChat(geminiRequest *dto.GeminiChatRequest, info convmeta.Meta) (*dto.GeneralOpenAIRequest, error) {
	return geminichat.GeminiGenerateContentRequestToOpenAIChat(geminiRequest, info)
}

func OpenAIChatRequestToGeminiGenerateContent(c context.Context, textRequest dto.GeneralOpenAIRequest, info convmeta.Meta) (*dto.GeminiChatRequest, error) {
	return oaichat.OpenAIChatRequestToGeminiGenerateContent(c, textRequest, info)
}

func ApplyGeminiThinkingConfigChecked(geminiRequest *dto.GeminiChatRequest, info convmeta.Meta, oaiRequest ...dto.GeneralOpenAIRequest) error {
	return sharedgemini.ApplyThinkingConfig(geminiRequest, info, oaiRequest...)
}

func ChatCompletionsRequestToResponsesRequest(req *dto.GeneralOpenAIRequest) (*dto.OpenAIResponsesRequest, error) {
	return oaichat.ChatCompletionsRequestToResponsesRequest(req)
}

func ResponsesRequestToChatCompletionsRequest(req *dto.OpenAIResponsesRequest) (*dto.GeneralOpenAIRequest, error) {
	result, err := ConvertRequest(context.Background(), nil, types.RelayFormatOpenAI, req)
	if err != nil {
		return nil, err
	}
	return result.Value.(*dto.GeneralOpenAIRequest), nil
}

func OpenAIResponsesRequestToClaudeMessages(c context.Context, info convmeta.Meta, req *dto.OpenAIResponsesRequest) (*dto.ClaudeRequest, error) {
	result, err := ConvertRequest(c, info, types.RelayFormatClaude, req)
	if err != nil {
		return nil, err
	}
	return result.Value.(*dto.ClaudeRequest), nil
}

func OpenAIResponsesRequestToGeminiChat(c context.Context, req *dto.OpenAIResponsesRequest, info convmeta.Meta) (*dto.GeminiChatRequest, error) {
	result, err := ConvertRequest(c, info, types.RelayFormatGemini, req)
	if err != nil {
		return nil, err
	}
	return result.Value.(*dto.GeminiChatRequest), nil
}
