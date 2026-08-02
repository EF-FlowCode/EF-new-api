package ali_asr

// ModelList 是本渠道（阿里 ASR，channel type 60）支持的模型。
// 仅覆盖 DashScope 走 HTTP 同步/异步 转写接口的 ASR 模型；
// realtime 系列（qwen3-asr-flash-realtime / fun-asr-realtime）走 WebSocket，
// 由 OpenAI 类型的 realtime 渠道承接，不在本 adapter 范围。
//
//   - qwen3-asr-flash           : multimodal-generation 同步，base64 上传
//   - fun-asr                   : 异步录音转写，input.file_urls(数组)，
//                                 结果 output.results[].transcription_url
//   - qwen3-asr-flash-filetrans : 异步录音转写(最长 12h)，input 用
//                                 file_url(单数字符串)，结果在
//                                 output.result.transcription_url(单数对象)
//
// 三个异步/同步模型都吃 base64 data: URI（DashScope 自动转存 OSS），
// 无需站内托管上传文件。
var ModelList = []string{
	"qwen3-asr-flash",
	"fun-asr",
	"qwen3-asr-flash-filetrans",
}

var ChannelName = "ali_asr"

// qwen3-asr-flash：DashScope multimodal-generation 同步识别接口。
const mmGenerationPath = "/api/v1/services/aigc/multimodal-generation/generation"

// fun-asr / qwen3-asr-flash-filetrans：DashScope 异步录音转写接口。
const transcriptionPath = "/api/v1/services/audio/asr/transcription"
const tasksPathPrefix = "/api/v1/tasks/"

// 走异步转写任务的模型集合（其余默认走 multimodal-generation 同步）。
var asyncModels = map[string]bool{
	"fun-asr":                   true,
	"qwen3-asr-flash-filetrans": true,
}

func isAsyncModel(model string) bool { return asyncModels[model] }

// isSingularFileURLModel 区分异步提交体：qwen3-asr-flash-filetrans 用
// input.file_url(单数字符串) + 结果 output.result.transcription_url；
// fun-asr 用 input.file_urls(数组) + 结果 output.results[].transcription_url。
func isSingularFileURLModel(model string) bool {
	return model == "qwen3-asr-flash-filetrans"
}
