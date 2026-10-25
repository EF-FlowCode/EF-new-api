package ali_asr

import (
	"bytes"
	"context"
	"encoding/base64"
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
	relayconstant "eflow.local/new-api/relay/constant"
	"eflow.local/new-api/service"
	"eflow.local/new-api/types"

	"github.com/gin-gonic/gin"
)

// Adaptor 把客户端 OpenAI 风格 POST /v1/audio/transcriptions（multipart 上传
// 音频）转成 DashScope 原生 ASR 调用，再把识别文本按 OpenAI 转写规范回吐。
//
//   - qwen3-asr-flash : multimodal-generation 同步识别
//   - fun-asr         : 异步录音转写（submit + 轮询 task），file_urls 直接
//     传 base64 data: URI（DashScope 自动转存 OSS）
//
// 计费：阿里 ASR 官方按「音频时长 元/秒」计（0.00022 元/秒）。沿用 AliTTS
// 「按计费单位当 token」手法：把音频秒数当 PromptTokens，ModelRatio 编码
// 元/1M秒（110 → 0.00022 元/秒），走 PostTextConsumeQuota。DB option 权威。
type Adaptor struct {
	responseFormat string
	model          string
	async          bool
}

// ── 同步 multimodal-generation ──────────────────────────────────────

type dsRequest struct {
	Model      string       `json:"model"`
	Input      dsInput      `json:"input"`
	Parameters dsParameters `json:"parameters"`
}

type dsInput struct {
	Messages []dsMessage `json:"messages"`
}

type dsMessage struct {
	Role    string      `json:"role"`
	Content []dsContent `json:"content"`
}

type dsContent struct {
	Text  string `json:"text,omitempty"`
	Audio string `json:"audio,omitempty"`
}

type dsParameters struct {
	AsrOptions dsAsrOptions `json:"asr_options"`
}

type dsAsrOptions struct {
	EnableItn bool `json:"enable_itn"`
}

type dsSyncResponse struct {
	Output struct {
		Choices []struct {
			Message struct {
				Content []struct {
					Text string `json:"text"`
				} `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	} `json:"output"`
	Usage struct {
		AudioTokens int `json:"audio_tokens"`
		Seconds     int `json:"seconds"`
	} `json:"usage"`
	RequestID string `json:"request_id"`
	Code      string `json:"code"`
	Message   string `json:"message"`
}

// ── 异步录音转写（fun-asr）──────────────────────────────────────────

type dsAsyncSubmit struct {
	Model string             `json:"model"`
	Input dsAsyncSubmitInput `json:"input"`
}

// fun-asr 用 file_urls(数组)；qwen3-asr-flash-filetrans 用 file_url(单数)。
type dsAsyncSubmitInput struct {
	FileURLs []string `json:"file_urls,omitempty"`
	FileURL  string   `json:"file_url,omitempty"`
}

type dsTaskResponse struct {
	Output struct {
		TaskID     string `json:"task_id"`
		TaskStatus string `json:"task_status"`
		Code       string `json:"code"`
		Message    string `json:"message"`
		// fun-asr：results 数组
		Results []struct {
			TranscriptionURL string `json:"transcription_url"`
			SubTaskStatus    string `json:"subtask_status"`
		} `json:"results"`
		// qwen3-asr-flash-filetrans：result 单数对象
		Result struct {
			TranscriptionURL string `json:"transcription_url"`
		} `json:"result"`
	} `json:"output"`
	Usage struct {
		Duration int `json:"duration"` // fun-asr 计费秒数
		Seconds  int `json:"seconds"`  // qwen3-asr-flash-filetrans 计费秒数
	} `json:"usage"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

type dsTranscriptionDoc struct {
	Transcripts []struct {
		Text string `json:"text"`
	} `json:"transcripts"`
}

func (a *Adaptor) Init(info *relaycommon.RelayInfo) {}

func (a *Adaptor) GetRequestURL(info *relaycommon.RelayInfo) (string, error) {
	if a.async {
		return fmt.Sprintf("%s%s", info.ChannelBaseUrl, transcriptionPath), nil
	}
	return fmt.Sprintf("%s%s", info.ChannelBaseUrl, mmGenerationPath), nil
}

func (a *Adaptor) SetupRequestHeader(c *gin.Context, req *http.Header, info *relaycommon.RelayInfo) error {
	channel.SetupApiRequestHeader(info, c, req)
	req.Set("Authorization", "Bearer "+strings.TrimSpace(info.ApiKey))
	req.Set("Content-Type", "application/json")
	if a.async {
		// 异步录音转写必须声明 async，否则上游不按 task 处理。
		req.Set("X-DashScope-Async", "enable")
	}
	return nil
}

func (a *Adaptor) ConvertAudioRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.AudioRequest) (io.Reader, error) {
	if info.RelayMode != relayconstant.RelayModeAudioTranscription {
		return nil, errors.New("ali_asr only supports /v1/audio/transcriptions")
	}
	a.responseFormat = strings.TrimSpace(request.ResponseFormat)

	a.model = request.Model
	if info.IsModelMapped {
		a.model = info.UpstreamModelName
	}
	a.async = isAsyncModel(a.model)

	formData, err := common.ParseMultipartFormReusable(c)
	if err != nil {
		return nil, fmt.Errorf("error parsing multipart form: %w", err)
	}
	fileHeaders := formData.File["file"]
	if len(fileHeaders) == 0 {
		return nil, errors.New("file is required")
	}
	fileHeader := fileHeaders[0]
	f, err := fileHeader.Open()
	if err != nil {
		return nil, fmt.Errorf("error opening audio file: %w", err)
	}
	defer f.Close()
	raw, err := io.ReadAll(f)
	if err != nil {
		return nil, fmt.Errorf("read audio file failed: %w", err)
	}
	if len(raw) == 0 {
		return nil, errors.New("uploaded audio file is empty")
	}

	mime := audioMimeFromName(fileHeader.Filename)
	dataURI := fmt.Sprintf("data:%s;base64,%s", mime, base64.StdEncoding.EncodeToString(raw))

	var body []byte
	if a.async {
		// 异步录音转写，file_url(s) 直接吃 base64 data: URI（DashScope
		// 自动转存 OSS）。filetrans 用单数 file_url，fun-asr 用数组 file_urls。
		in := dsAsyncSubmitInput{}
		if isSingularFileURLModel(a.model) {
			in.FileURL = dataURI
		} else {
			in.FileURLs = []string{dataURI}
		}
		body, err = common.Marshal(&dsAsyncSubmit{Model: a.model, Input: in})
	} else {
		// 用 map 精确构造，避免 struct `omitempty` 把 system 的空
		// {"text":""} 整个吞掉（→ DashScope 400 "content should be a
		// message of a certain modal"）；audio item 也不能混入空 text。
		body, err = common.Marshal(map[string]any{
			"model": a.model,
			"input": map[string]any{
				"messages": []map[string]any{
					{"role": "system", "content": []map[string]any{{"text": ""}}},
					{"role": "user", "content": []map[string]any{{"audio": dataURI}}},
				},
			},
			"parameters": map[string]any{
				"asr_options": map[string]any{"enable_itn": false},
			},
		})
	}
	if err != nil {
		return nil, fmt.Errorf("marshal ali asr request failed: %w", err)
	}
	return bytes.NewReader(body), nil
}

func (a *Adaptor) DoRequest(c *gin.Context, info *relaycommon.RelayInfo, requestBody io.Reader) (any, error) {
	// 同步与异步首跳都是 JSON POST（异步首跳=提交 task）。
	return channel.DoApiRequest(a, c, info, requestBody)
}

func (a *Adaptor) DoResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (usage any, err *types.NewAPIError) {
	defer service.CloseResponseBodyGracefully(resp)
	body, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		return nil, types.NewOpenAIError(readErr, types.ErrorCodeReadResponseBodyFailed, http.StatusInternalServerError)
	}
	if a.async {
		return a.handleAsync(c, info, body)
	}
	return a.handleSync(c, body)
}

func (a *Adaptor) handleSync(c *gin.Context, body []byte) (any, *types.NewAPIError) {
	var r dsSyncResponse
	if uErr := common.Unmarshal(body, &r); uErr != nil {
		return nil, types.NewOpenAIError(fmt.Errorf("unmarshal dashscope response failed: %w, body: %s", uErr, string(body)),
			types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}
	// DashScope 业务错误：HTTP 200 但 body 带顶层 code（AudioHelper 不拦 200）。
	if r.Code != "" {
		return nil, types.NewOpenAIError(fmt.Errorf("dashscope error %s: %s", r.Code, r.Message),
			types.ErrorCodeBadResponse, http.StatusBadRequest)
	}
	text := ""
	if len(r.Output.Choices) > 0 && len(r.Output.Choices[0].Message.Content) > 0 {
		text = r.Output.Choices[0].Message.Content[0].Text
	}
	a.writeTranscription(c, text)

	seconds := r.Usage.Seconds
	if seconds <= 0 && r.Usage.AudioTokens > 0 {
		seconds = (r.Usage.AudioTokens + 24) / 25 // 兜底：实测 ~25 token/s
	}
	return billBySeconds(seconds), nil
}

func (a *Adaptor) handleAsync(c *gin.Context, info *relaycommon.RelayInfo, submitBody []byte) (any, *types.NewAPIError) {
	var sub dsTaskResponse
	if uErr := common.Unmarshal(submitBody, &sub); uErr != nil {
		return nil, types.NewOpenAIError(fmt.Errorf("unmarshal submit response failed: %w, body: %s", uErr, string(submitBody)),
			types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}
	if sub.Code != "" && !strings.EqualFold(sub.Code, "Success") {
		return nil, types.NewOpenAIError(fmt.Errorf("dashscope submit error %s: %s", sub.Code, sub.Message),
			types.ErrorCodeBadResponse, http.StatusBadRequest)
	}
	taskID := sub.Output.TaskID
	if taskID == "" {
		return nil, types.NewOpenAIError(fmt.Errorf("dashscope returned empty task_id, body: %s", string(submitBody)),
			types.ErrorCodeBadResponse, http.StatusInternalServerError)
	}

	client, cErr := service.GetHttpClientWithProxy("")
	if cErr != nil {
		return nil, types.NewOpenAIError(fmt.Errorf("new http client failed: %w", cErr),
			types.ErrorCodeBadResponse, http.StatusInternalServerError)
	}

	taskURL := fmt.Sprintf("%s%s%s", info.ChannelBaseUrl, tasksPathPrefix, taskID)
	deadline := time.Now().Add(150 * time.Second)
	var final dsTaskResponse
	for {
		if time.Now().After(deadline) {
			return nil, types.NewOpenAIError(errors.New("dashscope transcription task polling timeout"),
				types.ErrorCodeBadResponse, http.StatusGatewayTimeout)
		}
		st, pErr := pollTask(c.Request.Context(), client, taskURL, info.ApiKey)
		if pErr != nil {
			return nil, types.NewOpenAIError(pErr, types.ErrorCodeBadResponse, http.StatusInternalServerError)
		}
		status := strings.ToUpper(st.Output.TaskStatus)
		if status == "SUCCEEDED" {
			final = *st
			break
		}
		if status == "FAILED" || status == "CANCELED" {
			return nil, types.NewOpenAIError(
				fmt.Errorf("dashscope transcription task %s: %s %s", status, st.Output.Code, st.Output.Message),
				types.ErrorCodeBadResponse, http.StatusBadRequest)
		}
		time.Sleep(2 * time.Second)
	}

	// fun-asr 结果在 output.results[0]，filetrans 在 output.result（单数）。
	txURL := ""
	if len(final.Output.Results) > 0 {
		txURL = final.Output.Results[0].TranscriptionURL
	}
	if txURL == "" {
		txURL = final.Output.Result.TranscriptionURL
	}
	if txURL == "" {
		return nil, types.NewOpenAIError(errors.New("dashscope task succeeded but no transcription_url"),
			types.ErrorCodeBadResponse, http.StatusInternalServerError)
	}
	text, tErr := fetchTranscript(c.Request.Context(), client, txURL)
	if tErr != nil {
		return nil, types.NewOpenAIError(tErr, types.ErrorCodeBadResponse, http.StatusInternalServerError)
	}
	a.writeTranscription(c, text)
	// fun-asr 计费秒数在 usage.duration，filetrans 在 usage.seconds。
	secs := final.Usage.Duration
	if secs <= 0 {
		secs = final.Usage.Seconds
	}
	return billBySeconds(secs), nil
}

func pollTask(ctx context.Context, client *http.Client, taskURL, apiKey string) (*dsTaskResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, taskURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build task poll request failed: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(apiKey))
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("poll task failed: %w", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read task poll body failed: %w", err)
	}
	var tr dsTaskResponse
	if err := common.Unmarshal(data, &tr); err != nil {
		return nil, fmt.Errorf("unmarshal task poll failed: %w, body: %s", err, string(data))
	}
	return &tr, nil
}

func fetchTranscript(ctx context.Context, client *http.Client, url string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("build transcript request failed: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("download transcript failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download transcript bad status: %d", resp.StatusCode)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read transcript body failed: %w", err)
	}
	var doc dsTranscriptionDoc
	if err := common.Unmarshal(data, &doc); err != nil {
		return "", fmt.Errorf("unmarshal transcript doc failed: %w", err)
	}
	parts := make([]string, 0, len(doc.Transcripts))
	for _, t := range doc.Transcripts {
		if strings.TrimSpace(t.Text) != "" {
			parts = append(parts, t.Text)
		}
	}
	return strings.Join(parts, "\n"), nil
}

// writeTranscription 按 OpenAI /v1/audio/transcriptions 规范回吐。默认 json：
// {"text":"..."}；response_format=text 时回纯文本；verbose_json 退化为 json。
func (a *Adaptor) writeTranscription(c *gin.Context, text string) {
	if strings.EqualFold(a.responseFormat, "text") {
		c.Writer.Header().Set("Content-Type", "text/plain; charset=utf-8")
		c.Writer.WriteHeader(http.StatusOK)
		if _, wErr := c.Writer.WriteString(text); wErr != nil {
			logger.LogError(c, fmt.Sprintf("write ali asr text failed: %v", wErr))
		}
		return
	}
	out, _ := common.Marshal(map[string]string{"text": text})
	c.Writer.Header().Set("Content-Type", "application/json")
	c.Writer.WriteHeader(http.StatusOK)
	if _, wErr := c.Writer.Write(out); wErr != nil {
		logger.LogError(c, fmt.Sprintf("write ali asr json failed: %v", wErr))
	}
}

// billBySeconds 把音频秒数当 PromptTokens（不写 AudioTokens → 走
// PostTextConsumeQuota，ModelRatio 编码 元/1M秒）。至少计 1 秒，避免漏计。
func billBySeconds(seconds int) *dto.Usage {
	if seconds <= 0 {
		seconds = 1
	}
	u := &dto.Usage{PromptTokens: seconds, TotalTokens: seconds}
	u.PromptTokensDetails.TextTokens = seconds
	return u
}

// audioMimeFromName 按上传文件名扩展名推断 mime；DashScope data: URI 需要。
func audioMimeFromName(name string) string {
	lower := strings.ToLower(name)
	switch {
	case strings.HasSuffix(lower, ".mp3"):
		return "audio/mpeg"
	case strings.HasSuffix(lower, ".m4a"), strings.HasSuffix(lower, ".aac"):
		return "audio/aac"
	case strings.HasSuffix(lower, ".ogg"), strings.HasSuffix(lower, ".opus"):
		return "audio/ogg"
	case strings.HasSuffix(lower, ".flac"):
		return "audio/flac"
	case strings.HasSuffix(lower, ".webm"):
		return "audio/webm"
	default:
		return "audio/wav"
	}
}

func (a *Adaptor) GetModelList() []string { return ModelList }
func (a *Adaptor) GetChannelName() string { return ChannelName }

// 以下是 channel.Adaptor 接口的其余方法，本 ASR adapter 不涉及，统一占位。
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
