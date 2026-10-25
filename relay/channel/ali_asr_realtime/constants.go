package ali_asr_realtime

import "strings"

// 阿里 ASR Realtime（channel type 61）。客户端统一用 OpenAI-realtime 风格
// 连 wss://<本站>/v1/realtime?model=<model>，本 adapter 按 model 分派到
// DashScope 两套不同的实时协议：
//
//   - qwen3-asr-flash-realtime-* : DashScope OpenAI-realtime 兼容端点
//     wss://dashscope.aliyuncs.com/api-ws/v1/realtime?model=<model>
//     两端协议一致 → 原样双向透传，仅旁路统计上行音频字节用于按秒计费。
//   - fun-asr-realtime-*         : DashScope 自有 inference 协议
//     wss://dashscope.aliyuncs.com/api-ws/v1/inference
//     run-task / 二进制音频帧 / finish-task ↔ OpenAI-realtime 事件互转。
//
// 计费：官方按音频时长 0.00033 元/秒。handler 把上行音频秒数写进
// RealtimeUsage.InputTokenDetails.TextTokens，PostWssConsumeQuota 按
// ModelRatio(=165 → 元/1M秒=330 → 0.00033 元/秒) 结算。DB option 权威。
var ModelList = []string{
	"qwen3-asr-flash-realtime-2026-02-10",
	"fun-asr-realtime-2026-02-28",
}

var ChannelName = "ali_asr_realtime"

const (
	realtimePath  = "/api-ws/v1/realtime"
	inferencePath = "/api-ws/v1/inference"
)

// isFunAsrRealtime 判定走 DashScope 自有 inference 协议（需协议桥接），
// 否则按 OpenAI-realtime 兼容端点原样透传。
func isFunAsrRealtime(model string) bool {
	return strings.HasPrefix(model, "fun-asr-realtime")
}

// 默认上行音频参数（DashScope 实时 ASR 缺省 pcm16 / 16k 单声道）。
// 用于把累计上行字节换算成计费秒数。
const (
	defaultSampleRate   = 16000
	bytesPerSamplePCM16 = 2
)
