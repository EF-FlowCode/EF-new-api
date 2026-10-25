package claude

import (
	"net/http"
	"net/http/httptest"
	"testing"

	relaycommon "eflow.local/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestCommonClaudeHeadersOperationForwardsEveryBetaLine(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	c.Request.Header.Add("anthropic-beta", "fast-mode-2026-02-01")
	c.Request.Header.Add("anthropic-beta", "thinking-binding-controls-2026-08-01")

	upstream := http.Header{}
	CommonClaudeHeadersOperation(c, &upstream, &relaycommon.RelayInfo{OriginModelName: "claude-opus-5-5"})
	require.Equal(t, "fast-mode-2026-02-01,thinking-binding-controls-2026-08-01", upstream.Get("anthropic-beta"))
}
