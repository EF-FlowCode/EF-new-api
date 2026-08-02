package ali_asr_realtime

import (
	"encoding/base64"
	"fmt"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

// secondsFromBytes 把累计上行 PCM 字节按 pcm16/给定采样率换算成计费秒数，
// 至少 1 秒，避免漏计。
func secondsFromBytes(audioBytes int64, sampleRate int) int {
	if sampleRate <= 0 {
		sampleRate = defaultSampleRate
	}
	per := int64(sampleRate * bytesPerSamplePCM16)
	if per <= 0 {
		per = int64(defaultSampleRate * bytesPerSamplePCM16)
	}
	secs := int((audioBytes + per - 1) / per)
	if secs < 1 {
		secs = 1
	}
	return secs
}

func billSeconds(seconds int) *dto.RealtimeUsage {
	u := &dto.RealtimeUsage{InputTokens: seconds, TotalTokens: seconds}
	u.InputTokenDetails.TextTokens = seconds
	return u
}

// clientEvent 是从客户端 OpenAI-realtime 帧里需要识别的最小字段。
type clientEvent struct {
	Type    string `json:"type"`
	Audio   string `json:"audio"`
	Session struct {
		SampleRate       int             `json:"sample_rate"`
		InputAudioFormat json_RawAudioFmt `json:"input_audio_format"`
	} `json:"session"`
}

// input_audio_format 可能是字符串("pcm16")或对象({encoding,sample_rate})，
// 用宽松解析只为尽量取到 sample_rate（取不到就用默认 16k）。
type json_RawAudioFmt struct {
	SampleRate int `json:"sample_rate"`
}

func (j *json_RawAudioFmt) UnmarshalJSON(b []byte) error {
	if len(b) == 0 || b[0] == '"' || string(b) == "null" {
		return nil // 字符串形态或空，无 sample_rate 可取
	}
	type alias json_RawAudioFmt
	var a alias
	if err := common.Unmarshal(b, &a); err != nil {
		return nil // 宽松：解析不了就忽略
	}
	*j = json_RawAudioFmt(a)
	return nil
}

// ── qwen3-asr-flash-realtime：两端同为 OpenAI-realtime，原样双向透传 ──
//
// 仅在 client→upstream 方向旁路统计 input_audio_buffer.append 的音频字节，
// 用于按秒计费；帧本身一字不改转发，最大化兼容。
func passthroughRealtime(c *gin.Context, info *relaycommon.RelayInfo) (any, *types.NewAPIError) {
	clientConn := info.ClientWs
	targetConn := info.TargetWs

	var audioBytes int64
	sampleRate := defaultSampleRate
	done := make(chan struct{}, 2)

	// upstream → client（targetConn 的唯一写者是本 goroutine 的对端 clientConn）
	go func() {
		defer func() { recover(); done <- struct{}{} }()
		for {
			mt, msg, err := targetConn.ReadMessage()
			if err != nil {
				return
			}
			if err := clientConn.WriteMessage(mt, msg); err != nil {
				return
			}
		}
	}()

	// client → upstream
	go func() {
		defer func() { recover(); done <- struct{}{} }()
		for {
			mt, msg, err := clientConn.ReadMessage()
			if err != nil {
				return
			}
			if mt == websocket.TextMessage {
				var ev clientEvent
				if uErr := common.Unmarshal(msg, &ev); uErr == nil {
					switch ev.Type {
					case "session.update":
						if ev.Session.SampleRate > 0 {
							sampleRate = ev.Session.SampleRate
						} else if ev.Session.InputAudioFormat.SampleRate > 0 {
							sampleRate = ev.Session.InputAudioFormat.SampleRate
						}
					case "input_audio_buffer.append":
						if ev.Audio != "" {
							if raw, dErr := base64.StdEncoding.DecodeString(ev.Audio); dErr == nil {
								audioBytes += int64(len(raw))
							}
						}
					}
				}
			}
			if err := targetConn.WriteMessage(mt, msg); err != nil {
				return
			}
		}
	}()

	<-done // 任一方向断开即结束，关闭两端让另一 goroutine 退出
	_ = clientConn.Close()
	_ = targetConn.Close()
	<-done

	seconds := secondsFromBytes(audioBytes, sampleRate)
	logger.LogInfo(c, fmt.Sprintf("ali_asr_realtime passthrough done: audioBytes=%d sampleRate=%d seconds=%d",
		audioBytes, sampleRate, seconds))
	return billSeconds(seconds), nil
}

// ── fun-asr-realtime：客户端 OpenAI-realtime ↔ DashScope inference 协议桥接 ──
//
// 上游协议（实测）：run-task(text) → task-started → 二进制 PCM 帧 →
// finish-task(text) → result-generated(text)* → task-finished(text)。
// ⚠️ 本桥接无法在部署前真机联调，上线后需按 docs 验收单实测校准。

type dsHeader struct {
	Action    string `json:"action,omitempty"`
	TaskID    string `json:"task_id,omitempty"`
	Streaming string `json:"streaming,omitempty"`
	Event     string `json:"event,omitempty"`
}

type dsRunTask struct {
	Header  dsHeader        `json:"header"`
	Payload dsRunPayload    `json:"payload"`
}

type dsRunPayload struct {
	TaskGroup  string                 `json:"task_group"`
	Task       string                 `json:"task"`
	Function   string                 `json:"function"`
	Model      string                 `json:"model"`
	Parameters map[string]interface{} `json:"parameters"`
	Input      map[string]interface{} `json:"input"`
}

type dsFinishTask struct {
	Header  dsHeader               `json:"header"`
	Payload map[string]interface{} `json:"payload"`
}

type dsServerMsg struct {
	Header  dsHeader `json:"header"`
	Payload struct {
		Output struct {
			Sentence struct {
				Text string `json:"text"`
			} `json:"sentence"`
		} `json:"output"`
	} `json:"payload"`
}

func funAsrRealtimeBridge(c *gin.Context, info *relaycommon.RelayInfo) (any, *types.NewAPIError) {
	clientConn := info.ClientWs
	targetConn := info.TargetWs
	taskID := strings.ReplaceAll(uuid.NewString(), "-", "")

	// 1) 向 DashScope 发 run-task
	runTask := dsRunTask{
		Header: dsHeader{Action: "run-task", TaskID: taskID, Streaming: "duplex"},
		Payload: dsRunPayload{
			TaskGroup: "audio", Task: "asr", Function: "recognition",
			Model:      info.UpstreamModelName,
			Parameters: map[string]interface{}{"format": "pcm", "sample_rate": defaultSampleRate},
			Input:      map[string]interface{}{},
		},
	}
	rt, _ := common.Marshal(&runTask)
	if err := targetConn.WriteMessage(websocket.TextMessage, rt); err != nil {
		return &dto.RealtimeUsage{}, types.NewOpenAIError(fmt.Errorf("send run-task failed: %w", err),
			types.ErrorCodeBadResponse, 500)
	}

	// 2) 等 task-started（最多 15s）
	_ = targetConn.SetReadDeadline(time.Now().Add(15 * time.Second))
	for {
		mt, msg, err := targetConn.ReadMessage()
		if err != nil {
			return &dto.RealtimeUsage{}, types.NewOpenAIError(fmt.Errorf("wait task-started failed: %w", err),
				types.ErrorCodeBadResponse, 502)
		}
		if mt != websocket.TextMessage {
			continue
		}
		var sm dsServerMsg
		if uErr := common.Unmarshal(msg, &sm); uErr != nil {
			continue
		}
		if sm.Header.Event == "task-started" {
			break
		}
		if sm.Header.Event == "task-failed" {
			return &dto.RealtimeUsage{}, types.NewOpenAIError(
				fmt.Errorf("dashscope task-failed before start: %s", string(msg)),
				types.ErrorCodeBadResponse, 502)
		}
	}
	_ = targetConn.SetReadDeadline(time.Time{})

	// 3) 给 OpenAI-realtime 客户端回最小 session.created / session.updated
	_ = helper.WssObject(c, clientConn, map[string]interface{}{
		"type": "session.created", "session": map[string]interface{}{"id": taskID},
	})
	_ = helper.WssObject(c, clientConn, map[string]interface{}{
		"type": "session.updated", "session": map[string]interface{}{"id": taskID},
	})

	var audioBytes int64
	done := make(chan struct{}, 2)
	finishOnce := false

	sendFinish := func() {
		if finishOnce {
			return
		}
		finishOnce = true
		ft, _ := common.Marshal(&dsFinishTask{
			Header:  dsHeader{Action: "finish-task", TaskID: taskID, Streaming: "duplex"},
			Payload: map[string]interface{}{"input": map[string]interface{}{}},
		})
		_ = targetConn.WriteMessage(websocket.TextMessage, ft)
	}

	// client(OpenAI-realtime) → DashScope(binary)
	go func() {
		defer func() { recover(); done <- struct{}{} }()
		for {
			mt, msg, err := clientConn.ReadMessage()
			if err != nil {
				sendFinish()
				return
			}
			if mt != websocket.TextMessage {
				continue
			}
			var ev clientEvent
			if uErr := common.Unmarshal(msg, &ev); uErr != nil {
				continue
			}
			switch ev.Type {
			case "input_audio_buffer.append":
				if ev.Audio == "" {
					continue
				}
				raw, dErr := base64.StdEncoding.DecodeString(ev.Audio)
				if dErr != nil || len(raw) == 0 {
					continue
				}
				audioBytes += int64(len(raw))
				if wErr := targetConn.WriteMessage(websocket.BinaryMessage, raw); wErr != nil {
					return
				}
			case "input_audio_buffer.commit", "session.close", "response.create":
				sendFinish()
			}
		}
	}()

	// DashScope → client(OpenAI-realtime 事件)
	go func() {
		defer func() { recover(); done <- struct{}{} }()
		lastText := ""
		for {
			mt, msg, err := targetConn.ReadMessage()
			if err != nil {
				return
			}
			if mt != websocket.TextMessage {
				continue
			}
			var sm dsServerMsg
			if uErr := common.Unmarshal(msg, &sm); uErr != nil {
				continue
			}
			switch sm.Header.Event {
			case "result-generated":
				txt := sm.Payload.Output.Sentence.Text
				if txt != "" {
					lastText = txt
					_ = helper.WssObject(c, clientConn, map[string]interface{}{
						"type":  "conversation.item.input_audio_transcription.delta",
						"delta": txt,
					})
				}
			case "task-finished":
				_ = helper.WssObject(c, clientConn, map[string]interface{}{
					"type":       "conversation.item.input_audio_transcription.completed",
					"transcript": lastText,
				})
				return
			case "task-failed":
				_ = helper.WssObject(c, clientConn, map[string]interface{}{
					"type":  "error",
					"error": map[string]interface{}{"message": string(msg)},
				})
				return
			}
		}
	}()

	<-done
	_ = clientConn.Close()
	_ = targetConn.Close()
	<-done

	seconds := secondsFromBytes(audioBytes, defaultSampleRate)
	logger.LogInfo(c, fmt.Sprintf("ali_asr_realtime fun-asr bridge done: audioBytes=%d seconds=%d",
		audioBytes, seconds))
	return billSeconds(seconds), nil
}
