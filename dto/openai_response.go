package dto

import (
	"encoding/json"

	relaydto "eflow.local/new-api/relaykit/dto"
	relaytypes "eflow.local/new-api/relaykit/types"
)

type SimpleResponse = relaydto.SimpleResponse
type TextResponse = relaydto.TextResponse
type OpenAITextResponseChoice = relaydto.OpenAITextResponseChoice
type OpenAITextResponse = relaydto.OpenAITextResponse
type OpenAIEmbeddingResponseItem = relaydto.OpenAIEmbeddingResponseItem
type OpenAIEmbeddingResponse = relaydto.OpenAIEmbeddingResponse
type FlexibleEmbeddingResponseItem = relaydto.FlexibleEmbeddingResponseItem
type FlexibleEmbeddingResponse = relaydto.FlexibleEmbeddingResponse
type ChatCompletionsStreamResponseChoice = relaydto.ChatCompletionsStreamResponseChoice
type ChatCompletionsStreamResponseChoiceDelta = relaydto.ChatCompletionsStreamResponseChoiceDelta
type ToolCallResponse = relaydto.ToolCallResponse
type FunctionResponse = relaydto.FunctionResponse
type ChatCompletionsStreamResponse = relaydto.ChatCompletionsStreamResponse
type ChatCompletionsStreamResponseSimple = relaydto.ChatCompletionsStreamResponseSimple
type CompletionsStreamResponse = relaydto.CompletionsStreamResponse
type Usage = relaydto.Usage
type OpenAIVideoResponse = relaydto.OpenAIVideoResponse
type InputTokenDetails = relaydto.InputTokenDetails
type OutputTokenDetails = relaydto.OutputTokenDetails
type OpenAIResponsesResponse = relaydto.OpenAIResponsesResponse
type IncompleteDetails = relaydto.IncompleteDetails
type ResponsesOutput = relaydto.ResponsesOutput
type ResponsesOutputContent = relaydto.ResponsesOutputContent
type ResponsesReasoningSummaryPart = relaydto.ResponsesReasoningSummaryPart
type ResponsesStreamResponse = relaydto.ResponsesStreamResponse

const (
	ResponsesOutputTypeImageGenerationCall = relaydto.ResponsesOutputTypeImageGenerationCall
	BuildInToolWebSearchPreview            = relaydto.BuildInToolWebSearchPreview
	BuildInToolWebSearch                   = relaydto.BuildInToolWebSearch
	BuildInToolFileSearch                  = relaydto.BuildInToolFileSearch
	BuildInToolGoogleSearch                = relaydto.BuildInToolGoogleSearch
	BuildInToolImageGeneration             = relaydto.BuildInToolImageGeneration
	BuildInCallWebSearchCall               = relaydto.BuildInCallWebSearchCall
	BuildInCallFileSearchCall              = relaydto.BuildInCallFileSearchCall
	BuildInCallFunctionCall                = relaydto.BuildInCallFunctionCall
	BuildInCallToolUse                     = relaydto.BuildInCallToolUse
	ResponsesOutputTypeItemAdded           = relaydto.ResponsesOutputTypeItemAdded
	ResponsesOutputTypeItemDone            = relaydto.ResponsesOutputTypeItemDone
)

func ResponsesArgumentsString(arguments json.RawMessage) string {
	return relaydto.ResponsesArgumentsString(arguments)
}

func GetOpenAIError(errorField any) *relaytypes.OpenAIError {
	return relaydto.GetOpenAIError(errorField)
}
