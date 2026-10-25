package claude

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"eflow.local/new-api/dto"
	relaycommon "eflow.local/new-api/relay/common"
	"eflow.local/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestClaudeStreamRecordsWebSearchRequests(t *testing.T) {
	gin.SetMode(gin.TestMode)
	// The count is recorded before the per-format branch, so the Claude format
	// exercises the same code the OpenAI conversion path runs.
	for _, format := range []types.RelayFormat{types.RelayFormatClaude} {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
		info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{}, RelayFormat: format, IsStream: true}
		claudeInfo := &ClaudeResponseInfo{Usage: &dto.Usage{}}

		// message_delta carries the cumulative count; a later delta replaces it.
		for _, data := range []string{
			`{"type":"message_delta","delta":{"stop_reason":null},"usage":{"output_tokens":5,"server_tool_use":{"web_search_requests":1}}}`,
			`{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":9,"server_tool_use":{"web_search_requests":3}}}`,
		} {
			require.Nil(t, HandleStreamResponseData(c, info, claudeInfo, nil, data), format)
		}
		require.Equal(t, 3, c.GetInt("claude_web_search_requests"), format)
	}
}
