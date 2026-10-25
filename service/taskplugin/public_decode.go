package taskplugin

import (
	"context"
	host "eflow.local/new-api/relay/channel/task/jsplugin"
	"errors"
	"slices"
)

var ErrPublicProtocolRequest = errors.New("invalid public task request")

func decodePublicSubmit(ctx context.Context, h *host.Handle, input map[string]any, protocol string) error {
	if protocol == "task" || protocol == "native" {
		return decodeTaskEntry(ctx, h, input, protocol)
	}
	body, ok := input["protocolBody"].(map[string]any)
	if !ok {
		return errors.New("missing protocol body")
	}
	if hasPublicTaskReference(body) {
		return errors.New("origin task references require the owned remix entry")
	}
	mode := ""
	if protocol == "openai_responses" {
		mode = "sync"
		value, _ := body["value"].(map[string]any)
		for _, key := range []string{"stream", "background"} {
			if v, exists := value[key]; exists {
				if _, ok := v.(bool); !ok {
					return errors.New("protocol flags must be boolean")
				}
			}
		}
		if value["background"] == true {
			if !SupportsProtocol(h.Meta(), stringValue(input["upstreamModel"]), protocol, "background") {
				return errors.New("protocol mode is not supported by the fixed plugin")
			}
			mode = "background"
			input["publicBackground"] = true
		}
		if value["stream"] == true {
			mode = "stream"
		}
	}
	if !SupportsProtocol(h.Meta(), stringValue(input["upstreamModel"]), protocol, mode) {
		return errors.New("protocol mode is not supported by the fixed plugin")
	}
	if protocol == "openai_responses" && h.Identity().Key != "sunoapi" && !SupportsProtocol(h.Meta(), stringValue(input["upstreamModel"]), "openai_video", "") {
		return errors.New("this model requires an unsupported result lifecycle")
	}
	pc := map[string]any{"protocol": protocol, "operation": "create", "method": "POST", "model": input["model"], "upstreamModel": input["upstreamModel"], "stream": mode == "stream", "body": body, "params": map[string]string{}, "query": map[string][]string{}}
	if protocol == "openai_responses" {
		pc["path"] = "/v1/responses"
	} else {
		pc["path"] = "/v1/videos"
	}
	value, err := h.Protocol(ctx, protocol, "decodeRequest", pc)
	if err != nil {
		return err
	}
	intent, ok := value.(map[string]any)
	if !ok || intent["kind"] != "submit" || intent["model"] != input["model"] && intent["model"] != input["upstreamModel"] {
		return errors.New("invalid protocol submit identity")
	}
	request, ok := intent["requestBody"].(map[string]any)
	if !ok {
		return errors.New("invalid protocol request body")
	}
	// Existing remix has an owner/channel/account-aware entry. A public hook
	// cannot smuggle arbitrary provider task IDs past that host check.
	if ids, exists := intent["originTaskIds"]; exists {
		list, ok := ids.([]any)
		if !ok || len(list) > 0 {
			return errors.New("origin task references require the owned remix entry")
		}
	}
	if hasPublicTaskReference(request) {
		return errors.New("origin task references require the owned remix entry")
	}
	request["model"] = input["model"]
	input["requestBody"], input["action"], input["publicMode"] = request, intent["action"], mode
	return nil
}

// Draft-task references in provider-shaped Video bodies must follow the same
// ownership check as references declared by a Responses decoder.
func hasPublicTaskReference(value any) bool {
	switch value := value.(type) {
	case map[string]any:
		for key, item := range value {
			switch key {
			case "draft_task", "originTaskId", "originTaskIds", "previous_response_id":
				return true
			}
			if hasPublicTaskReference(item) {
				return true
			}
		}
	case []any:
		if slices.ContainsFunc(value, hasPublicTaskReference) {
			return true
		}
	}
	return false
}
