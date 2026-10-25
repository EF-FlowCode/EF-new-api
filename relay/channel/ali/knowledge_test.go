package ali

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"eflow.local/new-api/dto"
	relaycommon "eflow.local/new-api/relay/common"
	relayconstant "eflow.local/new-api/relay/constant"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestKnowledgeAliRerankProtocols(t *testing.T) {
	for _, name := range []string{"qwen3-rerank", "qwen3.7-text-rerank", "gte-rerank-v2", "qwen3-vl-rerank"} {
		t.Run(name, func(t *testing.T) {
			info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ChannelBaseUrl: "https://dashscope.aliyuncs.com", UpstreamModelName: name}, RelayMode: relayconstant.RelayModeRerank}
			url, err := (&Adaptor{}).GetRequestURL(info)
			require.NoError(t, err)
			path := "/api/v1/services/rerank/text-rerank/text-rerank"
			if name == "qwen3-rerank" {
				path = "/compatible-api/v1/reranks"
			}
			require.Equal(t, "https://dashscope.aliyuncs.com"+path, url)
			request, err := (&Adaptor{}).ConvertRerankRequest(nil, relayconstant.RelayModeRerank, dto.RerankRequest{Model: name, Query: "question", Documents: []any{"document"}, Instruct: "rank"})
			require.NoError(t, err)
			if name == "qwen3-rerank" {
				require.IsType(t, dto.RerankRequest{}, request)
			} else {
				require.IsType(t, &AliRerankRequest{}, request)
			}
		})
	}
}

func TestKnowledgeAliRerankUsageAndMultimodal(t *testing.T) {
	for _, body := range []string{
		`{"results":[{"index":0,"relevance_score":0.9}],"usage":{"total_tokens":184}}`,
		`{"output":{"results":[{"index":0,"relevance_score":0.9}]},"usage":{"prompt_tokens":184,"total_tokens":184}}`,
	} {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		err, u := RerankHandler(c, &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}, &relaycommon.RelayInfo{})
		require.Nil(t, err)
		require.Equal(t, 184, u.PromptTokens)
		require.Contains(t, w.Body.String(), "relevance_score")
	}
	request := dto.RerankRequest{Model: "qwen3-vl-rerank", Query: map[string]any{"image": "https://example.com/query.png"}, Documents: []any{map[string]any{"text": "document"}}}
	_, err := (&Adaptor{}).ConvertRerankRequest(nil, relayconstant.RelayModeRerank, request)
	require.NoError(t, err)
	request.Model = "qwen3.7-text-rerank"
	_, err = (&Adaptor{}).ConvertRerankRequest(nil, relayconstant.RelayModeRerank, request)
	require.Error(t, err)
}

func TestKnowledgeAliVisionUsageIsNotDoubleCounted(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "tongyi-embedding-vision-plus"}}
	err, u := MultimodalEmbeddingHandler(c, &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"output":{"embeddings":[{"index":0,"embedding":[0.1]}]},"usage":{"input_tokens":100,"image_tokens":70}}`))}, info)
	require.Nil(t, err)
	require.Equal(t, 100, u.PromptTokens)
	require.Equal(t, 70, u.PromptTokensDetails.ImageTokens)
}

func TestKnowledgeAliMultimodalParameters(t *testing.T) {
	dimension, fps, fusion := 1024, 0.5, true
	r, err := ConvertMultimodalEmbeddingRequest(dto.EmbeddingRequest{Model: "qwen3-vl-embedding", Input: []any{map[string]any{"text": "query"}, map[string]any{"image": "https://example.com/image.png"}}, Dimensions: &dimension, FPS: &fps, EnableFusion: &fusion, Instruct: "Retrieve images"})
	require.NoError(t, err)
	require.Equal(t, 1024, *r.Parameters.Dimension)
	require.True(t, *r.Parameters.EnableFusion)
	require.Equal(t, "Retrieve images", r.Parameters.Instruct)
	_, err = ConvertMultimodalEmbeddingRequest(dto.EmbeddingRequest{Model: "tongyi-embedding-vision-plus", Input: "text", Dimensions: &dimension})
	require.Error(t, err)
}
