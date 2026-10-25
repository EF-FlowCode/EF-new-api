package dto

import relaydto "eflow.local/new-api/relaykit/dto"

type ResponseFormat = relaydto.ResponseFormat
type FormatJsonSchema = relaydto.FormatJsonSchema
type GeneralOpenAIRequest = relaydto.GeneralOpenAIRequest
type ToolCallRequest = relaydto.ToolCallRequest
type FunctionRequest = relaydto.FunctionRequest
type StreamOptions = relaydto.StreamOptions
type Message = relaydto.Message
type MediaContent = relaydto.MediaContent
type MessageImageUrl = relaydto.MessageImageUrl
type MessageInputAudio = relaydto.MessageInputAudio
type MessageFile = relaydto.MessageFile
type MessageVideoUrl = relaydto.MessageVideoUrl
type WebSearchOptions = relaydto.WebSearchOptions
type OpenAIResponsesRequest = relaydto.OpenAIResponsesRequest
type Reasoning = relaydto.Reasoning
type Input = relaydto.Input
type MediaInput = relaydto.MediaInput

const (
	ContentTypeText       = relaydto.ContentTypeText
	ContentTypeImageURL   = relaydto.ContentTypeImageURL
	ContentTypeInputAudio = relaydto.ContentTypeInputAudio
	ContentTypeFile       = relaydto.ContentTypeFile
	ContentTypeVideoUrl   = relaydto.ContentTypeVideoUrl
)

func IsOpenAIReasoningOModel(modelName string) bool {
	return relaydto.IsOpenAIReasoningOModel(modelName)
}

func IsOpenAIGPT5Model(modelName string) bool {
	return relaydto.IsOpenAIGPT5Model(modelName)
}

func IsQwenThinkingBudgetModel(modelName string) bool {
	return relaydto.IsQwenThinkingBudgetModel(modelName)
}

type OpenAIChatCapabilities = relaydto.OpenAIChatCapabilities

func GetOpenAIChatCapabilities(modelName, effort string) OpenAIChatCapabilities {
	return relaydto.GetOpenAIChatCapabilities(modelName, effort)
}
