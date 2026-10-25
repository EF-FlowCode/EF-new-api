package oairesponses

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"eflow.local/new-api/relaykit/dto"
	"eflow.local/new-api/relaykit/relayconvert/convmeta"
)

// PrepareCustomTools applies the custom-tool subset of upstream 335ebcff7 /
// 1a4166d8e. Normalizing to Responses function items lets all three existing
// converters share the same selection, history and forced-choice contract.
// The source request is never modified: retries must start from its raw input.
func PrepareCustomTools(req *dto.OpenAIResponsesRequest) (*dto.OpenAIResponsesRequest, *convmeta.ResponsesToolState, error) {
	if req == nil {
		return nil, nil, fmt.Errorf("request is nil")
	}
	out := *req
	state := &convmeta.ResponsesToolState{CustomToolNames: map[string]struct{}{}}
	var tools []map[string]any
	if rawJSONPresent(req.Tools) {
		if err := decodeCustomJSON(req.Tools, &tools); err != nil {
			return nil, nil, fmt.Errorf("invalid tools: %w", err)
		}
	}
	names := map[string]struct{}{}
	for _, tool := range tools {
		if tool["type"] == "function" {
			names[strings.TrimSpace(customString(tool["name"]))] = struct{}{}
		}
	}
	for i, tool := range tools {
		if tool["type"] != "custom" {
			continue
		}
		name := strings.TrimSpace(customString(tool["name"]))
		if name == "" {
			return nil, nil, fmt.Errorf("custom tool tools[%d] is missing name", i)
		}
		if _, exists := names[name]; exists {
			return nil, nil, fmt.Errorf("custom tool name conflict: %q", name)
		}
		names[name] = struct{}{}
		state.CustomToolNames[name] = struct{}{}
		parts := []string{}
		if description := strings.TrimSpace(customString(tool["description"])); description != "" {
			parts = append(parts, description)
		}
		parts = append(parts, `This tool takes freeform text. Put the complete raw text in the "input" argument.`)
		if format, ok := tool["format"].(map[string]any); ok && format["type"] == "grammar" {
			if definition := strings.TrimSpace(customString(format["definition"])); definition != "" {
				parts = append(parts, fmt.Sprintf("The input must match this %s grammar:\n%s", customString(format["syntax"]), definition))
			}
		}
		tools[i] = map[string]any{"type": "function", "name": name, "description": strings.Join(parts, "\n\n"),
			"parameters": map[string]any{"type": "object", "properties": map[string]any{"input": map[string]any{"type": "string"}}, "required": []string{"input"}, "additionalProperties": false}}
	}
	if len(state.CustomToolNames) > 0 {
		encoded, err := json.Marshal(tools)
		if err != nil {
			return nil, nil, err
		}
		out.Tools = encoded
	}
	if rawJSONPresent(req.ToolChoice) && bytes.HasPrefix(bytes.TrimSpace(req.ToolChoice), []byte("{")) {
		var choice map[string]any
		if err := decodeCustomJSON(req.ToolChoice, &choice); err != nil {
			return nil, nil, err
		}
		if choice["type"] == "custom" {
			name := strings.TrimSpace(customString(choice["name"]))
			if !state.IsCustomTool(name) {
				return nil, nil, fmt.Errorf("custom tool_choice %q is not declared", name)
			}
			choice["type"] = "function"
			encoded, err := json.Marshal(choice)
			if err != nil {
				return nil, nil, err
			}
			out.ToolChoice = encoded
		}
	}
	if rawJSONPresent(req.Input) && bytes.HasPrefix(bytes.TrimSpace(req.Input), []byte("[")) {
		var items []map[string]any
		if err := decodeCustomJSON(req.Input, &items); err != nil {
			return nil, nil, err
		}
		changed := false
		for _, item := range items {
			switch item["type"] {
			case "custom_tool_call":
				if strings.TrimSpace(customString(item["name"])) == "" {
					return nil, nil, fmt.Errorf("custom_tool_call item is missing name")
				}
				input := customString(item["input"])
				if _, ok := item["input"].(string); !ok && item["input"] != nil {
					raw, err := json.Marshal(item["input"])
					if err != nil {
						return nil, nil, err
					}
					input = string(raw)
				}
				args, err := json.Marshal(map[string]string{convmeta.CustomToolInputArgument: input})
				if err != nil {
					return nil, nil, err
				}
				item["type"] = "function_call"
				item["arguments"] = string(args)
				delete(item, "input")
				changed = true
			case "custom_tool_call_output":
				item["type"] = "function_call_output"
				changed = true
			}
		}
		if changed {
			encoded, err := json.Marshal(items)
			if err != nil {
				return nil, nil, err
			}
			out.Input = encoded
		}
	}
	if len(state.CustomToolNames) == 0 {
		state = nil
	}
	return &out, state, nil
}

func decodeCustomJSON(raw []byte, dst any) error {
	if !json.Valid(raw) {
		return fmt.Errorf("invalid JSON")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	return decoder.Decode(dst)
}
func customString(value any) string { text, _ := value.(string); return text }
