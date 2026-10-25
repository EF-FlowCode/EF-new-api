package gemini

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCleanFunctionParametersEnumSanitization(t *testing.T) {
	tests := []struct {
		name     string
		params   map[string]any
		expected map[string]any
	}{
		{
			name: "string enum passes through",
			params: map[string]any{
				"type": "string",
				"enum": []any{"red", "green", "blue"},
			},
			expected: map[string]any{
				"type": "STRING",
				"enum": []any{"red", "green", "blue"},
			},
		},
		{
			name: "boolean enum is dropped and type is kept",
			params: map[string]any{
				"type": "boolean",
				"enum": []any{true},
			},
			expected: map[string]any{
				"type": "BOOLEAN",
			},
		},
		{
			name: "mixed string and boolean enum is dropped",
			params: map[string]any{
				"type": "string",
				"enum": []any{"on", true},
			},
			expected: map[string]any{
				"type": "STRING",
			},
		},
		{
			name: "number enum is dropped",
			params: map[string]any{
				"type": "number",
				"enum": []any{1.0, 2.5},
			},
			expected: map[string]any{
				"type": "NUMBER",
			},
		},
		{
			name: "enum nested in properties is sanitized",
			params: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"flag": map[string]any{
						"type": "boolean",
						"enum": []any{true},
					},
					"color": map[string]any{
						"type": "string",
						"enum": []any{"red"},
					},
				},
			},
			expected: map[string]any{
				"type": "OBJECT",
				"properties": map[string]any{
					"flag": map[string]any{
						"type": "BOOLEAN",
					},
					"color": map[string]any{
						"type": "STRING",
						"enum": []any{"red"},
					},
				},
			},
		},
		{
			name: "enum nested in array items is sanitized",
			params: map[string]any{
				"type": "array",
				"items": map[string]any{
					"type": "boolean",
					"enum": []any{false},
				},
			},
			expected: map[string]any{
				"type":  "ARRAY",
				"items": map[string]any{"type": "BOOLEAN"},
			},
		},
		{
			name: "enum nested in anyOf is sanitized",
			params: map[string]any{
				"anyOf": []any{
					map[string]any{
						"type": "boolean",
						"enum": []any{true},
					},
					map[string]any{
						"type": "string",
						"enum": []any{"yes", "no"},
					},
				},
			},
			expected: map[string]any{
				"anyOf": []any{
					map[string]any{"type": "BOOLEAN"},
					map[string]any{
						"type": "STRING",
						"enum": []any{"yes", "no"},
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cleaned := CleanFunctionParameters(tt.params)
			assert.Equal(t, tt.expected, cleaned)
		})
	}
}

func TestCleanFunctionParametersShallowDropsBooleanEnum(t *testing.T) {
	// Schemas beyond geminiFunctionSchemaMaxDepth fall back to the shallow
	// cleaner, which must apply the same enum sanitization.
	params := map[string]any{
		"type": "boolean",
		"enum": []any{true},
	}
	cleaned := cleanGeminiFunctionParametersShallow(params)
	assert.Equal(t, map[string]any{"type": "BOOLEAN"}, cleaned)

	stringEnum := map[string]any{
		"type": "string",
		"enum": []any{"a", "b"},
	}
	cleaned = cleanGeminiFunctionParametersShallow(stringEnum)
	assert.Equal(t, map[string]any{
		"type": "STRING",
		"enum": []any{"a", "b"},
	}, cleaned)
}
