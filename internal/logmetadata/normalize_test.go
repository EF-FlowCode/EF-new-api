package logmetadata

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNormalizeLosslessAndIdempotent(t *testing.T) {
	input := `{"channel_id":123,"channel_name":"渠道","channel_type":2,"reject_reason":"private","quota":9007199254740993,"reasoning_effort":"high","admin_info":{"existing":true},"root_info":{"k":1},"audit_info":[1],"stream_status":"done"}`
	result, err := Normalize(input)
	require.NoError(t, err)
	var old, got map[string]json.RawMessage
	require.NoError(t, json.Unmarshal([]byte(input), &old))
	require.NoError(t, json.Unmarshal([]byte(result), &got))
	var admin map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(got["admin_info"], &admin))
	for _, key := range PrivateKeys() {
		require.JSONEq(t, string(old[key]), string(admin[key]))
		require.NotContains(t, got, key)
	}
	for _, key := range []string{"quota", "reasoning_effort", "root_info", "audit_info", "stream_status"} {
		require.Equal(t, old[key], got[key])
	}
	require.Equal(t, "9007199254740993", string(got["quota"]))
	again, err := Normalize(result)
	require.NoError(t, err)
	require.Equal(t, result, again)
}

func TestNormalizePreservesAmbiguousData(t *testing.T) {
	for _, input := range []string{
		`null`, `[]`, `3`, `{"x":`, `{"admin_info":null}`, `{"admin_info":[]}`,
		`{"channel_id":1,"admin_info":{"channel_id":2}}`,
		`{"channel_id":1,"channel_id":2}`, `{"x":{"a":1,"\u0061":2}}`,
		`{"x":[{"a":1,"a":2}]}`, `{"channel_id":1,"\ud800":2}`,
		"{\"channel_id\":1,\"x\":\"\xff\"}",
	} {
		t.Run(input, func(t *testing.T) {
			got, err := Normalize(input)
			require.Error(t, err)
			require.Equal(t, input, got)
		})
	}
	for _, input := range []string{"", "{}", " {\"n\":9007199254740993} ", `{"admin_info":{"channel_id":1}}`} {
		got, err := Normalize(input)
		require.NoError(t, err)
		require.Equal(t, input, got)
	}
	got, err := Normalize(`{"channel_id":1,"admin_info":{"channel_id":1}}`)
	require.NoError(t, err)
	require.JSONEq(t, `{"admin_info":{"channel_id":1}}`, got)
}
