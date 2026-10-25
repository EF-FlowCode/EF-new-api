package reasoning

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestClaudeEffortMatchesEachModel(t *testing.T) {
	t.Parallel()

	tests := []struct {
		model  string
		effort string
		want   string
	}{
		{model: "claude-opus-4-6", effort: "xhigh", want: "max"},
		{model: "claude-opus-4-6", effort: "max", want: "max"},
		{model: "claude-opus-4-6", effort: "minimal", want: "low"},
		{model: "claude-opus-4-6", effort: "high", want: "high"},
		{model: "claude-opus-4-7", effort: "xhigh", want: "xhigh"},
		{model: "claude-opus-4-8", effort: "minimal", want: "low"},
		{model: "claude-opus-4-5-20251101", effort: "max", want: "high"},
		{model: "claude-opus-4-5-20251101", effort: "xhigh", want: "high"},
		{model: "claude-haiku-5-5", effort: "max", want: "max"},
		{model: "claude-mythos-preview", effort: "xhigh", want: "max"},
		{model: "anthropic/claude-opus-4-6", effort: "xhigh", want: "max"},
		{model: "some-unknown-model", effort: "xhigh", want: "xhigh"},
	}
	for _, tt := range tests {
		t.Run(tt.model+"/"+tt.effort, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, ClaudeEffort(tt.model, tt.effort))
		})
	}
}
