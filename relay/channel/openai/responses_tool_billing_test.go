package openai

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"eflow.local/new-api/constant"
	"eflow.local/new-api/dto"
	relaycommon "eflow.local/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func responsesToolInfo(tools ...string) *relaycommon.RelayInfo {
	info := &relaycommon.RelayInfo{
		ChannelMeta:        &relaycommon.ChannelMeta{UpstreamModelName: "gpt-test"},
		ResponsesUsageInfo: &relaycommon.ResponsesUsageInfo{BuiltInTools: map[string]*relaycommon.BuildInToolInfo{}},
		DisablePing:        true,
	}
	for _, tool := range tools {
		info.ResponsesUsageInfo.BuiltInTools[tool] = &relaycommon.BuildInToolInfo{ToolName: tool}
	}
	return info
}

func TestResponsesNonStreamCountsActualBuiltInToolCalls(t *testing.T) {
	gin.SetMode(gin.TestMode)
	// Declared web_search and file_search; the model searched the web twice and
	// never used file_search. Counting response.tools used to bill 1 + 1.
	body := `{"id":"resp_1","status":"completed","tools":[{"type":"web_search"},{"type":"file_search"}],` +
		`"output":[{"type":"web_search_call","id":"ws_1"},{"type":"web_search_call","id":"ws_2"},{"type":"message","content":[{"type":"output_text","text":"ok"}]}],` +
		`"usage":{"input_tokens":3,"output_tokens":2,"total_tokens":5}}`
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	info := responsesToolInfo(dto.BuildInToolWebSearch, dto.BuildInToolFileSearch)

	_, apiErr := OaiResponsesHandler(c, info, &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))})
	require.Nil(t, apiErr)
	require.Equal(t, 2, info.ResponsesUsageInfo.BuiltInTools[dto.BuildInToolWebSearch].CallCount)
	require.Equal(t, 0, info.ResponsesUsageInfo.BuiltInTools[dto.BuildInToolFileSearch].CallCount)
}

func TestResponsesStreamCountsGAWebSearchAndFileSearchCalls(t *testing.T) {
	gin.SetMode(gin.TestMode)
	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 5
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })

	body := strings.Join([]string{
		`data: {"type":"response.output_item.done","item":{"type":"web_search_call","id":"ws_1"}}`, ``,
		`data: {"type":"response.output_item.done","item":{"type":"file_search_call","id":"fs_1"}}`, ``,
		`data: {"type":"response.completed","response":{"usage":{"input_tokens":3,"output_tokens":2,"total_tokens":5}}}`, ``,
	}, "\n")
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	info := responsesToolInfo(dto.BuildInToolWebSearch, dto.BuildInToolFileSearch)
	info.IsStream = true

	_, apiErr := OaiResponsesStreamHandler(c, info, &http.Response{Body: io.NopCloser(strings.NewReader(body))})
	require.Nil(t, apiErr)
	require.Equal(t, 1, info.ResponsesUsageInfo.BuiltInTools[dto.BuildInToolWebSearch].CallCount)
	require.Equal(t, 1, info.ResponsesUsageInfo.BuiltInTools[dto.BuildInToolFileSearch].CallCount)
}
