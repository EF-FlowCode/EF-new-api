package service

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"

	"eflow.local/new-api/common"
	"eflow.local/new-api/constant"
	"eflow.local/new-api/dto"
	"eflow.local/new-api/model"
	"eflow.local/new-api/setting/model_setting"
)

// eflow: protocol probe. The probe calls
// the upstream directly with the channel's own credentials and reports, per
// protocol, whether it is natively supported. It never changes routing: the
// administrator confirms the protocol set separately.

const (
	protocolProbeTimeout    = 90 * time.Second
	protocolProbeMaxTokens  = 256
	protocolProbeMaxBody    = 1 << 20
	protocolProbeErrPreview = 300
	// ProtocolTrafficWindow is the lookback for the native-traffic reference.
	ProtocolTrafficWindow = 7 * 24 * time.Hour
)

type protocolProbeTarget struct {
	baseURL string
	apiKey  string
	model   string
	headers map[string]string
	client  *http.Client
}

type protocolProbeQuestion struct {
	prompt string
	answer string
}

// ProbeChannelProtocols probes messages, chat and responses on the channel,
// each with one non-stream and one stream request.
func ProbeChannelProtocols(ctx context.Context, channel *model.Channel, requestedModel string) (*dto.UpstreamProtocolProbeResult, error) {
	if channel == nil {
		return nil, fmt.Errorf("channel is nil")
	}
	if !constant.SupportsProtocolRouting(channel.Type) {
		return nil, fmt.Errorf("渠道类型 %d 不支持协议探测", channel.Type)
	}
	target, err := newProtocolProbeTarget(channel, requestedModel)
	if err != nil {
		return nil, err
	}
	routing := model_setting.GetGlobalSettings().ProtocolRouting
	protocols := []string{constant.UpstreamProtocolMessages, constant.UpstreamProtocolChat, constant.UpstreamProtocolResponses}
	items := make([]dto.UpstreamProtocolProbeItem, len(protocols))
	var wg sync.WaitGroup
	for i, protocol := range protocols {
		wg.Add(1)
		go func(i int, protocol string) {
			defer wg.Done()
			attempts := make([]dto.UpstreamProtocolProbeAttempt, 0, 2)
			for _, stream := range []bool{false, true} {
				attempts = append(attempts, probeProtocolOnce(ctx, target, protocol, stream, routing))
			}
			items[i] = judgeProtocolProbe(protocol, attempts, routing)
		}(i, protocol)
	}
	wg.Wait()

	traffic := CountNativeProtocolTraffic(channel.Id, time.Now().Add(-ProtocolTrafficWindow))
	for i := range items {
		items[i].TrafficCount = traffic[items[i].Protocol]
	}
	return &dto.UpstreamProtocolProbeResult{
		ProbedAt: time.Now().Unix(),
		Model:    target.model,
		Items:    items,
	}, nil
}

func newProtocolProbeTarget(channel *model.Channel, requestedModel string) (*protocolProbeTarget, error) {
	modelName := strings.TrimSpace(requestedModel)
	if modelName == "" && channel.TestModel != nil {
		modelName = strings.TrimSpace(*channel.TestModel)
	}
	if modelName == "" {
		if models := channel.GetModels(); len(models) > 0 {
			modelName = strings.TrimSpace(models[0])
		}
	}
	if modelName == "" {
		return nil, fmt.Errorf("渠道没有可用于探测的模型")
	}
	if mapping := channel.GetModelMapping(); mapping != "" && mapping != "{}" {
		var modelMap map[string]string
		if err := common.UnmarshalJsonStr(mapping, &modelMap); err == nil {
			if mapped := strings.TrimSpace(modelMap[modelName]); mapped != "" {
				modelName = mapped
			}
		}
	}

	baseURL := strings.TrimRight(channel.GetBaseURL(), "/")
	if baseURL == "" {
		baseURL = strings.TrimRight(constant.GetChannelBaseURL(channel.Type), "/")
	}
	if baseURL == "" {
		return nil, fmt.Errorf("渠道没有 Base URL")
	}
	apiKey, _, keyErr := channel.GetNextEnabledKey()
	if keyErr != nil {
		return nil, keyErr
	}
	setting := channel.GetSetting()
	client, err := GetHttpClientWithProxySettings(setting.Proxy, setting)
	if err != nil {
		return nil, err
	}
	headers := make(map[string]string)
	for name, value := range channel.GetHeaderOverride() {
		if text, ok := value.(string); ok && name != "*" {
			headers[name] = strings.ReplaceAll(text, "{api_key}", apiKey)
		}
	}
	return &protocolProbeTarget{baseURL: baseURL, apiKey: apiKey, model: modelName, headers: headers, client: client}, nil
}

func newProtocolProbeQuestion() protocolProbeQuestion {
	a := randomInt(100, 999)
	b := randomInt(100, 999)
	return protocolProbeQuestion{
		prompt: fmt.Sprintf("Compute %d*%d. Reply with only the number.", a, b),
		answer: fmt.Sprintf("%d", a*b),
	}
}

func randomInt(low, high int64) int64 {
	n, err := rand.Int(rand.Reader, big.NewInt(high-low+1))
	if err != nil {
		return low
	}
	return low + n.Int64()
}

// RandomProbeNonce returns a short random string for answer checks.
func RandomProbeNonce() string {
	buf := make([]byte, 4)
	_, _ = rand.Read(buf)
	return hex.EncodeToString(buf)
}

func probeProtocolOnce(ctx context.Context, target *protocolProbeTarget, protocol string, stream bool, routing model_setting.ProtocolRoutingSettings) dto.UpstreamProtocolProbeAttempt {
	question := newProtocolProbeQuestion()
	attempt := dto.UpstreamProtocolProbeAttempt{Stream: stream}
	path, body, headers := buildProtocolProbeRequest(target, protocol, stream, question.prompt)

	reqCtx, cancel := context.WithTimeout(ctx, protocolProbeTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, target.baseURL+path, bytes.NewReader(body))
	if err != nil {
		attempt.Error = err.Error()
		return attempt
	}
	req.Header.Set("Content-Type", "application/json")
	for name, value := range headers {
		req.Header.Set(name, value)
	}
	for name, value := range target.headers {
		req.Header.Set(name, value)
	}

	started := time.Now()
	resp, err := target.client.Do(req)
	if err != nil {
		attempt.DurationMs = time.Since(started).Milliseconds()
		attempt.Error = previewProbeText(err.Error())
		return attempt
	}
	defer resp.Body.Close()
	attempt.StatusCode = resp.StatusCode
	raw, readErr := io.ReadAll(io.LimitReader(resp.Body, protocolProbeMaxBody))
	attempt.DurationMs = time.Since(started).Milliseconds()
	if readErr != nil {
		attempt.Error = previewProbeText(readErr.Error())
		return attempt
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		attempt.Error = previewProbeText(string(raw))
		// Client-only upstreams answer 403 or 400 (e.g. "client anomaly").
		attempt.ClientRestricted = resp.StatusCode >= 400 && resp.StatusCode < 500 && IsClientRestrictedMessage(string(raw), routing)
		return attempt
	}

	var parsed protocolProbeParsed
	if stream {
		parsed = parseProtocolProbeStream(protocol, raw)
	} else {
		parsed = parseProtocolProbeBody(protocol, raw)
	}
	if parsed.err != "" {
		attempt.Error = parsed.err
		return attempt
	}
	attempt.OK = true
	attempt.AnswerOK = probeAnswerMatches(parsed.text, question.answer)
	return attempt
}

// probeAnswerMatches accepts the product written with digit grouping
// ("1,234,567", "1 234 567").
var probeDigitSeparators = strings.NewReplacer(",", "", "\uff0c", "", " ", "", "\u00a0", "", "\u202f", "", "_", "")

func probeAnswerMatches(text string, answer string) bool {
	return strings.Contains(probeDigitSeparators.Replace(text), answer)
}

func buildProtocolProbeRequest(target *protocolProbeTarget, protocol string, stream bool, prompt string) (string, []byte, map[string]string) {
	switch protocol {
	case constant.UpstreamProtocolMessages:
		body := map[string]any{
			"model":      target.model,
			"max_tokens": protocolProbeMaxTokens,
			"messages":   []map[string]any{{"role": "user", "content": prompt}},
		}
		if stream {
			body["stream"] = true
		}
		data, _ := common.Marshal(body)
		return "/v1/messages", data, map[string]string{"x-api-key": target.apiKey, "anthropic-version": "2023-06-01"}
	case constant.UpstreamProtocolResponses:
		body := map[string]any{
			"model":             target.model,
			"max_output_tokens": protocolProbeMaxTokens,
			"input":             prompt,
		}
		if stream {
			body["stream"] = true
		}
		data, _ := common.Marshal(body)
		return "/v1/responses", data, map[string]string{"Authorization": "Bearer " + target.apiKey}
	default:
		body := map[string]any{
			"model":      target.model,
			"max_tokens": protocolProbeMaxTokens,
			"messages":   []map[string]any{{"role": "user", "content": prompt}},
		}
		if stream {
			body["stream"] = true
			body["stream_options"] = map[string]any{"include_usage": true}
		}
		data, _ := common.Marshal(body)
		return "/v1/chat/completions", data, map[string]string{"Authorization": "Bearer " + target.apiKey}
	}
}

type protocolProbeParsed struct {
	text string
	err  string
}

func parseProtocolProbeBody(protocol string, raw []byte) protocolProbeParsed {
	var body map[string]any
	if err := common.Unmarshal(raw, &body); err != nil {
		return protocolProbeParsed{err: "响应不是 JSON：" + previewProbeText(string(raw))}
	}
	usage, _ := body["usage"].(map[string]any)
	switch protocol {
	case constant.UpstreamProtocolMessages:
		if body["type"] != "message" {
			return protocolProbeParsed{err: "响应不是 Messages 格式：" + previewProbeText(string(raw))}
		}
		if _, ok := usage["input_tokens"]; !ok {
			return protocolProbeParsed{err: "响应缺少 usage.input_tokens"}
		}
		var text strings.Builder
		if content, ok := body["content"].([]any); ok {
			for _, part := range content {
				if block, ok := part.(map[string]any); ok {
					if s, ok := block["text"].(string); ok {
						text.WriteString(s)
					}
				}
			}
		}
		return protocolProbeParsed{text: text.String()}
	case constant.UpstreamProtocolResponses:
		if body["object"] != "response" {
			return protocolProbeParsed{err: "响应不是 Responses 格式：" + previewProbeText(string(raw))}
		}
		if _, ok := usage["input_tokens"]; !ok {
			return protocolProbeParsed{err: "响应缺少 usage.input_tokens"}
		}
		return protocolProbeParsed{text: responsesOutputText(body)}
	default:
		choices, ok := body["choices"].([]any)
		if !ok || len(choices) == 0 {
			return protocolProbeParsed{err: "响应不是 Chat Completions 格式：" + previewProbeText(string(raw))}
		}
		if _, ok := usage["prompt_tokens"]; !ok {
			return protocolProbeParsed{err: "响应缺少 usage.prompt_tokens"}
		}
		text := ""
		if choice, ok := choices[0].(map[string]any); ok {
			if message, ok := choice["message"].(map[string]any); ok {
				text, _ = message["content"].(string)
			}
		}
		return protocolProbeParsed{text: text}
	}
}

func responsesOutputText(response map[string]any) string {
	var text strings.Builder
	output, _ := response["output"].([]any)
	for _, item := range output {
		entry, ok := item.(map[string]any)
		if !ok {
			continue
		}
		content, _ := entry["content"].([]any)
		for _, part := range content {
			if block, ok := part.(map[string]any); ok {
				if s, ok := block["text"].(string); ok {
					text.WriteString(s)
				}
			}
		}
	}
	return text.String()
}

func parseProtocolProbeStream(protocol string, raw []byte) protocolProbeParsed {
	var text strings.Builder
	sawStart, sawEnd, sawUsage := false, false, false
	scanner := bufio.NewScanner(bytes.NewReader(raw))
	scanner.Buffer(make([]byte, 64*1024), protocolProbeMaxBody)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			sawEnd = true
			continue
		}
		var event map[string]any
		if err := common.UnmarshalJsonStr(data, &event); err != nil {
			continue
		}
		switch protocol {
		case constant.UpstreamProtocolMessages:
			switch event["type"] {
			case "message_start":
				sawStart = true
				if message, ok := event["message"].(map[string]any); ok {
					if usage, ok := message["usage"].(map[string]any); ok {
						_, sawUsage = usage["input_tokens"]
					}
				}
			case "content_block_delta":
				if delta, ok := event["delta"].(map[string]any); ok {
					if s, ok := delta["text"].(string); ok {
						text.WriteString(s)
					}
				}
			case "message_delta", "message_stop":
				sawEnd = true
			}
		case constant.UpstreamProtocolResponses:
			switch event["type"] {
			case "response.created":
				sawStart = true
			case "response.output_text.delta":
				if s, ok := event["delta"].(string); ok {
					text.WriteString(s)
				}
			case "response.completed":
				sawEnd = true
				if response, ok := event["response"].(map[string]any); ok {
					if usage, ok := response["usage"].(map[string]any); ok {
						_, sawUsage = usage["input_tokens"]
					}
					if text.Len() == 0 {
						text.WriteString(responsesOutputText(response))
					}
				}
			}
		default:
			if choices, ok := event["choices"].([]any); ok {
				sawStart = true
				for _, item := range choices {
					choice, _ := item.(map[string]any)
					if delta, ok := choice["delta"].(map[string]any); ok {
						if s, ok := delta["content"].(string); ok {
							text.WriteString(s)
						}
					}
				}
			}
			if usage, ok := event["usage"].(map[string]any); ok {
				if _, ok := usage["prompt_tokens"]; ok {
					sawUsage = true
				}
			}
		}
	}
	switch {
	case !sawStart:
		return protocolProbeParsed{err: "流式响应格式不符：" + previewProbeText(string(raw))}
	case !sawEnd:
		return protocolProbeParsed{err: "流式响应没有正常结束"}
	case !sawUsage:
		return protocolProbeParsed{err: "流式响应没有返回用量"}
	}
	return protocolProbeParsed{text: text.String()}
}

// judgeProtocolProbe turns the two attempts into one verdict. Only a clear
// endpoint/protocol rejection counts as unsupported; anything ambiguous
// (auth, client whitelist, rate limit, upstream errors) stays unknown.
func judgeProtocolProbe(protocol string, attempts []dto.UpstreamProtocolProbeAttempt, routing model_setting.ProtocolRoutingSettings) dto.UpstreamProtocolProbeItem {
	item := dto.UpstreamProtocolProbeItem{Protocol: protocol, Attempts: attempts}
	allOK := len(attempts) > 0
	for _, attempt := range attempts {
		allOK = allOK && attempt.OK
	}
	if allOK {
		item.Verdict = dto.UpstreamProtocolVerdictSupported
		for _, attempt := range attempts {
			if !attempt.AnswerOK {
				item.Reason = "answer_mismatch"
			}
		}
		return item
	}
	for _, attempt := range attempts {
		if attempt.ClientRestricted {
			item.Verdict = dto.UpstreamProtocolVerdictUnknown
			item.Reason = "client_restricted"
			return item
		}
	}
	for _, attempt := range attempts {
		if attempt.OK {
			continue
		}
		message := strings.ToLower(attempt.Error)
		switch attempt.StatusCode {
		case http.StatusNotFound:
			if strings.Contains(message, "model") {
				item.Verdict = dto.UpstreamProtocolVerdictUnknown
				item.Reason = "model_not_found"
				return item
			}
			item.Verdict = dto.UpstreamProtocolVerdictUnsupported
			item.Reason = "endpoint_missing"
			return item
		case http.StatusMethodNotAllowed, http.StatusNotImplemented:
			item.Verdict = dto.UpstreamProtocolVerdictUnsupported
			item.Reason = "endpoint_missing"
			return item
		case http.StatusBadRequest:
			if containsAnyKeyword(message, routing.FormatErrorKeywords) {
				item.Verdict = dto.UpstreamProtocolVerdictUnsupported
				item.Reason = "format_error"
				return item
			}
		}
	}
	item.Verdict = dto.UpstreamProtocolVerdictUnknown
	if len(attempts) == 2 && attempts[0].OK && !attempts[1].OK {
		item.Reason = "stream_failed"
	} else {
		item.Reason = "inconclusive"
	}
	return item
}

func previewProbeText(text string) string {
	text = strings.TrimSpace(text)
	runes := []rune(text)
	if len(runes) > protocolProbeErrPreview {
		return string(runes[:protocolProbeErrPreview]) + "…"
	}
	return text
}

// CountNativeProtocolTraffic counts successful single-step (unconverted)
// requests per governed protocol on the channel since the given time. It is
// reference information only and returns an empty map when unavailable.
func CountNativeProtocolTraffic(channelID int, since time.Time) map[string]int64 {
	counts := make(map[string]int64)
	if model.LOG_DB == nil {
		return counts
	}
	var minID int64
	if err := model.LOG_DB.Raw("SELECT COALESCE(MIN(id), 0) FROM logs WHERE created_at >= ?", since.Unix()).Scan(&minID).Error; err != nil || minID == 0 {
		return counts
	}
	type row struct {
		Path  string
		Count int64
	}
	var rows []row
	err := model.LOG_DB.Raw(`SELECT JSON_UNQUOTE(JSON_EXTRACT(other, '$.request_path')) AS path, COUNT(*) AS count
		FROM logs
		WHERE channel_id = ? AND id >= ? AND type = ?
		  AND JSON_VALID(other)
		  AND JSON_LENGTH(JSON_EXTRACT(other, '$.request_conversion')) = 1
		GROUP BY path`, channelID, minID, model.LogTypeConsume).Scan(&rows).Error
	if err != nil {
		common.SysLog(fmt.Sprintf("protocol probe: count native traffic for channel %d failed: %v", channelID, err))
		return counts
	}
	for _, r := range rows {
		if protocol := constant.RequestPathUpstreamProtocol(strings.TrimSpace(r.Path)); protocol != "" {
			counts[protocol] += r.Count
		}
	}
	return counts
}
