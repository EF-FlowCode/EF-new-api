package systemoneproxy

import (
	"encoding/json"
	"fmt"
)

type chatUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// Aliases describe the same dimensions, never additional billable tokens.
func normalizeUsage(raw json.RawMessage) (chatUsage, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil || fields == nil {
		return chatUsage{}, fmt.Errorf("SystemOne usage is missing or invalid")
	}
	counts := map[string]*int{}
	for _, key := range []string{"input_tokens", "output_tokens", "prompt_tokens", "completion_tokens"} {
		value, ok := fields[key]
		if !ok || string(value) == "null" {
			continue
		}
		var count int
		if err := json.Unmarshal(value, &count); err != nil || count < 0 || count > 1_000_000_000 {
			return chatUsage{}, fmt.Errorf("invalid SystemOne usage.%s", key)
		}
		counts[key] = &count
	}
	choose := func(native, legacy string) (int, error) {
		n, l := counts[native], counts[legacy]
		if n != nil && (*n > 0 || l == nil || *l == 0) {
			return *n, nil
		}
		if l != nil {
			return *l, nil
		}
		return 0, fmt.Errorf("SystemOne usage is missing %s/%s", native, legacy)
	}
	input, err := choose("input_tokens", "prompt_tokens")
	if err != nil {
		return chatUsage{}, err
	}
	output, err := choose("output_tokens", "completion_tokens")
	if err != nil {
		return chatUsage{}, err
	}
	// The unmodified Chat gateway estimates usage whenever prompt_tokens == 0.
	// Reject instead of silently charging for the transport envelope or inventing tokens.
	if input == 0 {
		return chatUsage{}, fmt.Errorf("zero input usage cannot be billed faithfully through this Chat gateway")
	}
	return chatUsage{input, output, input + output}, nil
}
