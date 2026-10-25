package ali_asr_realtime

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"eflow.local/new-api/dto"
	"eflow.local/new-api/relay/channel"
	relaycommon "eflow.local/new-api/relay/common"
	"eflow.local/new-api/types"

	"github.com/gin-gonic/gin"
)

// Adaptor 接入 new-api 现成 realtime 中继（RelayModeRealtime → WssHelper →
// DoWssRequest 拨上游 + DoResponse 双向泵）。DoResponse 必须返回
// *dto.RealtimeUsage（WssHelper 内会做类型断言后交 PostWssConsumeQuota）。
type Adaptor struct{}

func (a *Adaptor) Init(info *relaycommon.RelayInfo) {}

func (a *Adaptor) GetRequestURL(info *relaycommon.RelayInfo) (string, error) {
	base := info.ChannelBaseUrl
	switch {
	case strings.HasPrefix(base, "https://"):
		base = "wss://" + strings.TrimPrefix(base, "https://")
	case strings.HasPrefix(base, "http://"):
		base = "ws://" + strings.TrimPrefix(base, "http://")
	}
	base = strings.TrimRight(base, "/")
	model := info.UpstreamModelName
	if isFunAsrRealtime(model) {
		// DashScope 自有 inference 协议，model 走 run-task payload，不进 URL。
		return base + inferencePath, nil
	}
	// DashScope OpenAI-realtime 兼容端点，model 进 query。
	return fmt.Sprintf("%s%s?model=%s", base, realtimePath, model), nil
}

func (a *Adaptor) SetupRequestHeader(c *gin.Context, header *http.Header, info *relaycommon.RelayInfo) error {
	channel.SetupApiRequestHeader(info, c, header)
	// DashScope WS 鉴权：Authorization: bearer <api-key>（实测小写 bearer 可用）。
	header.Set("Authorization", "bearer "+strings.TrimSpace(info.ApiKey))
	return nil
}

func (a *Adaptor) DoRequest(c *gin.Context, info *relaycommon.RelayInfo, requestBody io.Reader) (any, error) {
	return channel.DoWssRequest(a, c, info, requestBody)
}

func (a *Adaptor) DoResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (usage any, err *types.NewAPIError) {
	if info == nil || info.ClientWs == nil || info.TargetWs == nil {
		return &dto.RealtimeUsage{}, types.NewError(errors.New("invalid websocket connection"), types.ErrorCodeBadResponse)
	}
	if isFunAsrRealtime(info.UpstreamModelName) {
		return funAsrRealtimeBridge(c, info)
	}
	return passthroughRealtime(c, info)
}

func (a *Adaptor) GetModelList() []string { return ModelList }
func (a *Adaptor) GetChannelName() string { return ChannelName }

// 其余 channel.Adaptor 接口方法本 adapter 不涉及，统一占位。
func (a *Adaptor) ConvertOpenAIRequest(c *gin.Context, info *relaycommon.RelayInfo, request *dto.GeneralOpenAIRequest) (any, error) {
	return nil, errors.New("not implemented")
}
func (a *Adaptor) ConvertRerankRequest(c *gin.Context, relayMode int, request dto.RerankRequest) (any, error) {
	return nil, errors.New("not implemented")
}
func (a *Adaptor) ConvertEmbeddingRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.EmbeddingRequest) (any, error) {
	return nil, errors.New("not implemented")
}
func (a *Adaptor) ConvertAudioRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.AudioRequest) (io.Reader, error) {
	return nil, errors.New("not implemented")
}
func (a *Adaptor) ConvertImageRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.ImageRequest) (any, error) {
	return nil, errors.New("not implemented")
}
func (a *Adaptor) ConvertOpenAIResponsesRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.OpenAIResponsesRequest) (any, error) {
	return nil, errors.New("not implemented")
}
func (a *Adaptor) ConvertClaudeRequest(c *gin.Context, info *relaycommon.RelayInfo, request *dto.ClaudeRequest) (any, error) {
	return nil, errors.New("not implemented")
}
func (a *Adaptor) ConvertGeminiRequest(c *gin.Context, info *relaycommon.RelayInfo, request *dto.GeminiChatRequest) (any, error) {
	return nil, errors.New("not implemented")
}
