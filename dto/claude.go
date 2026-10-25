package dto

import relaydto "eflow.local/new-api/relaykit/dto"

type ClaudeMetadata = relaydto.ClaudeMetadata
type ClaudeMediaMessage = relaydto.ClaudeMediaMessage
type ClaudeMessageSource = relaydto.ClaudeMessageSource
type ClaudeMessage = relaydto.ClaudeMessage
type Tool = relaydto.Tool
type InputSchema = relaydto.InputSchema
type ClaudeWebSearchTool = relaydto.ClaudeWebSearchTool
type ClaudeWebSearchUserLocation = relaydto.ClaudeWebSearchUserLocation
type ClaudeToolChoice = relaydto.ClaudeToolChoice
type ClaudeRequest = relaydto.ClaudeRequest
type OutputConfigForEffort = relaydto.OutputConfigForEffort
type Thinking = relaydto.Thinking
type ClaudeErrorWithStatusCode = relaydto.ClaudeErrorWithStatusCode
type ClaudeResponse = relaydto.ClaudeResponse
type ClaudeUsage = relaydto.ClaudeUsage
type ClaudeCacheCreationUsage = relaydto.ClaudeCacheCreationUsage
type ClaudeServerToolUse = relaydto.ClaudeServerToolUse

func ProcessTools(tools []any) ([]*Tool, []*ClaudeWebSearchTool) {
	return relaydto.ProcessTools(tools)
}
