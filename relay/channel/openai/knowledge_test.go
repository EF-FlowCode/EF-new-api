package openai

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	relaycommon "eflow.local/new-api/relay/common"
	relayconstant "eflow.local/new-api/relay/constant"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestKnowledgeEmbeddingPreservesTotalOnlyAndZero(t *testing.T) {
	for _, tc := range []struct {
		usage string
		want  int
	}{{`{"total_tokens":184}`, 184}, {`{"prompt_tokens":0,"total_tokens":0}`, 0}} {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("POST", "/v1/embeddings", nil)
		info := &relaycommon.RelayInfo{RelayMode: relayconstant.RelayModeEmbeddings, ChannelMeta: &relaycommon.ChannelMeta{}}
		body := `{"data":[{"index":0,"embedding":[0.1]}],"usage":` + tc.usage + `}`
		u, err := OpenaiHandler(c, info, &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))})
		require.Nil(t, err)
		require.Equal(t, tc.want, u.PromptTokens)
		require.Equal(t, tc.want, u.TotalTokens)
		require.Contains(t, w.Body.String(), "embedding")
	}
}
