package common

import (
	"testing"

	common2 "eflow.local/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestApplyParamOverrideConditionRegex(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		path        string
		pattern     any
		context     map[string]any
		invert      bool
		passMissing bool
		wantMatch   bool
		wantError   string
	}{
		{name: "anchors and alternation match", input: `{"model":"gpt-4o"}`, pattern: `^(gpt|claude)-`, wantMatch: true},
		{name: "anchors reject other models", input: `{"model":"vendor/gpt-4o"}`, pattern: `^gpt-`},
		{name: "unanchored pattern matches substring", input: `{"model":"vendor/gpt-4o"}`, pattern: `gpt-`, wantMatch: true},
		{name: "Go inline flags", input: `{"model":"GPT-4o"}`, pattern: `(?i)^gpt-`, wantMatch: true},
		{name: "escaped pattern", input: `{"model":"gpt-4.1"}`, pattern: `^gpt-\d\.\d$`, wantMatch: true},
		{name: "invert match", input: `{"model":"gpt-4o"}`, pattern: `^gpt-`, invert: true},
		{name: "invert nonmatch", input: `{"model":"claude-sonnet"}`, pattern: `^gpt-`, invert: true, wantMatch: true},
		{name: "missing key does not match even inverted", input: `{}`, pattern: `.*`, invert: true},
		{name: "missing key can pass", input: `{}`, pattern: `^gpt-`, passMissing: true, wantMatch: true},
		{name: "missing key skips invalid pattern", input: `{}`, pattern: `[`},
		{name: "missing key can pass with invalid pattern", input: `{}`, pattern: `[`, passMissing: true, wantMatch: true},
		{name: "context fallback", input: `{}`, pattern: `^gpt-`, context: map[string]any{"model": "gpt-4o"}, wantMatch: true},
		{name: "body takes priority over context", input: `{"model":"claude-sonnet"}`, pattern: `^gpt-`, context: map[string]any{"model": "gpt-4o"}},
		{name: "negative array index", input: `{"messages":[{"role":"user"}]}`, path: "messages.-1.role", pattern: `^(user|system)$`, wantMatch: true},
		{name: "string conversion matches other text comparisons", input: `{"model":123}`, pattern: `^\d+$`, wantMatch: true},
		{name: "empty pattern matches empty string", input: `{"model":""}`, pattern: "", wantMatch: true},
		{name: "invalid pattern returns error", input: `{"model":"gpt-4o"}`, pattern: "[", wantError: "invalid regex pattern"},
		{name: "nonstring pattern returns error", input: `{"model":"123"}`, pattern: 123, wantError: "regex pattern must be a string"},
		{name: "missing pattern returns error", input: `{"model":"gpt-4o"}`, wantError: "regex pattern must be a string"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := tt.path
			if path == "" {
				path = "model"
			}
			override := map[string]any{
				"operations": []any{map[string]any{
					"mode": "set", "path": "matched", "value": true,
					"conditions": []any{map[string]any{
						"path": path, "mode": "regex", "value": tt.pattern,
						"invert": tt.invert, "pass_missing_key": tt.passMissing,
					}},
				}},
			}
			out, err := ApplyParamOverride([]byte(tt.input), override, tt.context)
			if tt.wantError != "" {
				require.ErrorContains(t, err, tt.wantError)
				assert.ErrorContains(t, err, "comparison failed for path "+path)
				return
			}
			require.NoError(t, err)
			var result map[string]any
			require.NoError(t, common2.Unmarshal(out, &result))
			assert.Equal(t, tt.wantMatch, result["matched"] == true)
			if !tt.wantMatch {
				assert.JSONEq(t, tt.input, string(out))
			}
		})
	}
}

func TestApplyParamOverrideRegexConditionLogic(t *testing.T) {
	for _, tt := range []struct {
		logic     string
		wantMatch bool
	}{
		{logic: "OR", wantMatch: true},
		{logic: "AND", wantMatch: false},
	} {
		t.Run(tt.logic, func(t *testing.T) {
			override := map[string]any{
				"operations": []any{map[string]any{
					"mode": "set", "path": "matched", "value": true, "logic": tt.logic,
					"conditions": []any{
						map[string]any{"path": "model", "mode": "regex", "value": `^gpt-`},
						map[string]any{"path": "stream", "mode": "full", "value": true},
					},
				}},
			}
			out, err := ApplyParamOverride([]byte(`{"model":"gpt-4o","stream":false}`), override, nil)
			require.NoError(t, err)
			var result map[string]any
			require.NoError(t, common2.Unmarshal(out, &result))
			assert.Equal(t, tt.wantMatch, result["matched"] == true)
		})
	}
}

func TestApplyParamOverridePruneObjectsRegex(t *testing.T) {
	condition := map[string]any{"path": "role", "mode": "regex"}
	override := map[string]any{
		"operations": []any{map[string]any{
			"mode": "prune_objects", "path": "messages",
			"value": map[string]any{
				"conditions": []any{condition},
			},
		}},
	}
	// Reusing a channel configuration must not reuse compiled patterns across requests.
	for _, tt := range []struct {
		name      string
		input     string
		pattern   string
		want      string
		wantError string
	}{
		{
			name:    "one condition matches several objects",
			input:   `{"messages":[{"role":"system"},{"role":"user"},{"role":"developer"}]}`,
			pattern: `^(system|developer)$`, want: `{"messages":[{"role":"user"}]}`,
		},
		{
			name:    "next request uses updated pattern",
			input:   `{"messages":[{"role":"system"},{"role":"user"},{"role":"developer"}]}`,
			pattern: `^user$`, want: `{"messages":[{"role":"system"},{"role":"developer"}]}`,
		},
		{
			name:    "missing keys skip invalid pattern",
			input:   `{"messages":[{"content":"hello"},{"content":"world"}]}`,
			pattern: `[`, want: `{"messages":[{"content":"hello"},{"content":"world"}]}`,
		},
		{
			name:    "invalid pattern errors on first present key after missing keys",
			input:   `{"messages":[{"content":"hello"},{"role":"user"},{"role":"system"}]}`,
			pattern: `[`, wantError: "comparison failed for path role: invalid regex pattern",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			condition["value"] = tt.pattern
			out, err := ApplyParamOverride([]byte(tt.input), override, nil)
			if tt.wantError != "" {
				require.ErrorContains(t, err, tt.wantError)
				return
			}
			require.NoError(t, err)
			assert.JSONEq(t, tt.want, string(out))
		})
	}
}
