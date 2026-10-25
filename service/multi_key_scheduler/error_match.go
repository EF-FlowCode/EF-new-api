package multi_key_scheduler

import (
	"net/http"
	"strings"
)

// rateLimitMessageHints 是已知撞限文案的关键词列表（不区分大小写匹配）。
// 仓库内尚未 grep 到 dashscope 的真实撞限报文，先按通用文案预填，
// 灰度期日志反查后再补全 — 见 tmp/scheduler-handoff.md §4 的待补章节。
var rateLimitMessageHints = []string{
	"throttling.ratequota",
	"throttling.allocationquota",
	"requests rate limit exceeded",
	"rpm limit",
	"tpm limit",
	"rate_limit_exceeded",
	"request was rate-limited",
	"too many requests",
}

// isRateLimitError 判定一个上游错误是否属于"撞 RPM/TPM"，决定要不要给 (key,model) 写 cool。
//
// 优先级：
//  1. HTTP 429 一律算 — 最强信号，不再看正文（即便 message 是空字符串也命中）。
//  2. 任一关键词匹配 message（小写化对比）。
//
// 不匹配的错误（5xx / 网络异常 / 上游业务错）只释放并发槽位，不写 cool —
// 否则一台上游挂掉时整把 key 全 cool，故障面会从 1 个 key 扩散到全 channel。
func isRateLimitError(statusCode int, message string) bool {
	if statusCode == http.StatusTooManyRequests {
		return true
	}
	if message == "" {
		return false
	}
	lower := strings.ToLower(message)
	for _, hint := range rateLimitMessageHints {
		if strings.Contains(lower, hint) {
			return true
		}
	}
	return false
}
