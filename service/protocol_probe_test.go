package service

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strconv"
	"testing"

	"eflow.local/new-api/common"
	"eflow.local/new-api/constant"
	"eflow.local/new-api/dto"
	"eflow.local/new-api/model"
	"eflow.local/new-api/setting/model_setting"
	"github.com/stretchr/testify/require"
)

var probeQuestionPattern = regexp.MustCompile(`Compute (\d+)\*(\d+)`)

// newProbeUpstream serves native Messages and Chat, has no Responses endpoint,
// and omits usage from the Chat stream.
func newProbeUpstream(t *testing.T) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		var body map[string]any
		require.NoError(t, common.Unmarshal(raw, &body))
		require.Equal(t, "probe-upstream-model", body["model"])
		stream, _ := body["stream"].(bool)
		answer := ""
		if match := probeQuestionPattern.FindStringSubmatch(string(raw)); len(match) == 3 {
			a, _ := strconv.Atoi(match[1])
			b, _ := strconv.Atoi(match[2])
			answer = strconv.Itoa(a * b)
		}
		switch r.URL.Path {
		case "/v1/messages":
			require.Equal(t, "sk-probe", r.Header.Get("x-api-key"))
			if stream {
				w.Header().Set("Content-Type", "text/event-stream")
				fmt.Fprint(w, "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":12,\"output_tokens\":0}}}\n\n")
				fmt.Fprintf(w, "event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"%s\"}}\n\n", answer)
				fmt.Fprint(w, "event: message_delta\ndata: {\"type\":\"message_delta\",\"usage\":{\"output_tokens\":3}}\n\n")
				fmt.Fprint(w, "event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")
				return
			}
			fmt.Fprintf(w, `{"type":"message","content":[{"type":"text","text":"%s"}],"usage":{"input_tokens":12,"output_tokens":3}}`, answer)
		case "/v1/chat/completions":
			require.Equal(t, "Bearer sk-probe", r.Header.Get("Authorization"))
			if stream {
				w.Header().Set("Content-Type", "text/event-stream")
				fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"content\":\"%s\"}}]}\n\n", answer)
				fmt.Fprint(w, "data: [DONE]\n\n")
				return
			}
			fmt.Fprintf(w, `{"object":"chat.completion","choices":[{"message":{"content":"%s"}}],"usage":{"prompt_tokens":12,"completion_tokens":3}}`, answer)
		default:
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprintf(w, `{"error":{"message":"Invalid URL (POST %s)"}}`, r.URL.Path)
		}
	}))
}

func TestProbeChannelProtocols(t *testing.T) {
	upstream := newProbeUpstream(t)
	defer upstream.Close()

	baseURL := upstream.URL
	mapping := `{"probe-model":"probe-upstream-model"}`
	channel := &model.Channel{
		Type:         constant.ChannelTypeOpenAI,
		Key:          "sk-probe",
		BaseURL:      &baseURL,
		Models:       "probe-model",
		ModelMapping: &mapping,
		Status:       1,
	}
	result, err := ProbeChannelProtocols(context.Background(), channel, "")
	require.NoError(t, err)
	require.Equal(t, "probe-upstream-model", result.Model)

	verdicts := map[string]dto.UpstreamProtocolProbeItem{}
	for _, item := range result.Items {
		verdicts[item.Protocol] = item
	}
	require.Equal(t, dto.UpstreamProtocolVerdictSupported, verdicts["messages"].Verdict)
	require.Empty(t, verdicts["messages"].Reason)
	require.True(t, verdicts["messages"].Attempts[0].AnswerOK)

	// Non-stream works, stream lacks usage: not confirmed as supported.
	require.Equal(t, dto.UpstreamProtocolVerdictUnknown, verdicts["chat"].Verdict)
	require.Equal(t, "stream_failed", verdicts["chat"].Reason)
	require.Contains(t, verdicts["chat"].Attempts[1].Error, "用量")

	require.Equal(t, dto.UpstreamProtocolVerdictUnsupported, verdicts["responses"].Verdict)
	require.Equal(t, "endpoint_missing", verdicts["responses"].Reason)
}

func TestProbeAnswerMatchesDigitGrouping(t *testing.T) {
	require.True(t, probeAnswerMatches("The product is 1,234,567.", "1234567"))
	require.True(t, probeAnswerMatches("1 234 567", "1234567"))
	require.True(t, probeAnswerMatches("**1234567**", "1234567"))
	require.False(t, probeAnswerMatches("1,234,568", "1234567"))
}

func TestJudgeProtocolProbeAmbiguousResultsStayUnknown(t *testing.T) {
	routing := model_setting.DefaultProtocolRoutingSettings()
	item := judgeProtocolProbe("messages", []dto.UpstreamProtocolProbeAttempt{
		{StatusCode: http.StatusForbidden, ClientRestricted: true, Error: "请使用标准 Claude Code 客户端请求"},
		{Stream: true, StatusCode: http.StatusForbidden, ClientRestricted: true},
	}, routing)
	require.Equal(t, dto.UpstreamProtocolVerdictUnknown, item.Verdict)
	require.Equal(t, "client_restricted", item.Reason)

	item = judgeProtocolProbe("chat", []dto.UpstreamProtocolProbeAttempt{
		{StatusCode: http.StatusNotFound, Error: `{"error":{"code":"model_not_found","message":"The model does not exist"}}`},
	}, routing)
	require.Equal(t, dto.UpstreamProtocolVerdictUnknown, item.Verdict)
	require.Equal(t, "model_not_found", item.Reason)

	item = judgeProtocolProbe("responses", []dto.UpstreamProtocolProbeAttempt{
		{StatusCode: http.StatusInternalServerError, Error: "field messages is required"},
	}, routing)
	require.Equal(t, dto.UpstreamProtocolVerdictUnknown, item.Verdict)
}
