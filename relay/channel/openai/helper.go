package openai

import (
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relay/helper"
	relaydto "github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"

	"github.com/samber/lo"

	"github.com/gin-gonic/gin"
)

// 辅助函数
func HandleStreamFormat(c *gin.Context, info *relaycommon.RelayInfo, responseStream *service.RelayResponseStream, data string, forceFormat bool, thinkToContent bool) error {
	switch info.RelayFormat {
	case types.RelayFormatOpenAI:
		info.SendResponseCount++
		return sendStreamData(c, info, data, forceFormat, thinkToContent)
	case types.RelayFormatClaude:
		return handleClaudeFormat(c, data, info, responseStream)
	case types.RelayFormatGemini:
		return handleGeminiFormat(c, data, info, responseStream)
	}
	return nil
}

func handleClaudeFormat(c *gin.Context, data string, info *relaycommon.RelayInfo, responseStream *service.RelayResponseStream) error {
	var streamResponse dto.ChatCompletionsStreamResponse
	if err := common.Unmarshal(common.StringToByteSlice(data), &streamResponse); err != nil {
		return err
	}

	claudeResponses, err := service.ConvertRelayStreamChunk[relaydto.ChatCompletionsStreamResponse, dto.ClaudeResponse](c, info, responseStream, &streamResponse)
	if err != nil {
		return err
	}
	for _, resp := range claudeResponses {
		if err := helper.ClaudeData(c, *resp); err != nil {
			return err
		}
	}
	return nil
}

func handleGeminiFormat(c *gin.Context, data string, info *relaycommon.RelayInfo, responseStream *service.RelayResponseStream) error {
	var streamResponse dto.ChatCompletionsStreamResponse
	if err := common.Unmarshal(common.StringToByteSlice(data), &streamResponse); err != nil {
		logger.LogError(c, "failed to unmarshal stream response: "+err.Error())
		return err
	}

	geminiResponses, err := service.ConvertRelayStreamChunk[relaydto.ChatCompletionsStreamResponse, dto.GeminiChatResponse](c, info, responseStream, &streamResponse)
	if err != nil {
		return err
	}
	for _, geminiResponse := range geminiResponses {
		geminiResponseStr, err := common.Marshal(geminiResponse)
		if err != nil {
			logger.LogError(c, "failed to marshal gemini response: "+err.Error())
			return err
		}
		c.Render(-1, common.CustomEvent{Data: "data: " + string(geminiResponseStr)})
		if err = helper.FlushWriter(c); err != nil {
			return err
		}
	}
	return nil
}

func ProcessStreamResponse(streamResponse dto.ChatCompletionsStreamResponse, responseTextBuilder *strings.Builder, toolCount *int) error {
	for _, choice := range streamResponse.Choices {
		responseTextBuilder.WriteString(choice.Delta.GetContentString())
		responseTextBuilder.WriteString(choice.Delta.GetReasoningContent())
		if choice.Delta.ToolCalls != nil {
			if len(choice.Delta.ToolCalls) > *toolCount {
				*toolCount = len(choice.Delta.ToolCalls)
			}
			for _, tool := range choice.Delta.ToolCalls {
				responseTextBuilder.WriteString(tool.Function.Name)
				responseTextBuilder.WriteString(tool.Function.Arguments)
			}
		}
	}
	return nil
}

func processTokenData(relayMode int, data string, responseTextBuilder *strings.Builder, toolCount *int) error {
	switch relayMode {
	case relayconstant.RelayModeChatCompletions:
		var streamResponse dto.ChatCompletionsStreamResponse
		if err := common.UnmarshalJsonStr(data, &streamResponse); err != nil {
			return err
		}
		return ProcessStreamResponse(streamResponse, responseTextBuilder, toolCount)
	case relayconstant.RelayModeCompletions:
		var streamResponse dto.CompletionsStreamResponse
		if err := common.UnmarshalJsonStr(data, &streamResponse); err != nil {
			return err
		}
		processCompletionsStreamResponse(streamResponse, responseTextBuilder)
	}
	return nil
}

func processCompletionsStreamResponse(streamResponse dto.CompletionsStreamResponse, responseTextBuilder *strings.Builder) {
	for _, choice := range streamResponse.Choices {
		responseTextBuilder.WriteString(choice.Text)
	}
}

func handleLastResponse(lastStreamData string, responseId *string, createAt *int64,
	systemFingerprint *string, model *string, usage **dto.Usage,
	containStreamUsage *bool, info *relaycommon.RelayInfo,
	shouldSendLastResp *bool) error {

	var lastStreamResponse dto.ChatCompletionsStreamResponse
	if err := common.Unmarshal(common.StringToByteSlice(lastStreamData), &lastStreamResponse); err != nil {
		return err
	}

	*responseId = lastStreamResponse.Id
	*createAt = lastStreamResponse.Created
	*systemFingerprint = lastStreamResponse.GetSystemFingerprint()
	*model = lastStreamResponse.Model

	if service.ValidUsage(lastStreamResponse.Usage) {
		*containStreamUsage = true
		*usage = lastStreamResponse.Usage
		if !info.ShouldIncludeUsage {
			*shouldSendLastResp = lo.SomeBy(lastStreamResponse.Choices, func(choice dto.ChatCompletionsStreamResponseChoice) bool {
				return choice.Delta.GetContentString() != "" || choice.Delta.GetReasoningContent() != ""
			})
		}
	}

	return nil
}

func HandleFinalResponse(c *gin.Context, info *relaycommon.RelayInfo, responseStream *service.RelayResponseStream, lastStreamData string,
	responseId string, createAt int64, model string, systemFingerprint string,
	usage *dto.Usage, containStreamUsage bool) {

	switch info.RelayFormat {
	case types.RelayFormatOpenAI:
		if info.ShouldIncludeUsage && !containStreamUsage {
			response := helper.GenerateFinalUsageResponse(responseId, createAt, model, *usage)
			response.SetSystemFingerprint(systemFingerprint)
			helper.ObjectData(c, response)
		}
		helper.Done(c)

	case types.RelayFormatClaude:
		if info.ClaudeConvertInfo == nil {
			info.ClaudeConvertInfo = &relaycommon.ClaudeConvertInfo{LastMessagesType: relaycommon.LastMessageTypeNone}
		}
		info.ClaudeConvertInfo.Usage = usage
		if err := service.SetRelayResponseStreamUsage(responseStream, usage); err != nil {
			common.SysLog("error setting relay stream usage: " + err.Error())
			return
		}
		if err := handleClaudeFormat(c, lastStreamData, info, responseStream); err != nil {
			common.SysLog("error converting final Claude stream response: " + err.Error())
			return
		}
		claudeResponses, err := service.FinalizeRelayResponseStream[dto.ClaudeResponse](c, info, responseStream)
		if err != nil {
			common.SysLog("error finalizing Claude stream response: " + err.Error())
			return
		}
		for _, response := range claudeResponses {
			_ = helper.ClaudeData(c, *response)
		}

	case types.RelayFormatGemini:
		if err := service.SetRelayResponseStreamUsage(responseStream, usage); err != nil {
			common.SysLog("error setting relay stream usage: " + err.Error())
			return
		}
		if err := handleGeminiFormat(c, lastStreamData, info, responseStream); err != nil {
			common.SysLog("error converting final Gemini stream response: " + err.Error())
			return
		}
		geminiResponses, err := service.FinalizeRelayResponseStream[dto.GeminiChatResponse](c, info, responseStream)
		if err != nil {
			common.SysLog("error finalizing Gemini stream response: " + err.Error())
			return
		}
		for _, response := range geminiResponses {
			responseData, marshalErr := common.Marshal(response)
			if marshalErr != nil {
				common.SysLog("error marshalling Gemini stream response: " + marshalErr.Error())
				return
			}
			c.Render(-1, common.CustomEvent{Data: "data: " + string(responseData)})
			_ = helper.FlushWriter(c)
		}
	}
}

func sendResponsesStreamData(c *gin.Context, streamResponse dto.ResponsesStreamResponse, data string) {
	if data == "" {
		return
	}
	_ = helper.ResponseChunkData(c, streamResponse, data)
}
