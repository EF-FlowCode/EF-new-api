package controller

import (
	"encoding/json"
	"math"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPublicEventBatchPreservesStatePresence(t *testing.T) {
	tests := []struct {
		name        string
		value       map[string]any
		wantPresent bool
		wantNull    bool
		wantValue   string
	}{
		{
			name:        "omitted",
			value:       map[string]any{"events": []any{}, "done": false},
			wantPresent: false,
		},
		{
			name:        "explicit null",
			value:       map[string]any{"events": []any{}, "state": nil, "done": false},
			wantPresent: true,
			wantNull:    true,
			wantValue:   "null",
		},
		{
			name:        "json value",
			value:       map[string]any{"events": []any{}, "state": map[string]any{"seen": 1}, "done": false},
			wantPresent: true,
			wantValue:   `{"seen":1}`,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			result, err := decodePublicEventBatch(testCase.value, publicProtocolLimits{})
			require.NoError(t, err)
			assert.Equal(t, testCase.wantPresent, result.State.Present)
			assert.Equal(t, testCase.wantNull, result.State.Null)
			if testCase.wantPresent {
				assert.JSONEq(t, testCase.wantValue, string(result.State.Value))
			} else {
				assert.Empty(t, result.State.Value)
			}

			if result.State.Present {
				_, err = result.State.PluginValue()
				require.NoError(t, err)
			}
		})
	}
}

func TestPublicEventBatchValidatesSemanticContract(t *testing.T) {
	result, err := decodePublicEventBatch(map[string]any{
		"events": []any{
			map[string]any{"type": "progress", "progress": 42.5, "message": "working"},
			map[string]any{"type": "output", "data": map[string]any{"url": "https://example.invalid/video.mp4"}},
			map[string]any{"type": "error", "code": "provider_failed", "message": "not exposed"},
		},
		"done": true,
	}, publicProtocolLimits{})
	require.NoError(t, err)
	require.Len(t, result.Events, 3)
	assert.Equal(t, "progress", result.Events[0].Type)
	assert.Equal(t, 42.5, *result.Events[0].Progress)
	assert.Equal(t, "working", *result.Events[0].Message)
	assert.JSONEq(t, `{"url":"https://example.invalid/video.mp4"}`, string(result.Events[1].Data))
	assert.Equal(t, "provider_failed", *result.Events[2].Code)
	assert.True(t, result.Done)

	invalid := []struct {
		name  string
		value any
	}{
		{
			name:  "unknown top-level field",
			value: map[string]any{"events": []any{}, "done": false, "sequence_number": 99},
		},
		{
			name:  "unknown event field",
			value: map[string]any{"events": []any{map[string]any{"type": "output", "data": "x", "id": "plugin-id"}}, "done": false},
		},
		{
			name:  "progress outside range",
			value: map[string]any{"events": []any{map[string]any{"type": "progress", "progress": 101}}, "done": false},
		},
		{
			name:  "missing output data",
			value: map[string]any{"events": []any{map[string]any{"type": "output"}}, "done": false},
		},
		{
			name:  "empty error message",
			value: map[string]any{"events": []any{map[string]any{"type": "error", "message": " "}}, "done": false},
		},
		{
			name:  "unsupported event",
			value: map[string]any{"events": []any{map[string]any{"type": "response.completed"}}, "done": false},
		},
		{
			name:  "non-json number",
			value: map[string]any{"events": []any{}, "done": false, "state": math.Inf(1)},
		},
	}
	for _, testCase := range invalid {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := decodePublicEventBatch(testCase.value, publicProtocolLimits{})
			require.Error(t, err)
		})
	}
}

func TestPublicEventBatchEnforcesBounds(t *testing.T) {
	limits := defaultPublicProtocolLimits()
	limits.MaxEventsPerTick = 1
	_, err := decodePublicEventBatch(map[string]any{
		"events": []any{
			map[string]any{"type": "progress"},
			map[string]any{"type": "progress"},
		},
		"done": false,
	}, limits)
	require.ErrorContains(t, err, "exceed limit")

	limits = defaultPublicProtocolLimits()
	limits.MaxEventBytes = 32
	_, err = decodePublicEventBatch(map[string]any{
		"events": []any{map[string]any{"type": "output", "data": strings.Repeat("x", 64)}},
		"done":   false,
	}, limits)
	require.ErrorContains(t, err, "protocol event exceeds")

	limits = defaultPublicProtocolLimits()
	limits.MaxStateDepth = 3
	_, err = decodePublicEventBatch(map[string]any{
		"events": []any{},
		"state":  map[string]any{"one": map[string]any{"two": map[string]any{"three": true}}},
		"done":   false,
	}, limits)
	require.ErrorContains(t, err, "depth limit")

	limits = defaultPublicProtocolLimits()
	_, err = decodePublicEventBatch(map[string]any{
		"events": []any{map[string]any{
			"type":    "progress",
			"message": strings.Repeat("x", limits.MaxMetadataValueBytes+1),
		}},
		"done": false,
	}, limits)
	require.ErrorContains(t, err, "progress event message exceeds")
}

func TestPublicEventBatchMalformedAndDefaultLimits(t *testing.T) {
	for _, raw := range []string{
		`null`, `[]`, `{}`, `{"events":null,"done":false}`, `{"events":[],"done":null}`,
		`{"events":[],"done":"true"}`, `{"events":[null],"done":false}`,
		`{"events":[{"type":"progress","progress":null}],"done":false}`,
		`{"events":[{"type":"error","message":null}],"done":false}`,
	} {
		var value any
		require.NoError(t, json.Unmarshal([]byte(raw), &value))
		_, err := decodePublicEventBatch(value, defaultPublicProtocolLimits())
		require.Error(t, err, raw)
	}
	for _, kind := range []string{"count", "event_bytes", "events_bytes", "event_depth", "state_bytes", "state_depth", "message", "code"} {
		t.Run(kind, func(t *testing.T) {
			events := []any{}
			batch := map[string]any{"events": events, "done": false}
			deep := any(true)
			for range 17 {
				deep = []any{deep}
			}
			switch kind {
			case "count":
				for range 17 {
					events = append(events, map[string]any{"type": "progress"})
				}
			case "event_bytes":
				events = append(events, map[string]any{"type": "output", "data": strings.Repeat("x", 32<<10)})
			case "events_bytes":
				for range 3 {
					events = append(events, map[string]any{"type": "output", "data": strings.Repeat("x", 23<<10)})
				}
			case "event_depth":
				events = append(events, map[string]any{"type": "output", "data": deep})
			case "state_bytes":
				batch["state"] = strings.Repeat("x", 16<<10)
			case "state_depth":
				batch["state"] = deep
			case "message":
				events = append(events, map[string]any{"type": "error", "message": strings.Repeat("x", 4097)})
			case "code":
				events = append(events, map[string]any{"type": "error", "message": "failed", "code": strings.Repeat("x", 129)})
			}
			batch["events"] = events
			_, err := decodePublicEventBatch(batch, defaultPublicProtocolLimits())
			require.Error(t, err)
		})
	}
}
