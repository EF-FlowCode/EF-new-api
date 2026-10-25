package openai

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"eflow.local/new-api/constant"
	"eflow.local/new-api/dto"
	relaycommon "eflow.local/new-api/relay/common"
	"github.com/gin-gonic/gin"
)

type responsesFragmentReader struct{ io.Reader }

func (r responsesFragmentReader) Read(p []byte) (int, error) {
	if len(p) > 1 {
		p = p[:1]
	}
	return r.Reader.Read(p)
}

type responsesCutReader struct{}

func (responsesCutReader) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }

func TestResponsesStreamContract(t *testing.T) {
	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 5
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })
	delta := "data: {\"type\":\"response.output_text.delta\",\"delta\":\".\"}\n\n"
	complete := "data: {\"type\":\"response.completed\",\"response\":{\"usage\":{\"input_tokens\":100,\"output_tokens\":50,\"total_tokens\":150,\"input_tokens_details\":{\"cached_tokens\":80}}}}\n\n"
	for _, tc := range []struct {
		name, body                 string
		fragmented, cut, wantError bool
		input, output, cached      int
	}{
		{name: "complete_eof", body: delta + complete, input: 100, output: 50, cached: 80},
		{name: "complete_done", body: delta + complete + "data: [DONE]\n\n", input: 100, output: 50, cached: 80},
		{name: "one_byte_reads", body: delta + complete, fragmented: true, input: 100, output: 50, cached: 80},
		{name: "failed", body: delta + "data: {\"type\":\"response.failed\",\"response\":{\"error\":{\"type\":\"server_error\",\"message\":\"failed\"}}}\n\n", wantError: true},
		{name: "top_error", body: "data: {\"type\":\"error\",\"code\":\"server_error\",\"message\":\"failed\"}\n\n", wantError: true},
		{name: "response_error", body: delta + "data: {\"type\":\"response.error\"}\n\n", wantError: true},
		{name: "eof_without_terminal", body: delta, wantError: true},
		{name: "done_without_terminal", body: delta + "data: [DONE]\n\n", wantError: true},
		{name: "cut_without_terminal", body: delta, cut: true, wantError: true},
		{name: "explicit_zero", body: delta + "data: {\"type\":\"response.completed\",\"response\":{\"usage\":{\"input_tokens\":0,\"output_tokens\":0,\"total_tokens\":0}}}\n\n"},
		{name: "input_only", body: delta + "data: {\"type\":\"response.completed\",\"response\":{\"usage\":{\"input_tokens\":100,\"output_tokens\":0}}}\n\n", input: 100},
		{name: "output_only", body: delta + "data: {\"type\":\"response.completed\",\"response\":{\"usage\":{\"input_tokens\":0,\"output_tokens\":50}}}\n\n", output: 50},
		{name: "omitted_usage", body: delta + "data: {\"type\":\"response.completed\",\"response\":{}}\n\n", input: 42609, output: 1},
		// 按上游 55a6cd2a4：工具参数、推理与拒答增量，以及只在终态里出现的输出，都参与省略 usage 时的输出估算。
		{name: "omitted_usage_tool_args", body: "data: {\"type\":\"response.function_call_arguments.delta\",\"delta\":\"{}\"}\n\n" + "data: {\"type\":\"response.completed\",\"response\":{}}\n\n", input: 42609, output: 1},
		{name: "omitted_usage_reasoning", body: "data: {\"type\":\"response.reasoning_summary_text.delta\",\"delta\":\".\"}\n\n" + "data: {\"type\":\"response.completed\",\"response\":{}}\n\n", input: 42609, output: 1},
		{name: "omitted_usage_terminal_output", body: "data: {\"type\":\"response.completed\",\"response\":{\"output\":[{\"type\":\"message\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":\".\"}]}]}}\n\n", input: 42609, output: 1},
		{name: "incomplete", body: strings.ReplaceAll(complete, "response.completed", "response.incomplete"), input: 100, output: 50, cached: 80},
		{name: "multiline", body: "data: {\r\ndata: \"type\":\"response.completed\",\r\ndata: \"response\":{\"usage\":{\"input_tokens\":100,\"output_tokens\":50}}\r\ndata: }\r\n\r\n", input: 100, output: 50},
		{name: "multiline_eof", body: "data: {\ndata: \"type\":\"response.completed\",\ndata: \"response\":{\"usage\":{\"input_tokens\":100,\"output_tokens\":50}}\ndata: }", input: 100, output: 50},
		{name: "malformed", body: "data: {bad json}\n\n" + complete, wantError: true},
		{name: "empty_terminal", body: "data: {\"type\":\"response.completed\"}\n\n", wantError: true},
		{name: "contradictory_terminal", body: "data: {\"type\":\"response.completed\",\"response\":{\"status\":\"failed\"}}\n\n", wantError: true},
		{name: "ignore_after_terminal", body: complete + delta + complete, input: 100, output: 50, cached: 80},
	} {
		t.Run(tc.name, func(t *testing.T) {
			writer := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(writer)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
			var reader io.Reader = strings.NewReader(tc.body)
			if tc.cut {
				reader = io.MultiReader(reader, responsesCutReader{})
			}
			if tc.fragmented {
				reader = responsesFragmentReader{reader}
			}
			info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "diagnostic-model"}, IsStream: true, DisablePing: true}
			info.SetEstimatePromptTokens(42609)
			usage, apiErr := OaiResponsesStreamHandler(c, info, &http.Response{Body: io.NopCloser(reader)})
			if info.StreamStatus.ResponseFailed() != tc.wantError {
				t.Fatalf("success-limit outcome disagrees with protocol result: %s", info.StreamStatus.Summary())
			}
			if tc.wantError {
				if apiErr == nil || usage != nil {
					t.Fatalf("failed stream must return API error and no billable usage: usage=%+v error=%v", usage, apiErr)
				}
				if writer.Body.Len() > 0 && !apiErr.IsSkipRetry() {
					t.Fatal("written stream must skip retry")
				}
				if !info.StreamStatus.HasErrors() {
					t.Fatal("protocol failure must be recorded")
				}
				if tc.name == "top_error" && apiErr.ToOpenAIError().Code != "server_error" {
					t.Fatalf("upstream error code lost: %+v", apiErr.ToOpenAIError())
				}
				return
			}
			if apiErr != nil || usage == nil {
				t.Fatalf("usage=%+v error=%v", usage, apiErr)
			}
			if usage.PromptTokens != tc.input || usage.CompletionTokens != tc.output || usage.PromptTokensDetails.CachedTokens != tc.cached {
				t.Fatalf("unexpected usage: %+v", usage)
			}
			// Decode the actual downstream SSE, including multiline data fields.
			scanner := bufio.NewScanner(strings.NewReader(writer.Body.String()))
			var payload []string
			var frames int
			for scanner.Scan() {
				line := scanner.Text()
				if after, ok := strings.CutPrefix(line, "data: "); ok {
					payload = append(payload, after)
				}
				if line == "" && len(payload) > 0 {
					if !json.Valid([]byte(strings.Join(payload, "\n"))) {
						t.Fatalf("invalid downstream SSE: %q", payload)
					}
					frames++
					payload = nil
				}
			}
			if frames == 0 || uint(frames) != uint(info.SendResponseCount) {
				t.Fatalf("frames=%d sent=%d", frames, info.SendResponseCount)
			}
			if tc.name == "ignore_after_terminal" && frames != 1 {
				t.Fatalf("forwarded events after terminal: %d", frames)
			}
		})
	}
}

func TestResponsesStreamTerminalClosesUpstream(t *testing.T) {
	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 5
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	reader, writer := io.Pipe()
	defer writer.Close()
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{}, DisablePing: true,
		ResponsesUsageInfo: &relaycommon.ResponsesUsageInfo{BuiltInTools: map[string]*relaycommon.BuildInToolInfo{
			dto.BuildInToolWebSearchPreview: {},
		}}}
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		usage, apiErr := OaiResponsesStreamHandler(c, info, &http.Response{Body: reader})
		if apiErr != nil || usage == nil || usage.PromptTokens != 10 {
			t.Errorf("usage=%+v error=%v", usage, apiErr)
		}
	}()
	_, err := io.WriteString(writer, "data: {\"type\":\"response.output_item.done\",\"item\":{\"type\":\"web_search_call\"}}\n\ndata: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\",\"usage\":{\"input_tokens\":10,\"output_tokens\":1},\"output\":[{\"type\":\"image_generation_call\",\"quality\":\"high\",\"size\":\"1024x1024\"}]}}\n\n")
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-finished:
	case <-time.After(2 * time.Second):
		writer.Close()
		<-finished
		t.Fatal("terminal event waited for upstream EOF")
	}
	if _, err := writer.Write([]byte("data: ignored\n")); err != io.ErrClosedPipe {
		t.Fatalf("upstream was not closed: %v", err)
	}
	if info.StreamStatus.ResponseFailed() {
		t.Fatal("terminal cleanup must retain successful request outcome")
	}
	if info.ResponsesUsageInfo.BuiltInTools[dto.BuildInToolWebSearchPreview].CallCount != 1 {
		t.Fatal("web search usage lost")
	}
	if !c.GetBool("image_generation_call") || c.GetString("image_generation_call_quality") != "high" {
		t.Fatal("image generation metadata lost")
	}
}

func TestResponsesStreamClientCancellation(t *testing.T) {
	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 5
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil).WithContext(ctx)
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{}, DisablePing: true}
	reader, writer := io.Pipe()
	defer writer.Close()
	usage, apiErr := OaiResponsesStreamHandler(c, info, &http.Response{Body: reader})
	if apiErr != nil || usage == nil {
		t.Fatalf("cancellation policy changed: usage=%+v error=%v", usage, apiErr)
	}
	if info.StreamStatus.EndReason != relaycommon.StreamEndReasonClientGone {
		t.Fatalf("end=%s", info.StreamStatus.EndReason)
	}
}
