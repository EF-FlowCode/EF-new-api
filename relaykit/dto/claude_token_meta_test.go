package dto

import (
	"strings"
	"testing"

	kitutil "eflow.local/new-api/relaykit/relayconvert/kitutil"
	"eflow.local/new-api/relaykit/types"
	"github.com/stretchr/testify/require"
)

// tool_result 里的截图 base64 不能当文本计数，否则 Claude Code 会话会被估成数百万 token。
func TestClaudeTokenCountMetaToolResultImageNotCountedAsText(t *testing.T) {
	b64 := strings.Repeat("iVBORw0KGgoAAAANSUhEUgAAA8AAAAQ4CAIAAABnsVYU", 20000)
	body := `{"model":"claude-opus-5-5","max_tokens":1024,"messages":[
		{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":[
			{"type":"text","text":"screenshot taken"},
			{"type":"image","source":{"type":"base64","media_type":"image/png","data":"` + b64 + `"}}
		]}]},
		{"role":"user","content":[{"type":"tool_result","tool_use_id":"t2","content":"plain result"}]},
		{"role":"user","content":[{"type":"document","source":{"type":"base64","media_type":"application/pdf","data":"JVBERi0xLjQK"}}]}
	]}`
	var req ClaudeRequest
	require.NoError(t, kitutil.Unmarshal([]byte(body), &req))

	meta := req.GetTokenCountMeta()

	require.NotContains(t, meta.CombineText, "iVBORw0KGgo")
	require.Contains(t, meta.CombineText, "screenshot taken")
	require.Contains(t, meta.CombineText, "plain result")
	require.Less(t, len(meta.CombineText), 1000)
	require.Len(t, meta.Files, 2)
	require.Equal(t, types.FileTypeImage, meta.Files[0].FileType)
	require.Equal(t, types.FileTypeFile, meta.Files[1].FileType)
}
