package ali_tts

// ModelList 是本渠道（阿里 TTS，channel type 59）支持的模型。
// 仅覆盖 DashScope 走 HTTP multimodal-generation 同步接口的 TTS 模型；
// realtime 系列（qwen3-tts-flash-realtime 等）走 WebSocket，由 OpenAI 类型
// 的 realtime 渠道承接，不在本 adapter 范围。
var ModelList = []string{
	"qwen-tts",
	"qwen3-tts-flash",
}

var ChannelName = "ali_tts"

// 阿里 multimodal-generation 同步语音合成接口。
const ttsEndpointPath = "/api/v1/services/aigc/multimodal-generation/generation"

// DashScope 缺省音色（实测 qwen-tts / qwen3-tts-flash 通用）。
const defaultVoice = "Cherry"
