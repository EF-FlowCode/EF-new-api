package service

import (
	"eflow.local/new-api/dto"
	"eflow.local/new-api/pkg/knowledge"
	relaycommon "eflow.local/new-api/relay/common"
	"github.com/gin-gonic/gin"
)

// KnowledgeUsage runs before a compatibility handler overwrites raw usage.
func KnowledgeUsage(c *gin.Context, info *relaycommon.RelayInfo, body []byte, aliLegacyTextInput bool) (*dto.Usage, error) {
	u, err := knowledge.ParseUsage(body, info.GetEstimatePromptTokens(), aliLegacyTextInput)
	if err != nil {
		return nil, err
	}
	usage := &dto.Usage{PromptTokens: u.Input, CompletionTokens: u.Output, TotalTokens: u.Input + u.Output}
	usage.PromptTokensDetails.ImageTokens = u.Image
	usage.PromptTokensDetails.TextTokens = u.Input - u.Image
	c.Set("knowledge_usage_source", "upstream")
	if u.Estimated {
		c.Set("knowledge_usage_source", "estimated")
	}
	c.Set("knowledge_image_usage_known", u.ImageKnown)
	return usage, nil
}
