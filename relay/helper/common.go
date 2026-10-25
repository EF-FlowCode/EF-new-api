package helper

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"eflow.local/new-api/common"
	"eflow.local/new-api/dto"
	gopool "eflow.local/new-api/internal/drainpool"
	"eflow.local/new-api/logger"
	"eflow.local/new-api/types"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

// streamKeepAliveMaxDuration caps a request keepalive, as the former
// upstream-request pinger did.
const streamKeepAliveMaxDuration = 120 * time.Minute

// streamKeepAliveWriter 在业务写入前保活，业务写入开始时先等心跳退出。
// 心跳只提交响应头、不算客户端输出，所以心跳期间请求仍可换渠道重试。
type streamKeepAliveWriter struct {
	gin.ResponseWriter
	mutex       sync.Mutex
	cancel      context.CancelFunc
	done        chan struct{}
	pingContext *gin.Context
}

func (writer *streamKeepAliveWriter) stop() {
	writer.cancel()
	<-writer.done
}

func (writer *streamKeepAliveWriter) Unwrap() http.ResponseWriter {
	return writer.ResponseWriter
}

func (writer *streamKeepAliveWriter) Write(data []byte) (int, error) {
	writer.stop()
	return writer.ResponseWriter.Write(data)
}

func (writer *streamKeepAliveWriter) WriteString(data string) (int, error) {
	writer.stop()
	return writer.ResponseWriter.WriteString(data)
}

func (writer *streamKeepAliveWriter) WriteHeader(status int) {
	writer.stop()
	writer.ResponseWriter.WriteHeader(status)
}

func (writer *streamKeepAliveWriter) WriteHeaderNow() {
	writer.stop()
	writer.ResponseWriter.WriteHeaderNow()
}

func (writer *streamKeepAliveWriter) Flush() {
	writer.stop()
	writer.ResponseWriter.Flush()
}

func (writer *streamKeepAliveWriter) Status() int {
	writer.mutex.Lock()
	defer writer.mutex.Unlock()
	return writer.ResponseWriter.Status()
}

func (writer *streamKeepAliveWriter) Size() int {
	writer.mutex.Lock()
	defer writer.mutex.Unlock()
	return writer.ResponseWriter.Size()
}

func (writer *streamKeepAliveWriter) Written() bool {
	writer.mutex.Lock()
	defer writer.mutex.Unlock()
	return writer.ResponseWriter.Written()
}

func (writer *streamKeepAliveWriter) ping() error {
	writer.mutex.Lock()
	defer writer.mutex.Unlock()
	return PingData(writer.pingContext)
}

// StartStreamKeepAlive 每隔 interval 发送一次心跳，覆盖请求准备、上游等待与重试间隙；
// 第一次心跳在一个间隔之后，短请求不会提前提交响应头。业务写入开始后交给流处理器。
// 已有未结束的保活时直接复用，内层清理不会提前结束外层保活。
func StartStreamKeepAlive(c *gin.Context, interval time.Duration) func() {
	if previous, ok := c.Writer.(*streamKeepAliveWriter); ok {
		select {
		case <-previous.done:
			c.Writer = previous.ResponseWriter
		default:
			return func() {}
		}
	}
	if requestContextDone(c) {
		return func() {}
	}
	if interval <= 0 {
		interval = DefaultPingInterval
	}
	SetEventStreamHeaders(c)
	ctx, cancel := context.WithTimeout(c.Request.Context(), streamKeepAliveMaxDuration)
	pingContext := c.Copy()
	// 心跳使用原 writer，业务写入才会触发交接。
	pingContext.Writer = c.Writer
	writer := &streamKeepAliveWriter{ResponseWriter: c.Writer, cancel: cancel, done: make(chan struct{}), pingContext: pingContext}
	c.Writer = writer
	// drainpool keeps the keepalive visible to graceful drain, like the
	// upstream-request pinger it replaced.
	gopool.Go(func() { writer.keepAlive(ctx, interval) })
	return writer.stop
}

func (writer *streamKeepAliveWriter) keepAlive(ctx context.Context, interval time.Duration) {
	defer close(writer.done)
	defer writer.cancel()
	defer func() {
		if recovered := recover(); recovered != nil {
			logger.LogDebug(writer.pingContext, "SSE ping goroutine panic recovered: %v", recovered)
		}
	}()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if ctx.Err() != nil {
				return
			}
			if err := writer.ping(); err != nil {
				logger.LogDebug(writer.pingContext, "SSE ping error, stopping keepalive: %s", err.Error())
				return
			}
		}
	}
}

// keepAliveTarget finds the stream writer that can write a ping without
// counting it as client output. It is installed by SetEventStreamHeaders.
func keepAliveTarget(writer http.ResponseWriter) *streamResponseWriter {
	for writer != nil {
		if target, ok := writer.(*streamResponseWriter); ok {
			return target
		}
		unwrapper, ok := writer.(interface{ Unwrap() http.ResponseWriter })
		if !ok {
			return nil
		}
		writer = unwrapper.Unwrap()
	}
	return nil
}

func FlushWriter(c *gin.Context) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("flush panic recovered: %v", r)
		}
	}()

	if c == nil || c.Writer == nil {
		return nil
	}

	if requestContextDone(c) {
		return fmt.Errorf("request context done: %w", c.Request.Context().Err())
	}

	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		return errors.New("streaming error: flusher not found")
	}

	flusher.Flush()
	// Gin's http.Flusher wrapper discards the underlying FlushError result.
	// Flush wrappers first so their buffers are preserved, then surface any
	// stored write error from an error-aware writer underneath them.
	var writer http.ResponseWriter = c.Writer
	for {
		if flusher, ok := writer.(interface{ FlushError() error }); ok {
			return flusher.FlushError()
		}
		unwrapper, ok := writer.(interface{ Unwrap() http.ResponseWriter })
		if !ok {
			break
		}
		writer = unwrapper.Unwrap()
	}
	return nil
}

func requestContextDone(c *gin.Context) bool {
	return c != nil && c.Request != nil && c.Request.Context().Err() != nil
}

func SetEventStreamHeaders(c *gin.Context) {
	// 检查是否已经设置过头部
	if _, exists := c.Get("event_stream_headers_set"); exists {
		return
	}

	// 设置标志，表示头部已经设置过
	c.Set("event_stream_headers_set", true)

	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")
	c.Writer.Header().Set("Transfer-Encoding", "chunked")
	c.Writer.Header().Set("X-Accel-Buffering", "no")
	c.Writer = &streamResponseWriter{ResponseWriter: c.Writer}
}

func ClaudeData(c *gin.Context, resp dto.ClaudeResponse) error {
	if requestContextDone(c) {
		return nil
	}

	jsonData, err := common.Marshal(resp)
	if err != nil {
		common.SysError("error marshalling stream response: " + err.Error())
	} else {
		c.Render(-1, common.CustomEvent{Data: fmt.Sprintf("event: %s\n", resp.Type)})
		c.Render(-1, common.CustomEvent{Data: "data: " + string(jsonData)})
	}
	_ = FlushWriter(c)
	return nil
}

func ClaudeChunkData(c *gin.Context, resp dto.ClaudeResponse, data string) {
	if requestContextDone(c) {
		return
	}

	c.Render(-1, common.CustomEvent{Data: fmt.Sprintf("event: %s\n", resp.Type)})
	c.Render(-1, common.CustomEvent{Data: fmt.Sprintf("data: %s\n", data)})
	_ = FlushWriter(c)
}

func ResponseChunkData(c *gin.Context, resp dto.ResponsesStreamResponse, data string) error {
	if requestContextDone(c) {
		return fmt.Errorf("request context done: %w", c.Request.Context().Err())
	}

	// Prefix every data line so multiline JSON remains one valid SSE event.
	data = strings.ReplaceAll(strings.ReplaceAll(data, "\r\n", "\n"), "\r", "\n")
	_, err := fmt.Fprintf(c.Writer, "event: %s\ndata: %s\n\n", resp.Type, strings.ReplaceAll(data, "\n", "\ndata: "))
	if err != nil {
		return err
	}
	return FlushWriter(c)
}

func StringData(c *gin.Context, str string) error {
	if c == nil || c.Writer == nil {
		return errors.New("context or writer is nil")
	}

	if requestContextDone(c) {
		return fmt.Errorf("request context done: %w", c.Request.Context().Err())
	}

	c.Render(-1, common.CustomEvent{Data: "data: " + str})
	return FlushWriter(c)
}

func PingData(c *gin.Context) error {
	if c == nil || c.Writer == nil {
		return errors.New("context or writer is nil")
	}

	if requestContextDone(c) {
		return fmt.Errorf("request context done: %w", c.Request.Context().Err())
	}

	ping := []byte(": PING\n\n")
	var err error
	if target := keepAliveTarget(c.Writer); target != nil {
		_, err = target.writeKeepAlive(ping)
	} else {
		_, err = c.Writer.Write(ping)
	}
	if err != nil {
		return fmt.Errorf("write ping data failed: %w", err)
	}
	return FlushWriter(c)
}

func ObjectData(c *gin.Context, object any) error {
	if object == nil {
		return errors.New("object is nil")
	}
	jsonData, err := common.Marshal(object)
	if err != nil {
		return fmt.Errorf("error marshalling object: %w", err)
	}
	return StringData(c, string(jsonData))
}

func Done(c *gin.Context) {
	_ = StringData(c, "[DONE]")
}

func WssString(c *gin.Context, ws *websocket.Conn, str string) error {
	if ws == nil {
		logger.LogError(c, "websocket connection is nil")
		return errors.New("websocket connection is nil")
	}
	//common.LogInfo(c, fmt.Sprintf("sending message: %s", str))
	return ws.WriteMessage(1, []byte(str))
}

func WssObject(c *gin.Context, ws *websocket.Conn, object any) error {
	jsonData, err := common.Marshal(object)
	if err != nil {
		return fmt.Errorf("error marshalling object: %w", err)
	}
	if ws == nil {
		logger.LogError(c, "websocket connection is nil")
		return errors.New("websocket connection is nil")
	}
	//common.LogInfo(c, fmt.Sprintf("sending message: %s", jsonData))
	return ws.WriteMessage(1, jsonData)
}

func WssError(c *gin.Context, ws *websocket.Conn, openaiError types.OpenAIError) {
	if ws == nil {
		return
	}
	errorObj := &dto.RealtimeEvent{
		Type:    "error",
		EventId: GetLocalRealtimeID(c),
		Error:   &openaiError,
	}
	_ = WssObject(c, ws, errorObj)
}

func GetResponseID(c *gin.Context) string {
	logID := c.GetString(common.RequestIdKey)
	return fmt.Sprintf("chatcmpl-%s", logID)
}

func GetLocalRealtimeID(c *gin.Context) string {
	logID := c.GetString(common.RequestIdKey)
	return fmt.Sprintf("evt_%s", logID)
}

func GenerateStartEmptyResponse(id string, createAt int64, model string, systemFingerprint *string) *dto.ChatCompletionsStreamResponse {
	return &dto.ChatCompletionsStreamResponse{
		Id:                id,
		Object:            "chat.completion.chunk",
		Created:           createAt,
		Model:             model,
		SystemFingerprint: systemFingerprint,
		Choices: []dto.ChatCompletionsStreamResponseChoice{
			{
				Delta: dto.ChatCompletionsStreamResponseChoiceDelta{
					Role:    "assistant",
					Content: common.GetPointer(""),
				},
			},
		},
	}
}

func GenerateStopResponse(id string, createAt int64, model string, finishReason string) *dto.ChatCompletionsStreamResponse {
	return &dto.ChatCompletionsStreamResponse{
		Id:                id,
		Object:            "chat.completion.chunk",
		Created:           createAt,
		Model:             model,
		SystemFingerprint: nil,
		Choices: []dto.ChatCompletionsStreamResponseChoice{
			{
				FinishReason: &finishReason,
			},
		},
	}
}

func GenerateFinalUsageResponse(id string, createAt int64, model string, usage dto.Usage) *dto.ChatCompletionsStreamResponse {
	return &dto.ChatCompletionsStreamResponse{
		Id:                id,
		Object:            "chat.completion.chunk",
		Created:           createAt,
		Model:             model,
		SystemFingerprint: nil,
		Choices:           make([]dto.ChatCompletionsStreamResponseChoice, 0),
		Usage:             &usage,
	}
}
