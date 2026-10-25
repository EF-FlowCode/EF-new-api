package dto

import relaykitdto "eflow.local/new-api/relaykit/dto"

// Advanced Custom channel settings live in relaykit because the route
// matcher is shared with request conversion. These aliases keep the main
// application DTO package as the persistence/API boundary without duplicating
// the validation implementation.
type AdvancedCustomConfig = relaykitdto.AdvancedCustomConfig
type AdvancedCustomRoute = relaykitdto.AdvancedCustomRoute
type AdvancedCustomRouteAuth = relaykitdto.AdvancedCustomRouteAuth

const (
	AdvancedCustomAuthTypeNone   = relaykitdto.AdvancedCustomAuthTypeNone
	AdvancedCustomAuthTypeHeader = relaykitdto.AdvancedCustomAuthTypeHeader
	AdvancedCustomAuthTypeQuery  = relaykitdto.AdvancedCustomAuthTypeQuery

	AdvancedCustomConverterNone                                         = "none"
	AdvancedCustomConverterAnthropicMessagesToOpenAIChatCompletions     = "anthropic_messages_to_openai_chat_completions"
	AdvancedCustomConverterOpenAIChatCompletionsToAnthropicMessages     = "openai_chat_completions_to_anthropic_messages"
	AdvancedCustomConverterOpenAIChatCompletionsToOpenAIResponses       = "openai_chat_completions_to_openai_responses"
	AdvancedCustomConverterOpenAIResponsesToOpenAIChatCompletions       = "openai_responses_to_openai_chat_completions"
	AdvancedCustomConverterOpenAIResponsesToGeminiGenerateContent       = "openai_responses_to_gemini_generate_content"
	AdvancedCustomConverterGeminiGenerateContentToOpenAIChatCompletions = "gemini_generate_content_to_openai_chat_completions"
	AdvancedCustomConverterOpenAIChatCompletionsToGeminiGenerateContent = "openai_chat_completions_to_gemini_generate_content"

	AdvancedCustomModelListPath = relaykitdto.AdvancedCustomModelListPath
	AdvancedCustomBalancePath   = relaykitdto.AdvancedCustomBalancePath
)
