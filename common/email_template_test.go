package common

import "testing"

func TestRenderPlaceholders(t *testing.T) {
	cases := []struct {
		name string
		tpl  string
		vars map[string]string
		want string
	}{
		{
			name: "basic",
			tpl:  "Hello {{name}}!",
			vars: map[string]string{"name": "Alice"},
			want: "Hello Alice!",
		},
		{
			name: "with whitespace inside braces",
			tpl:  "Hi {{ name }}",
			vars: map[string]string{"name": "Bob"},
			want: "Hi Bob",
		},
		{
			name: "missing key keeps original",
			tpl:  "Hi {{unknown}} ok",
			vars: map[string]string{"name": "X"},
			want: "Hi {{unknown}} ok",
		},
		{
			name: "unclosed brace preserved",
			tpl:  "Hi {{name",
			vars: map[string]string{"name": "X"},
			want: "Hi {{name",
		},
		{
			name: "multiple",
			tpl:  "{{a}}-{{b}}-{{a}}",
			vars: map[string]string{"a": "1", "b": "2"},
			want: "1-2-1",
		},
		{
			name: "empty template",
			tpl:  "",
			vars: map[string]string{"a": "1"},
			want: "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := RenderPlaceholders(tc.tpl, tc.vars)
			if got != tc.want {
				t.Fatalf("want %q, got %q", tc.want, got)
			}
		})
	}
}
