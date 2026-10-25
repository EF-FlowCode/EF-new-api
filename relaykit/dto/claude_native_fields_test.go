package dto

import (
	"encoding/json"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestClaudeNativeFieldsRoundTrip(t *testing.T) {
	input := `{"model":"claude-opus-5-5","max_tokens":64,"safeguards":{"auto_mode":true,"future_id":9007199254740993},"output_config":{"effort":"medium"},"messages":[{"role":"user","content":"summary"},{"role":"system","content":[],"output_config":{"effort":"high"}}]}`
	var req ClaudeRequest
	require.NoError(t, json.Unmarshal([]byte(input), &req))
	encoded, err := json.Marshal(req)
	require.NoError(t, err)
	var got struct {
		Safeguards json.RawMessage `json:"safeguards"`
		Messages   []struct {
			OutputConfig json.RawMessage `json:"output_config"`
		} `json:"messages"`
	}
	require.NoError(t, json.Unmarshal(encoded, &got))
	require.Contains(t, string(got.Safeguards), "9007199254740993")
	require.Empty(t, got.Messages[0].OutputConfig)
	require.JSONEq(t, `{"effort":"high"}`, string(got.Messages[1].OutputConfig))
}
