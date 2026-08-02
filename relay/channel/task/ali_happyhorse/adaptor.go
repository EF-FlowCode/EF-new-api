package ali_happyhorse

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel"
	"github.com/QuantumNous/new-api/relay/channel/task/taskcommon"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
	"github.com/pkg/errors"
)

// ============================
// Request / Response structures
// ============================

// Request 是 happyhorse 系列的请求体。与 wan 系列不同：
// - 用 input.media: [MediaItem] 替代 input.img_url
// - parameters 多了 ratio 字段
type Request struct {
	Model      string      `json:"model"`
	Input      Input       `json:"input"`
	Parameters *Parameters `json:"parameters,omitempty"`
}

type Input struct {
	Prompt string      `json:"prompt,omitempty"`
	Media  []MediaItem `json:"media,omitempty"`
}

// MediaItem 的 type 字段按模型不同而不同：
// - i2v        → "first_frame"
// - r2v        → "reference_image"
// - video-edit → "video"
type MediaItem struct {
	Type string `json:"type"`
	URL  string `json:"url"`
}

type Parameters struct {
	Resolution string `json:"resolution,omitempty"` // "480P" | "720P" | "1080P"
	Ratio      string `json:"ratio,omitempty"`      // "16:9" | "9:16" | "1:1" ...
	Duration   int    `json:"duration,omitempty"`   // [3,15]
}

// Response 与阿里 wan 系列响应结构一致；本地定义避免跨包耦合。
type Response struct {
	Output    Output    `json:"output"`
	RequestID string    `json:"request_id"`
	Code      string    `json:"code,omitempty"`
	Message   string    `json:"message,omitempty"`
	Usage     *RespUsage `json:"usage,omitempty"`
}

type Output struct {
	TaskID        string `json:"task_id"`
	TaskStatus    string `json:"task_status"`
	SubmitTime    string `json:"submit_time,omitempty"`
	ScheduledTime string `json:"scheduled_time,omitempty"`
	EndTime       string `json:"end_time,omitempty"`
	OrigPrompt    string `json:"orig_prompt,omitempty"`
	ActualPrompt  string `json:"actual_prompt,omitempty"`
	VideoURL      string `json:"video_url,omitempty"`
	Code          string `json:"code,omitempty"`
	Message       string `json:"message,omitempty"`
}

type RespUsage struct {
	Duration            dto.IntValue `json:"duration,omitempty"`
	InputVideoDuration  dto.IntValue `json:"input_video_duration,omitempty"`
	OutputVideoDuration dto.IntValue `json:"output_video_duration,omitempty"`
	VideoCount          dto.IntValue `json:"video_count,omitempty"`
	SR                  dto.IntValue `json:"SR,omitempty"`
}

// Metadata 仅承载客户端可覆盖的扩展字段。
type Metadata struct {
	Resolution *string `json:"resolution,omitempty"`
	Ratio      *string `json:"ratio,omitempty"`
	MediaURL   *string `json:"media_url,omitempty"` // video-edit 用
}

// resolutionRatios 分辨率到价格倍率的映射（基础 = 720P = 1.0）。
// 阿里 happyhorse-1.0 全 4 模型统一定价：720P ¥0.9/秒、1080P ¥1.6/秒 → 1080P 倍率 = 1.6/0.9 ≈ 1.778。
// ModelPrice 配 720P 的每秒美元价（¥0.9 ÷ 汇率），1080P 自动按倍率乘出来。
var resolutionRatios = map[string]float64{
	"720P":  1.0,
	"1080P": 1.778,
}

// parseSize 把顶层 size 字段（OpenAI 风格如 "1280x720" / "1920*1080"）
// 解析成 happyhorse 用的 resolution + ratio。
// 长边判分辨率，宽高比判 ratio；解析失败返回 ok=false。
func parseSize(size string) (resolution, ratio string, ok bool) {
	if size == "" {
		return "", "", false
	}
	s := strings.ReplaceAll(strings.ToLower(strings.TrimSpace(size)), "x", "*")
	parts := strings.Split(s, "*")
	if len(parts) != 2 {
		return "", "", false
	}
	w, err1 := strconv.Atoi(strings.TrimSpace(parts[0]))
	h, err2 := strconv.Atoi(strings.TrimSpace(parts[1]))
	if err1 != nil || err2 != nil || w <= 0 || h <= 0 {
		return "", "", false
	}

	maxDim := w
	if h > maxDim {
		maxDim = h
	}
	switch {
	case maxDim <= 832:
		resolution = "480P"
	case maxDim <= 1280:
		resolution = "720P"
	default:
		resolution = "1080P"
	}

	switch {
	case w == h:
		ratio = "1:1"
	case w*9 == h*16:
		ratio = "16:9"
	case w*16 == h*9:
		ratio = "9:16"
	case w*3 == h*4:
		ratio = "4:3"
	case w*4 == h*3:
		ratio = "3:4"
	default:
		ratio = "16:9"
	}
	return resolution, ratio, true
}

// resolveResolution 综合 metadata、size、默认值得到最终分辨率与画幅比。
// 优先级：metadata > size > 默认 720P/16:9。
func resolveResolution(req relaycommon.TaskSubmitReq, meta Metadata) (resolution, ratio string) {
	resolution = "720P"
	ratio = "16:9"
	if r, rt, ok := parseSize(req.Size); ok {
		resolution, ratio = r, rt
	}
	if meta.Resolution != nil && *meta.Resolution != "" {
		resolution = *meta.Resolution
	}
	if meta.Ratio != nil && *meta.Ratio != "" {
		ratio = *meta.Ratio
	}
	return
}

// ============================
// Adaptor implementation
// ============================

type TaskAdaptor struct {
	taskcommon.BaseBilling
	ChannelType int
	apiKey      string
	baseURL     string
}

func (a *TaskAdaptor) Init(info *relaycommon.RelayInfo) {
	a.ChannelType = info.ChannelType
	a.baseURL = info.ChannelBaseUrl
	a.apiKey = info.ApiKey
}

func (a *TaskAdaptor) ValidateRequestAndSetAction(c *gin.Context, info *relaycommon.RelayInfo) (taskErr *dto.TaskError) {
	if err := relaycommon.ValidateMultipartDirect(c, info); err != nil {
		return err
	}

	req, err := relaycommon.GetTaskRequest(c)
	if err != nil {
		return service.TaskErrorWrapperLocal(err, "invalid_request", http.StatusBadRequest)
	}

	upstreamModel := req.Model
	if info.IsModelMapped {
		upstreamModel = info.UpstreamModelName
	}

	if req.Duration > 0 && (req.Duration < MinDuration || req.Duration > MaxDuration) {
		return service.TaskErrorWrapperLocal(
			fmt.Errorf("duration must be between %d and %d seconds, got %d", MinDuration, MaxDuration, req.Duration),
			"invalid_parameter", http.StatusBadRequest)
	}

	switch {
	case strings.HasSuffix(upstreamModel, "-i2v"):
		if req.InputReference == "" {
			return service.TaskErrorWrapperLocal(
				errors.New("i2v requires input_reference (image url or data uri)"),
				"invalid_parameter", http.StatusBadRequest)
		}
	case strings.HasSuffix(upstreamModel, "-r2v"):
		if req.InputReference == "" {
			return service.TaskErrorWrapperLocal(
				errors.New("r2v requires input_reference (reference image url or data uri)"),
				"invalid_parameter", http.StatusBadRequest)
		}
	case strings.HasSuffix(upstreamModel, "-video-edit"):
		var meta Metadata
		if err := taskcommon.UnmarshalMetadata(req.Metadata, &meta); err != nil {
			return service.TaskErrorWrapperLocal(err, "invalid_parameter", http.StatusBadRequest)
		}
		if meta.MediaURL == nil || *meta.MediaURL == "" {
			return service.TaskErrorWrapperLocal(
				errors.New("video-edit requires metadata.media_url (video url)"),
				"invalid_parameter", http.StatusBadRequest)
		}
	}

	return nil
}

func (a *TaskAdaptor) BuildRequestURL(info *relaycommon.RelayInfo) (string, error) {
	return fmt.Sprintf("%s/api/v1/services/aigc/video-generation/video-synthesis", a.baseURL), nil
}

func (a *TaskAdaptor) BuildRequestHeader(c *gin.Context, req *http.Request, info *relaycommon.RelayInfo) error {
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(a.apiKey))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-DashScope-Async", "enable") // 阿里异步任务必须设置
	return nil
}

func (a *TaskAdaptor) BuildRequestBody(c *gin.Context, info *relaycommon.RelayInfo) (io.Reader, error) {
	taskReq, err := relaycommon.GetTaskRequest(c)
	if err != nil {
		return nil, errors.Wrap(err, "get_task_request_failed")
	}

	hhReq, err := convertToRequest(info, taskReq)
	if err != nil {
		return nil, errors.Wrap(err, "convert_to_happyhorse_request_failed")
	}
	logger.LogJson(c, "happyhorse request body", hhReq)

	bodyBytes, err := common.Marshal(hhReq)
	if err != nil {
		return nil, errors.Wrap(err, "marshal_happyhorse_request_failed")
	}
	return bytes.NewReader(bodyBytes), nil
}

// convertToRequest 把 TaskSubmitReq 转成 happyhorse 的请求体。
// 必填字段和 duration 范围已在 ValidateRequestAndSetAction 校验过，此处假定有效。
func convertToRequest(info *relaycommon.RelayInfo, req relaycommon.TaskSubmitReq) (*Request, error) {
	upstreamModel := req.Model
	if info.IsModelMapped {
		upstreamModel = info.UpstreamModelName
	}

	var meta Metadata
	if err := taskcommon.UnmarshalMetadata(req.Metadata, &meta); err != nil {
		return nil, err
	}
	resolution, ratio := resolveResolution(req, meta)

	params := &Parameters{
		Resolution: resolution,
		Ratio:      ratio,
		Duration:   5,
	}
	if req.Duration > 0 {
		params.Duration = req.Duration
	}

	out := &Request{
		Model:      upstreamModel,
		Input:      Input{Prompt: req.Prompt},
		Parameters: params,
	}

	switch {
	case strings.HasSuffix(upstreamModel, "-t2v"):
		// 纯文生视频，不需要 media
	case strings.HasSuffix(upstreamModel, "-i2v"):
		out.Input.Media = []MediaItem{{Type: "first_frame", URL: req.InputReference}}
	case strings.HasSuffix(upstreamModel, "-r2v"):
		out.Input.Media = []MediaItem{{Type: "reference_image", URL: req.InputReference}}
	case strings.HasSuffix(upstreamModel, "-video-edit"):
		out.Input.Media = []MediaItem{{Type: "video", URL: *meta.MediaURL}}
	default:
		return nil, fmt.Errorf("unsupported happyhorse model: %s", upstreamModel)
	}

	return out, nil
}

// EstimateBilling 把请求侧 duration + 分辨率倍率写进 OtherRatios，
// 让 relay_task.go 在基础价格上乘出最终配额。
// ModelPrice 配为 720P 的每秒美元价；最终：
//   quota = ModelPrice * QuotaPerUnit * GroupRatio * seconds * resolution_ratio
func (a *TaskAdaptor) EstimateBilling(c *gin.Context, info *relaycommon.RelayInfo) map[string]float64 {
	taskReq, err := relaycommon.GetTaskRequest(c)
	if err != nil {
		return nil
	}

	duration := taskReq.Duration
	if duration <= 0 {
		duration = 5
	}
	if duration < MinDuration {
		duration = MinDuration
	} else if duration > MaxDuration {
		duration = MaxDuration
	}

	ratios := map[string]float64{
		"seconds": float64(duration),
	}

	var meta Metadata
	if err := taskcommon.UnmarshalMetadata(taskReq.Metadata, &meta); err == nil {
		resolution, _ := resolveResolution(taskReq, meta)
		if r, ok := resolutionRatios[resolution]; ok && r != 1.0 {
			ratios[fmt.Sprintf("resolution-%s", resolution)] = r
		}
	}

	return ratios
}

func (a *TaskAdaptor) DoRequest(c *gin.Context, info *relaycommon.RelayInfo, requestBody io.Reader) (*http.Response, error) {
	return channel.DoTaskApiRequest(a, c, info, requestBody)
}

func (a *TaskAdaptor) DoResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (taskID string, taskData []byte, taskErr *dto.TaskError) {
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		taskErr = service.TaskErrorWrapper(err, "read_response_body_failed", http.StatusInternalServerError)
		return
	}
	_ = resp.Body.Close()

	var hhResp Response
	if err := common.Unmarshal(responseBody, &hhResp); err != nil {
		taskErr = service.TaskErrorWrapper(errors.Wrapf(err, "body: %s", responseBody), "unmarshal_response_body_failed", http.StatusInternalServerError)
		return
	}

	if hhResp.Code != "" {
		taskErr = service.TaskErrorWrapper(fmt.Errorf("%s: %s", hhResp.Code, hhResp.Message), "happyhorse_api_error", resp.StatusCode)
		return
	}

	if hhResp.Output.TaskID == "" {
		taskErr = service.TaskErrorWrapper(fmt.Errorf("task_id is empty"), "invalid_response", http.StatusInternalServerError)
		return
	}

	openAIResp := dto.NewOpenAIVideo()
	openAIResp.ID = info.PublicTaskID
	openAIResp.TaskID = info.PublicTaskID
	openAIResp.Model = c.GetString("model")
	if openAIResp.Model == "" && info != nil {
		openAIResp.Model = info.OriginModelName
	}
	openAIResp.Status = convertStatus(hhResp.Output.TaskStatus)
	openAIResp.CreatedAt = common.GetTimestamp()

	c.JSON(http.StatusOK, openAIResp)

	return hhResp.Output.TaskID, responseBody, nil
}

func (a *TaskAdaptor) FetchTask(baseUrl, key string, body map[string]any, proxy string) (*http.Response, error) {
	taskID, ok := body["task_id"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid task_id")
	}

	uri := fmt.Sprintf("%s/api/v1/tasks/%s", baseUrl, taskID)

	req, err := http.NewRequest(http.MethodGet, uri, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(key))

	client, err := service.GetHttpClientWithProxy(proxy)
	if err != nil {
		return nil, fmt.Errorf("new proxy http client failed: %w", err)
	}
	return client.Do(req)
}

func (a *TaskAdaptor) GetModelList() []string {
	return ModelList
}

func (a *TaskAdaptor) GetChannelName() string {
	return ChannelName
}

func (a *TaskAdaptor) ParseTaskResult(respBody []byte) (*relaycommon.TaskInfo, error) {
	var hhResp Response
	if err := common.Unmarshal(respBody, &hhResp); err != nil {
		return nil, errors.Wrap(err, "unmarshal task result failed")
	}

	taskResult := relaycommon.TaskInfo{Code: 0}

	switch hhResp.Output.TaskStatus {
	case "PENDING":
		taskResult.Status = model.TaskStatusQueued
	case "RUNNING":
		taskResult.Status = model.TaskStatusInProgress
	case "SUCCEEDED":
		taskResult.Status = model.TaskStatusSuccess
		taskResult.Url = hhResp.Output.VideoURL
	case "FAILED", "CANCELED", "UNKNOWN":
		taskResult.Status = model.TaskStatusFailure
		switch {
		case hhResp.Message != "":
			taskResult.Reason = hhResp.Message
		case hhResp.Output.Message != "":
			taskResult.Reason = fmt.Sprintf("task failed, code: %s , message: %s", hhResp.Output.Code, hhResp.Output.Message)
		default:
			taskResult.Reason = "task failed"
		}
	default:
		taskResult.Status = model.TaskStatusQueued
	}

	return &taskResult, nil
}

func (a *TaskAdaptor) ConvertToOpenAIVideo(task *model.Task) ([]byte, error) {
	var hhResp Response
	if err := common.Unmarshal(task.Data, &hhResp); err != nil {
		return nil, errors.Wrap(err, "unmarshal happyhorse response failed")
	}

	openAIResp := dto.NewOpenAIVideo()
	openAIResp.ID = task.TaskID
	openAIResp.Status = convertStatus(hhResp.Output.TaskStatus)
	openAIResp.Model = task.Properties.OriginModelName
	openAIResp.SetProgressStr(task.Progress)
	openAIResp.CreatedAt = task.CreatedAt
	openAIResp.CompletedAt = task.UpdatedAt

	openAIResp.SetMetadata("url", hhResp.Output.VideoURL)

	switch {
	case hhResp.Code != "":
		openAIResp.Error = &dto.OpenAIVideoError{Code: hhResp.Code, Message: hhResp.Message}
	case hhResp.Output.Code != "":
		openAIResp.Error = &dto.OpenAIVideoError{Code: hhResp.Output.Code, Message: hhResp.Output.Message}
	}

	return common.Marshal(openAIResp)
}

func convertStatus(s string) string {
	switch s {
	case "PENDING":
		return dto.VideoStatusQueued
	case "RUNNING":
		return dto.VideoStatusInProgress
	case "SUCCEEDED":
		return dto.VideoStatusCompleted
	case "FAILED", "CANCELED", "UNKNOWN":
		return dto.VideoStatusFailed
	default:
		return dto.VideoStatusUnknown
	}
}
