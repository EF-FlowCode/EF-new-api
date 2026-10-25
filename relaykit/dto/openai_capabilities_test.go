package dto

import (
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestIsOpenAIGPT5Model(t *testing.T) {
	tests := []struct {
		model string
		want  bool
	}{
		{model: "gpt-5", want: true},
		{model: "gpt-5-mini", want: true},
		{model: "gpt-5-chat-latest", want: true},
		{model: "gpt-5.6-luna", want: true},
		{model: "gpt-5.4-nano", want: true},
		{model: "gpt-5.2-2025-12-11", want: true},
		{model: "gpt-6-astra", want: false},
		{model: "gpt-50", want: false},
		{model: "gpt-5custom", want: false},
		{model: " GPT-5 ", want: false},
		{model: "gpt-4.1", want: false},
		{model: "gpt-4.1-nano", want: false},
		{model: "gpt-4o", want: false},
		{model: "gpt-4.5-preview", want: false},
		{model: "gpt-oss-120b", want: false},
		{model: "gpt-image-2", want: false},
		{model: "gpt-realtime-2.1", want: false},
		{model: "chatgpt-4o-latest", want: false},
		{model: "o3-mini", want: false},
		{model: "gpt-", want: false},
		{model: "", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			assert.Equal(t, tt.want, IsOpenAIGPT5Model(tt.model))
		})
	}
}
