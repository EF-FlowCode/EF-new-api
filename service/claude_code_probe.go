package service

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"eflow.local/new-api/common"
	"eflow.local/new-api/model"
)

// eflow: Claude Code native channel test.
// Upstreams that only accept the Claude Code client reject SDK probes, so this
// test runs the real Claude Code CLI in the separate cc-probe service against
// the channel's upstream. The prompt carries a random question whose answer is
// verified, because some upstreams return canned replies to trivial prompts.

const claudeCodeProbeTimeout = 180 * time.Second

type claudeCodeProbeRequest struct {
	BaseURL        string `json:"base_url"`
	APIKey         string `json:"api_key"`
	Model          string `json:"model"`
	Prompt         string `json:"prompt"`
	CustomHeaders  string `json:"custom_headers,omitempty"`
	TimeoutSeconds int    `json:"timeout_seconds"`
}

// ClaudeCodeProbeResult is the cc-probe service response.
type ClaudeCodeProbeResult struct {
	OK         bool           `json:"ok"`
	Result     string         `json:"result"`
	DurationMs int64          `json:"duration_ms"`
	Usage      map[string]any `json:"usage"`
	Version    string         `json:"version"`
	Error      string         `json:"error"`
}

func claudeCodeProbeEndpoint() (string, string) {
	return strings.TrimRight(strings.TrimSpace(common.GetEnvOrDefaultString("CC_PROBE_URL", "")), "/"),
		strings.TrimSpace(common.GetEnvOrDefaultString("CC_PROBE_SECRET", ""))
}

// RunClaudeCodeChannelTest runs one Claude Code request against the channel's
// upstream with the requested model and verifies the answer.
func RunClaudeCodeChannelTest(ctx context.Context, channel *model.Channel, requestedModel string) (*ClaudeCodeProbeResult, error) {
	probeURL, secret := claudeCodeProbeEndpoint()
	if probeURL == "" || secret == "" {
		return nil, fmt.Errorf("未配置 Claude Code 测试服务（CC_PROBE_URL / CC_PROBE_SECRET）")
	}
	target, err := newProtocolProbeTarget(channel, requestedModel)
	if err != nil {
		return nil, err
	}
	a, b := randomInt(100, 999), randomInt(100, 999)
	answer := fmt.Sprintf("%d", a*b)
	nonce := RandomProbeNonce()
	reversed := reverseString(nonce)
	prompt := fmt.Sprintf("Compute %d*%d, then write the string %s reversed. Output only: <number> <reversed>", a, b, nonce)

	payload, err := common.Marshal(claudeCodeProbeRequest{
		BaseURL:        target.baseURL,
		APIKey:         target.apiKey,
		Model:          target.model,
		Prompt:         prompt,
		CustomHeaders:  claudeCodeCustomHeaders(target.headers),
		TimeoutSeconds: int(claudeCodeProbeTimeout/time.Second) - 10,
	})
	if err != nil {
		return nil, err
	}
	reqCtx, cancel := context.WithTimeout(ctx, claudeCodeProbeTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, probeURL+"/run", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Probe-Secret", secret)
	resp, err := (&http.Client{}).Do(req)
	if err != nil {
		return nil, fmt.Errorf("Claude Code 测试服务不可用: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, protocolProbeMaxBody))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Claude Code 测试服务返回 %d: %s", resp.StatusCode, previewProbeText(string(raw)))
	}
	var result ClaudeCodeProbeResult
	if err := common.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("Claude Code 测试服务响应无效: %w", err)
	}
	if !result.OK {
		return &result, fmt.Errorf("Claude Code %s 请求失败: %s", result.Version, previewProbeText(result.Error))
	}
	if !strings.Contains(result.Result, answer) || !strings.Contains(result.Result, reversed) {
		return &result, fmt.Errorf("Claude Code %s 收到的答案不正确，上游可能返回了预设回复: %s", result.Version, previewProbeText(result.Result))
	}
	return &result, nil
}

// claudeCodeCustomHeaders renders static header overrides in the
// ANTHROPIC_CUSTOM_HEADERS format ("Name: value" per line).
func claudeCodeCustomHeaders(headers map[string]string) string {
	if len(headers) == 0 {
		return ""
	}
	names := make([]string, 0, len(headers))
	for name := range headers {
		names = append(names, name)
	}
	sort.Strings(names)
	lines := make([]string, 0, len(names))
	for _, name := range names {
		value := strings.ReplaceAll(headers[name], "\n", " ")
		lines = append(lines, name+": "+value)
	}
	return strings.Join(lines, "\n")
}

func reverseString(value string) string {
	runes := []rune(value)
	for i, j := 0, len(runes)-1; i < j; i, j = i+1, j-1 {
		runes[i], runes[j] = runes[j], runes[i]
	}
	return string(runes)
}
