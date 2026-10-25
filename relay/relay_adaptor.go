package relay

import (
	"strconv"
	"strings"

	"eflow.local/new-api/constant"
	"eflow.local/new-api/dto"
	"eflow.local/new-api/relay/channel"
	"eflow.local/new-api/relay/channel/advancedcustom"
	"eflow.local/new-api/relay/channel/ali"
	"eflow.local/new-api/relay/channel/ali_asr"
	"eflow.local/new-api/relay/channel/ali_asr_realtime"
	"eflow.local/new-api/relay/channel/ali_tts"
	"eflow.local/new-api/relay/channel/aws"
	"eflow.local/new-api/relay/channel/baidu"
	"eflow.local/new-api/relay/channel/baidu_v2"
	"eflow.local/new-api/relay/channel/claude"
	"eflow.local/new-api/relay/channel/cloudflare"
	"eflow.local/new-api/relay/channel/codex"
	"eflow.local/new-api/relay/channel/cohere"
	"eflow.local/new-api/relay/channel/coze"
	"eflow.local/new-api/relay/channel/deepseek"
	"eflow.local/new-api/relay/channel/dify"
	"eflow.local/new-api/relay/channel/gemini"
	"eflow.local/new-api/relay/channel/jimeng"
	"eflow.local/new-api/relay/channel/jina"
	"eflow.local/new-api/relay/channel/minimax"
	"eflow.local/new-api/relay/channel/mistral"
	"eflow.local/new-api/relay/channel/mokaai"
	"eflow.local/new-api/relay/channel/moonshot"
	"eflow.local/new-api/relay/channel/ollama"
	"eflow.local/new-api/relay/channel/openai"
	"eflow.local/new-api/relay/channel/palm"
	"eflow.local/new-api/relay/channel/perplexity"
	"eflow.local/new-api/relay/channel/replicate"
	"eflow.local/new-api/relay/channel/siliconflow"
	"eflow.local/new-api/relay/channel/submodel"
	"eflow.local/new-api/relay/channel/tencent"
	"eflow.local/new-api/relay/channel/vertex"
	"eflow.local/new-api/relay/channel/volcengine"
	"eflow.local/new-api/relay/channel/xai"
	"eflow.local/new-api/relay/channel/xunfei"
	"eflow.local/new-api/relay/channel/zhipu"
	"eflow.local/new-api/relay/channel/zhipu_4v"
	relaycommon "eflow.local/new-api/relay/common"
	"github.com/gin-gonic/gin"
)

func GetAdaptor(apiType int) channel.Adaptor {
	switch apiType {
	case constant.APITypeAli:
		return &ali.Adaptor{}
	case constant.APITypeAnthropic:
		return &claude.Adaptor{}
	case constant.APITypeBaidu:
		return &baidu.Adaptor{}
	case constant.APITypeGemini:
		return &gemini.Adaptor{}
	case constant.APITypeOpenAI:
		return &openai.Adaptor{}
	case constant.APITypePaLM:
		return &palm.Adaptor{}
	case constant.APITypeTencent:
		return &tencent.DispatchAdaptor{}
	case constant.APITypeXunfei:
		return &xunfei.Adaptor{}
	case constant.APITypeZhipu:
		return &zhipu.Adaptor{}
	case constant.APITypeZhipuV4:
		return &zhipu_4v.Adaptor{}
	case constant.APITypeOllama:
		return &ollama.Adaptor{}
	case constant.APITypePerplexity:
		return &perplexity.Adaptor{}
	case constant.APITypeAws:
		return &aws.Adaptor{}
	case constant.APITypeCohere:
		return &cohere.Adaptor{}
	case constant.APITypeDify:
		return &dify.Adaptor{}
	case constant.APITypeJina:
		return &jina.Adaptor{}
	case constant.APITypeCloudflare:
		return &cloudflare.Adaptor{}
	case constant.APITypeSiliconFlow:
		return &siliconflow.Adaptor{}
	case constant.APITypeVertexAi:
		return &vertex.Adaptor{}
	case constant.APITypeMistral:
		return &mistral.Adaptor{}
	case constant.APITypeDeepSeek:
		return &deepseek.Adaptor{}
	case constant.APITypeMokaAI:
		return &mokaai.Adaptor{}
	case constant.APITypeVolcEngine:
		return &volcengine.Adaptor{}
	case constant.APITypeBaiduV2:
		return &baidu_v2.Adaptor{}
	case constant.APITypeOpenRouter:
		return &openai.Adaptor{}
	case constant.APITypeXinference:
		return &openai.Adaptor{}
	case constant.APITypeXai:
		return &xai.Adaptor{}
	case constant.APITypeCoze:
		return &coze.Adaptor{}
	case constant.APITypeJimeng:
		return &jimeng.Adaptor{}
	case constant.APITypeMoonshot:
		return &moonshot.Adaptor{} // Moonshot uses Claude API
	case constant.APITypeSubmodel:
		return &submodel.Adaptor{}
	case constant.APITypeMiniMax:
		return &minimax.Adaptor{}
	case constant.APITypeReplicate:
		return &replicate.Adaptor{}
	case constant.APITypeCodex:
		return &codex.Adaptor{}
	case constant.APITypeAliTTS:
		return &ali_tts.Adaptor{}
	case constant.APITypeAliASR:
		return &ali_asr.Adaptor{}
	case constant.APITypeAliASRRealtime:
		return &ali_asr_realtime.Adaptor{}
	}
	return nil
}

// GetAdaptorForInfo selects the protocol adaptor using both the legacy API
// type and channel-scoped settings. Advanced Custom is stored as the legacy
// Custom channel type (8), so it cannot be selected from ApiType alone; vLLM
// and SGLang always relay through their route preset.
func GetAdaptorForInfo(info *relaycommon.RelayInfo) channel.Adaptor {
	if info == nil || info.ChannelMeta == nil {
		return nil
	}
	if constant.SupportsAdvancedCustomRoutes(info.ChannelType) {
		if config := info.ChannelOtherSettings.AdvancedCustom; config != nil && advancedCustomRelayRouteConfigured(config, info) {
			return &advancedcustom.Adaptor{}
		}
	}
	return GetAdaptor(info.ApiType)
}

func advancedCustomRelayRouteConfigured(config *dto.AdvancedCustomConfig, info *relaycommon.RelayInfo) bool {
	if config == nil {
		return false
	}
	requestPath := ""
	if info != nil {
		requestPath = strings.TrimSpace(strings.Split(info.RequestURLPath, "?")[0])
	}
	if requestPath != "" && config.SupportsPathForModel(requestPath, info.OriginModelName) {
		return true
	}
	// A configuration with only management routes (/v1/models and balance)
	// must retain legacy Custom relay behavior. If a relay route exists but the
	// current path does not match, keep the advanced adaptor so it can return a
	// clear unsupported-route error instead of silently using the wrong endpoint.
	return config.HasRelayRoutes()
}

func GetTaskPlatform(c *gin.Context) constant.TaskPlatform {
	channelType := c.GetInt("channel_type")
	if channelType > 0 {
		return constant.TaskPlatform(strconv.Itoa(channelType))
	}
	return constant.TaskPlatform(c.GetString("platform"))
}
