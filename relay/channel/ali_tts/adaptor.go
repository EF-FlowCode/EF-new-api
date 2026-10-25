package ali_tts

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"eflow.local/new-api/common"
	"eflow.local/new-api/dto"
	"eflow.local/new-api/logger"
	"eflow.local/new-api/relay/channel"
	relaycommon "eflow.local/new-api/relay/common"
	"eflow.local/new-api/service"
	"eflow.local/new-api/types"

	"github.com/gin-gonic/gin"
)

// Adaptor 把客户端的 OpenAI 风格 POST /v1/audio/speech 转成 DashScope 原生
// multimodal-generation 同步语音合成调用，再把生成的音频字节按 OpenAI
// /v1/audio/speech 规范回吐给客户端。覆盖 qwen-tts / qwen3-tts-flash。
//
// 实测协议（线上活 key）：
//   - 请求：{"model":"qwen-tts","input":{"text":"...","voice":"Cherry"}} 同步返回
//   - 响应：output.audio.url 是带过期时间的 OSS wav 链接
//   - qwen-tts        usage: input_tokens / output_tokens(audio_tokens)
//   - qwen3-tts-flash usage: 只有 characters（按字符计费）
//   - 200 但 body 带顶层 code 时是业务错误，需自检（AudioHelper 不拦 200）
type Adaptor struct{}

// dsRequest 是发往 DashScope multimodal-generation 的请求体。
type dsRequest struct {
	Model string  `json:"model"`
	Input dsInput `json:"input"`
}

type dsInput struct {
	Text  string `json:"text"`
	Voice string `json:"voice,omitempty"`
}

// dsResponse 覆盖 qwen-tts 与 qwen3-tts-flash 两种 usage 形态。
type dsResponse struct {
	Output struct {
		Audio struct {
			URL  string `json:"url"`
			Data string `json:"data"`
		} `json:"audio"`
		FinishReason string `json:"finish_reason"`
	} `json:"output"`
	Usage struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
		Characters   int `json:"characters"`
	} `json:"usage"`
	RequestID string `json:"request_id"`
	Code      string `json:"code"`
	Message   string `json:"message"`
}

func (a *Adaptor) Init(info *relaycommon.RelayInfo) {}

func (a *Adaptor) GetRequestURL(info *relaycommon.RelayInfo) (string, error) {
	return fmt.Sprintf("%s%s", info.ChannelBaseUrl, ttsEndpointPath), nil
}

func (a *Adaptor) SetupRequestHeader(c *gin.Context, req *http.Header, info *relaycommon.RelayInfo) error {
	channel.SetupApiRequestHeader(info, c, req)
	req.Set("Authorization", "Bearer "+strings.TrimSpace(info.ApiKey))
	req.Set("Content-Type", "application/json")
	// 注意：同步语音合成不能加 X-DashScope-Async，否则会被当成异步 task。
	return nil
}

func (a *Adaptor) ConvertAudioRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.AudioRequest) (io.Reader, error) {
	if strings.TrimSpace(request.Input) == "" {
		return nil, errors.New("input text is empty")
	}
	voice := strings.TrimSpace(request.Voice)
	if voice == "" {
		voice = defaultVoice
	}
	model := request.Model
	if info.IsModelMapped {
		model = info.UpstreamModelName
	}
	body, err := common.Marshal(&dsRequest{
		Model: model,
		Input: dsInput{Text: request.Input, Voice: voice},
	})
	if err != nil {
		return nil, fmt.Errorf("marshal ali tts request failed: %w", err)
	}
	return bytes.NewReader(body), nil
}

func (a *Adaptor) DoRequest(c *gin.Context, info *relaycommon.RelayInfo, requestBody io.Reader) (any, error) {
	return channel.DoApiRequest(a, c, info, requestBody)
}

func (a *Adaptor) DoResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (usage any, err *types.NewAPIError) {
	defer service.CloseResponseBodyGracefully(resp)

	body, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		return nil, types.NewOpenAIError(readErr, types.ErrorCodeReadResponseBodyFailed, http.StatusInternalServerError)
	}

	var dsResp dsResponse
	if uErr := common.Unmarshal(body, &dsResp); uErr != nil {
		return nil, types.NewOpenAIError(fmt.Errorf("unmarshal dashscope response failed: %w, body: %s", uErr, string(body)),
			types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}

	// DashScope 业务错误：HTTP 200 但 body 带顶层 code（AudioHelper 不会拦截 200）。
	if dsResp.Code != "" {
		return nil, types.NewOpenAIError(fmt.Errorf("dashscope error %s: %s", dsResp.Code, dsResp.Message),
			types.ErrorCodeBadResponse, http.StatusBadRequest)
	}
	if dsResp.Output.Audio.URL == "" {
		return nil, types.NewOpenAIError(fmt.Errorf("dashscope returned empty audio url, body: %s", string(body)),
			types.ErrorCodeBadResponse, http.StatusInternalServerError)
	}

	audioBytes, dErr := downloadAudio(c, dsResp.Output.Audio.URL)
	if dErr != nil {
		return nil, types.NewOpenAIError(dErr, types.ErrorCodeBadResponse, http.StatusInternalServerError)
	}

	// 按 OpenAI /v1/audio/speech 规范回吐二进制音频。DashScope 该接口固定出 wav。
	c.Writer.Header().Set("Content-Type", "audio/wav")
	c.Writer.WriteHeader(http.StatusOK)
	if _, wErr := c.Writer.Write(audioBytes); wErr != nil {
		logger.LogError(c, fmt.Sprintf("write ali tts audio failed: %v", wErr))
	}

	// 计费（按 token，与官方口径对齐）：
	//   qwen-tts        → input_tokens / output_tokens(音频) 真实计
	//   qwen3-tts-flash → 只有 characters，按字符当 token
	// 一律不写 AudioTokens，使 AudioHelper 走 PostTextConsumeQuota
	// （model_ratio + completion_ratio 结算，等价官方按 token 计费）。
	u := &dto.Usage{}
	switch {
	case dsResp.Usage.InputTokens > 0 || dsResp.Usage.OutputTokens > 0:
		u.PromptTokens = dsResp.Usage.InputTokens
		u.CompletionTokens = dsResp.Usage.OutputTokens
	case dsResp.Usage.Characters > 0:
		u.PromptTokens = dsResp.Usage.Characters
	default:
		u.PromptTokens = info.GetEstimatePromptTokens()
	}
	u.PromptTokensDetails.TextTokens = u.PromptTokens
	u.TotalTokens = u.PromptTokens + u.CompletionTokens
	return u, nil
}

// downloadAudio 拉取 DashScope 返回的 OSS 音频链接（带过期时间，需限时下载）。
func downloadAudio(c *gin.Context, url string) ([]byte, error) {
	client, err := service.GetHttpClientWithProxy("")
	if err != nil {
		return nil, fmt.Errorf("new http client failed: %w", err)
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 60*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build audio download request failed: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("download audio failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download audio bad status: %d", resp.StatusCode)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read audio body failed: %w", err)
	}
	if len(data) == 0 {
		return nil, errors.New("downloaded audio is empty")
	}
	return data, nil
}

func (a *Adaptor) GetModelList() []string { return ModelList }
func (a *Adaptor) GetChannelName() string { return ChannelName }

// 以下是 channel.Adaptor 接口的其余方法，本 TTS adapter 不涉及，统一占位。
func (a *Adaptor) ConvertOpenAIRequest(c *gin.Context, info *relaycommon.RelayInfo, request *dto.GeneralOpenAIRequest) (any, error) {
	return nil, errors.New("not implemented")
}
func (a *Adaptor) ConvertRerankRequest(c *gin.Context, relayMode int, request dto.RerankRequest) (any, error) {
	return nil, errors.New("not implemented")
}
func (a *Adaptor) ConvertEmbeddingRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.EmbeddingRequest) (any, error) {
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
