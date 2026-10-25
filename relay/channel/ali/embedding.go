package ali

import (
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"

	"eflow.local/new-api/common"
	"eflow.local/new-api/dto"
	relaycommon "eflow.local/new-api/relay/common"
	"eflow.local/new-api/service"
	"eflow.local/new-api/types"

	"github.com/gin-gonic/gin"
)

// isMultimodalEmbeddingModel 识别走 dashscope native 多模态 embedding endpoint 的模型。
// 命中后会切到 /api/v1/services/embeddings/multimodal-embedding/multimodal-embedding，
// 请求 / 响应都按 dashscope 原生协议跑。模仿 image_wan.go 里的同类工具函数风格。
func isMultimodalEmbeddingModel(modelName string) bool {
	m := strings.ToLower(modelName)
	return strings.Contains(m, "multimodal-embedding") ||
		strings.Contains(m, "embedding-vision") ||
		strings.Contains(m, "vl-embedding")
}

func embeddingModelName(info *relaycommon.RelayInfo) string {
	if info.UpstreamModelName != "" {
		return info.UpstreamModelName
	}
	return info.OriginModelName
}

// ---------- dashscope 多模态 embedding 请求 / 响应结构 ----------

// AliMultimodalContent 是 dashscope contents 数组中的一个元素，
// text / image / video 三个字段最多设置一个。
type AliMultimodalContent struct {
	Text        string   `json:"text,omitempty"`
	Image       string   `json:"image,omitempty"`
	Video       string   `json:"video,omitempty"`
	MultiImages []string `json:"multi_images,omitempty"`
}

type AliMultimodalEmbeddingInput struct {
	Contents []AliMultimodalContent `json:"contents"`
}

type AliMultimodalEmbeddingParameters struct {
	Dimension    *int     `json:"dimension,omitempty"`
	FPS          *float64 `json:"fps,omitempty"`
	Instruct     string   `json:"instruct,omitempty"`
	EnableFusion *bool    `json:"enable_fusion,omitempty"`
}

type AliMultimodalEmbeddingRequest struct {
	Model      string                            `json:"model"`
	Input      AliMultimodalEmbeddingInput       `json:"input"`
	Parameters *AliMultimodalEmbeddingParameters `json:"parameters,omitempty"`
}

// AliMultimodalEmbeddingItem 对应 output.embeddings[] 中的一项。
// type 字段 dashscope 用来标注这条向量是文本 / 图像 / 视频 / vl 混合，
// 我们透传 index + embedding 给客户端，type 仅内部参考、不暴露到 OpenAI 响应。
type AliMultimodalEmbeddingItem struct {
	Embedding []float64 `json:"embedding"`
	Index     int       `json:"index"`
	Type      string    `json:"type,omitempty"`
}

type AliMultimodalEmbeddingResponse struct {
	Output struct {
		Embeddings []AliMultimodalEmbeddingItem `json:"embeddings"`
	} `json:"output"`
	Usage     AliUsage `json:"usage"`
	RequestId string   `json:"request_id"`
	AliError
}

// ---------- 请求转换：OpenAI vision 风格 → dashscope contents ----------

// ConvertMultimodalEmbeddingRequest 把 OpenAI 风格的 EmbeddingRequest 转成 dashscope
// 多模态 embedding 请求。约定 input 字段接受以下三种形态：
//  1. string —— 单条文本，转成 [{text}]
//  2. []string（JSON 数组里全是字符串）—— 每个 string 一个 {text}
//  3. []any 元素是 OpenAI vision content piece，例如:
//     [{"type":"image_url","image_url":{"url":"..."}},
//     {"type":"text","text":"..."},
//     {"type":"video_url","video_url":{"url":"..."}}]
//
// 也兼容 dashscope 风格的裸 dict（{"image":"...", "text":"...", "video":"..."}）直传。
func ConvertMultimodalEmbeddingRequest(req dto.EmbeddingRequest) (*AliMultimodalEmbeddingRequest, error) {
	if req.EncodingFormat != "" && req.EncodingFormat != "float" {
		return nil, errors.New("native multimodal embedding supports encoding_format=float only")
	}
	if req.Dimensions != nil && *req.Dimensions <= 0 {
		return nil, errors.New("dimensions must be positive")
	}
	if req.FPS != nil && (*req.FPS < 0 || *req.FPS > 1 || math.IsNaN(*req.FPS) || math.IsInf(*req.FPS, 0)) {
		return nil, errors.New("fps must be between 0 and 1")
	}
	if req.EnableFusion != nil && req.Model != "qwen3-vl-embedding" {
		return nil, errors.New("enable_fusion is supported only by qwen3-vl-embedding")
	}
	if req.Dimensions != nil && (req.Model == "tongyi-embedding-vision-plus" || req.Model == "tongyi-embedding-vision-flash" || req.Model == "multimodal-embedding-v1") {
		return nil, errors.New("this model has fixed dimensions")
	}
	if req.Input == nil {
		return nil, errors.New("embedding input is required for multimodal embedding model")
	}

	contents, err := buildMultimodalContents(req.Input)
	if err != nil {
		return nil, err
	}
	if len(contents) == 0 {
		return nil, errors.New("embedding input must contain at least one content element")
	}

	return &AliMultimodalEmbeddingRequest{
		Model:      req.Model,
		Input:      AliMultimodalEmbeddingInput{Contents: contents},
		Parameters: &AliMultimodalEmbeddingParameters{Dimension: req.Dimensions, FPS: req.FPS, Instruct: req.Instruct, EnableFusion: req.EnableFusion},
	}, nil
}

func buildMultimodalContents(input any) ([]AliMultimodalContent, error) {
	switch v := input.(type) {
	case string:
		return []AliMultimodalContent{{Text: v}}, nil
	case []any:
		out := make([]AliMultimodalContent, 0, len(v))
		for i, item := range v {
			content, err := convertOneContent(item)
			if err != nil {
				return nil, fmt.Errorf("input[%d]: %w", i, err)
			}
			out = append(out, content)
		}
		return out, nil
	case []string:
		out := make([]AliMultimodalContent, 0, len(v))
		for _, s := range v {
			out = append(out, AliMultimodalContent{Text: s})
		}
		return out, nil
	case map[string]any:
		// 单元素 dict 直接当一个 content
		content, err := convertOneContent(v)
		if err != nil {
			return nil, err
		}
		return []AliMultimodalContent{content}, nil
	default:
		return nil, fmt.Errorf("unsupported input type %T for multimodal embedding", input)
	}
}

func convertOneContent(item any) (AliMultimodalContent, error) {
	if s, ok := item.(string); ok {
		return AliMultimodalContent{Text: s}, nil
	}
	m, ok := item.(map[string]any)
	if !ok {
		return AliMultimodalContent{}, fmt.Errorf("element must be string or object, got %T", item)
	}

	// 1) dashscope 原生风格透传：{"text":"...", "image":"...", "video":"..."}
	if hasDashscopeShape(m) {
		c := AliMultimodalContent{}
		if t, ok := m["text"].(string); ok {
			c.Text = t
		}
		if img, ok := m["image"].(string); ok {
			c.Image = img
		}
		if vid, ok := m["video"].(string); ok {
			c.Video = vid
		}
		if images, ok := m["multi_images"].([]any); ok {
			for _, image := range images {
				url, ok := image.(string)
				if !ok || url == "" {
					return c, errors.New("multi_images must contain nonempty image URLs")
				}
				c.MultiImages = append(c.MultiImages, url)
			}
		}
		if images, ok := m["multi_images"].([]string); ok {
			c.MultiImages = images
		}
		if c.Text == "" && c.Image == "" && c.Video == "" && len(c.MultiImages) == 0 {
			return c, errors.New("empty multimodal content")
		}
		return c, nil
	}

	// 2) OpenAI vision 风格：{"type":"image_url"/"text"/"video_url", ...}
	tRaw, _ := m["type"].(string)
	switch tRaw {
	case "text":
		if s, ok := m["text"].(string); ok {
			return AliMultimodalContent{Text: s}, nil
		}
		return AliMultimodalContent{}, errors.New(`OpenAI "text" content missing string field "text"`)
	case "image_url", "image":
		if url, ok := extractURL(m["image_url"]); ok {
			return AliMultimodalContent{Image: url}, nil
		}
		if s, ok := m["image"].(string); ok {
			return AliMultimodalContent{Image: s}, nil
		}
		return AliMultimodalContent{}, errors.New(`OpenAI "image_url" content missing "image_url.url"`)
	case "video_url", "video":
		if url, ok := extractURL(m["video_url"]); ok {
			return AliMultimodalContent{Video: url}, nil
		}
		if s, ok := m["video"].(string); ok {
			return AliMultimodalContent{Video: s}, nil
		}
		return AliMultimodalContent{}, errors.New(`OpenAI "video_url" content missing "video_url.url"`)
	default:
		return AliMultimodalContent{}, fmt.Errorf(`unsupported content type %q (expected text / image_url / video_url, or dashscope-native {text|image|video})`, tRaw)
	}
}

func hasDashscopeShape(m map[string]any) bool {
	// 没有 "type" 字段、却含有 text / image / video 至少一个 string，视为 dashscope 原生
	if _, hasType := m["type"]; hasType {
		return false
	}
	if _, ok := m["multi_images"]; ok {
		return true
	}
	for _, k := range []string{"text", "image", "video"} {
		if _, ok := m[k].(string); ok {
			return true
		}
	}
	return false
}

func extractURL(raw any) (string, bool) {
	// OpenAI 风格 image_url 可能是 string 或 {"url":"..."}
	switch v := raw.(type) {
	case string:
		return v, v != ""
	case map[string]any:
		if u, ok := v["url"].(string); ok {
			return u, u != ""
		}
	}
	return "", false
}

// ---------- 响应处理：dashscope → OpenAI 兼容格式 ----------

// MultimodalEmbeddingHandler 读取 dashscope 多模态 embedding 响应，
// 转成 OpenAI 兼容的 {object:"list", data:[{object:"embedding", index, embedding}], model, usage}，
// 同时计算用于扣费的 dto.Usage（覆盖三种 usage 形态）。
func MultimodalEmbeddingHandler(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (*types.NewAPIError, *dto.Usage) {
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return types.NewOpenAIError(err, types.ErrorCodeReadResponseBodyFailed, http.StatusInternalServerError), nil
	}
	service.CloseResponseBodyGracefully(resp)

	var aliResp AliMultimodalEmbeddingResponse
	if err := common.Unmarshal(responseBody, &aliResp); err != nil {
		return types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError), nil
	}
	if aliResp.Code != "" {
		return types.WithOpenAIError(types.OpenAIError{
			Message: aliResp.Message,
			Type:    aliResp.Code,
			Param:   aliResp.RequestId,
			Code:    aliResp.Code,
		}, resp.StatusCode), nil
	}

	// 拼装 OpenAI 兼容数据段
	data := make([]dto.EmbeddingResponseItem, 0, len(aliResp.Output.Embeddings))
	for _, item := range aliResp.Output.Embeddings {
		data = append(data, dto.EmbeddingResponseItem{
			Object:    "embedding",
			Index:     item.Index,
			Embedding: item.Embedding,
		})
	}

	// Qwen VL and legacy v1 report text-only input_tokens; vision input
	// already includes images. total_tokens, when present, takes precedence.
	modelName := info.UpstreamModelName
	if modelName == "" {
		modelName = info.OriginModelName
	}
	textOnlyInput := modelName == "multimodal-embedding-v1" || modelName == "qwen2.5-vl-embedding" || modelName == "qwen3-vl-embedding"
	usage, usageErr := service.KnowledgeUsage(c, info, responseBody, textOnlyInput)
	if usageErr != nil {
		return types.NewOpenAIError(usageErr, types.ErrorCodeBadResponseBody, http.StatusBadGateway), nil
	}

	openaiResp := dto.EmbeddingResponse{
		Object: "list",
		Data:   data,
		Model:  info.UpstreamModelName,
		Usage:  *usage,
	}

	jsonResponse, err := common.Marshal(openaiResp)
	if err != nil {
		return types.NewError(err, types.ErrorCodeBadResponseBody), nil
	}
	c.Writer.Header().Set("Content-Type", "application/json")
	c.Writer.WriteHeader(resp.StatusCode)
	_, _ = c.Writer.Write(jsonResponse)
	return nil, usage
}
