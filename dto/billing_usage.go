package dto

import relaydto "eflow.local/new-api/relaykit/dto"

type BillingUsage = relaydto.BillingUsage

const (
	BillingUsageSourceClaudeMessages = relaydto.BillingUsageSourceClaudeMessages
	BillingUsageSourceGeminiChat     = relaydto.BillingUsageSourceGeminiChat
	BillingUsageSourceOAIChat        = relaydto.BillingUsageSourceOAIChat
	BillingUsageSourceOAIResponses   = relaydto.BillingUsageSourceOAIResponses
	BillingUsageSemanticAnthropic    = relaydto.BillingUsageSemanticAnthropic
	BillingUsageSemanticGemini       = relaydto.BillingUsageSemanticGemini
	BillingUsageSemanticOpenAI       = relaydto.BillingUsageSemanticOpenAI
)

func NewClaudeMessagesBillingUsage(usage *ClaudeUsage) *BillingUsage {
	return relaydto.NewClaudeMessagesBillingUsage(usage)
}

func HasClaudeUsageTokens(usage *ClaudeUsage) bool {
	return relaydto.HasClaudeUsageTokens(usage)
}

func NewOpenAIChatBillingUsage(usage *Usage) *BillingUsage {
	return relaydto.NewOpenAIChatBillingUsage(usage)
}

func NewOpenAIResponsesBillingUsage(usage *Usage) *BillingUsage {
	return relaydto.NewOpenAIResponsesBillingUsage(usage)
}

func HasOpenAIUsageTokens(usage *Usage) bool {
	return relaydto.HasOpenAIUsageTokens(usage)
}

func NewGeminiChatBillingUsage(metadata *GeminiUsageMetadata) *BillingUsage {
	return relaydto.NewGeminiChatBillingUsage(metadata)
}

func NewEstimatedGeminiChatBillingUsage(usage *Usage) *BillingUsage {
	return relaydto.NewEstimatedGeminiChatBillingUsage(usage)
}

func CloneBillingUsage(usage *BillingUsage) *BillingUsage {
	return relaydto.CloneBillingUsage(usage)
}

func HasGeminiUsageMetadataTokens(metadata *GeminiUsageMetadata) bool {
	return relaydto.HasGeminiUsageMetadataTokens(metadata)
}
