package dto

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestThinkingRoundTripsBlockBindingAndUnknownFields(t *testing.T) {
	raw := `{"type":"adaptive","display":"summarized","block_binding":{"prefix_mismatch_behavior":"drop_block"},"future_flag":true}`
	var thinking Thinking
	require.NoError(t, json.Unmarshal([]byte(raw), &thinking))
	require.JSONEq(t, `{"prefix_mismatch_behavior":"drop_block"}`, string(thinking.BlockBinding))
	require.Contains(t, thinking.Extra, "future_flag")

	out, err := json.Marshal(thinking)
	require.NoError(t, err)
	require.JSONEq(t, raw, string(out))

	plain, err := json.Marshal(Thinking{Type: "enabled"})
	require.NoError(t, err)
	require.JSONEq(t, `{"type":"enabled"}`, string(plain), "the gateway must not add block_binding on its own")
}
