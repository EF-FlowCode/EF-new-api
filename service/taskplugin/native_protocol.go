package taskplugin

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strings"

	"eflow.local/new-api/common"
	"eflow.local/new-api/model"
	"eflow.local/new-api/pkg/jsplugin"
	host "eflow.local/new-api/relay/channel/task/jsplugin"
)

const NativeQueryContextKey = "task_plugin_native_query"

type NativeRequest struct {
	Key     string
	Path    string
	Method  string
	Body    map[string]any
	Query   map[string][]string
	TaskIDs []string
}

// MatchNativeRoute uses the fixed source manifest, including exact trailing
// slashes. No plugin may register or intercept host management endpoints.
func MatchNativeRoute(meta jsplugin.Meta, method, path string) (jsplugin.Route, map[string]string, bool) {
	var found *jsplugin.Route
	var params map[string]string
	for _, r := range meta.Routes {
		if r.Method != method {
			continue
		}
		want, got := strings.Split(r.Path, "/"), strings.Split(path, "/")
		catchAll := strings.HasPrefix(want[len(want)-1], "*")
		if (!catchAll && len(want) != len(got)) || (catchAll && len(got) < len(want)) {
			continue
		}
		p, matches := map[string]string{}, true
		for i, part := range want {
			if catchAll && i == len(want)-1 {
				p[part[1:]] = "/" + strings.Join(got[i:], "/")
			} else if strings.HasPrefix(part, ":") && got[i] != "" {
				p[part[1:]] = got[i]
			} else if part != got[i] {
				matches = false
				break
			}
		}
		if !matches {
			continue
		}
		if found != nil {
			return jsplugin.Route{}, nil, false
		}
		copy := r
		found, params = &copy, p
	}
	if found == nil {
		return jsplugin.Route{}, nil, false
	}
	return *found, params, true
}

func taskEntryChannels(ctx context.Context) ([]model.Channel, error) {
	var channels []model.Channel
	err := model.DB.WithContext(ctx).Select("id", "type", "status", "models", "model_mapping").Where("status = ?", common.ChannelStatusEnabled).Find(&channels).Error
	return channels, err
}

func (m *Manager) ResolveGenericRoute(ctx context.Context, key, name string) (*PublicRoute, error) {
	if model.TaskPluginChannelType(key) == 0 || name == "" {
		return nil, ErrNotFound
	}
	channels, err := taskEntryChannels(ctx)
	if err != nil {
		return nil, err
	}
	route, err := m.resolveTaskRoute(channels, name, "", http.MethodPost, "task", key)
	if err == nil && route == nil {
		err = ErrNotFound
	}
	return route, err
}

func nativeIntent(ctx context.Context, h *host.Handle, n *NativeRequest) (jsplugin.Route, map[string]any, error) {
	r, params, ok := MatchNativeRoute(h.Meta(), n.Method, n.Path)
	if !ok {
		return r, nil, ErrNotFound
	}
	if r.Type == jsplugin.RouteTypeQuery {
		return r, map[string]any{"kind": "query", "taskIds": []any{params[r.TaskIDParam]}}, nil
	}
	pc := map[string]any{"path": n.Path, "method": n.Method, "params": params, "query": n.Query, "body": n.Body}
	value, err := h.Native(ctx, r.Decode, pc)
	if err != nil {
		return r, nil, err
	}
	intent, ok := value.(map[string]any)
	if !ok {
		return r, nil, errors.New("invalid native intent")
	}
	if r.Type == jsplugin.RouteTypeSubmit && intent["kind"] != "submit" {
		return r, nil, errors.New("invalid submit intent")
	}
	return r, intent, nil
}

// ResolveNativeRequest only decodes source code belonging to enabled fixed
// bindings. Decoders have no HTTP capability; execution and billing happen later.
func (m *Manager) ResolveNativeRequest(ctx context.Context, n *NativeRequest) (*PublicRoute, error) {
	channels, err := taskEntryChannels(ctx)
	if err != nil {
		return nil, err
	}
	eligible := []model.Channel{}
	var chosenName, chosenKind, chosenKey, queryIdentity string
	var lastError error
	for _, ch := range channels {
		h, b, ok := m.ProductionHandle(ch.Id)
		if !ok || model.TaskPluginKeyForChannel(ch.Type) != b.Key {
			continue
		}
		r, intent, err := nativeIntent(ctx, h, n)
		if err != nil {
			if !errors.Is(err, ErrNotFound) {
				lastError = err
			}
			continue
		}
		kind, name := stringValue(intent["kind"]), stringValue(intent["model"])
		if kind != "submit" && kind != "query" {
			return nil, errors.New("invalid native intent kind")
		}
		ids := ""
		if kind == "submit" {
			if name == "" {
				lastError = errors.New("native model is required")
				continue
			}
			pin, e := m.resolveTaskRoute([]model.Channel{ch}, name, n.Path, n.Method, "native", b.Key)
			if e != nil {
				return nil, e
			}
			if pin == nil {
				continue
			}
		} else {
			list, ok := intent["taskIds"].([]any)
			if !ok || len(list) == 0 || len(list) > 100 {
				return nil, errors.New("invalid native task IDs")
			}
			for _, id := range list {
				if s, ok := id.(string); !ok || s == "" || len(s) > 191 {
					return nil, errors.New("invalid task ID")
				}
			}
			raw, _ := json.Marshal(list)
			ids = string(raw)
		}
		if chosenKind != "" && (chosenName != name || chosenKind != kind || chosenKey != b.Key || queryIdentity != ids) {
			return nil, errors.New("ambiguous native route")
		}
		chosenName, chosenKind, chosenKey, queryIdentity = name, kind, b.Key, ids
		eligible = append(eligible, ch)
		_ = r
	}
	if len(eligible) == 0 {
		if lastError != nil {
			return nil, lastError
		}
		return nil, ErrNotFound
	}
	n.Key = chosenKey
	if chosenKind == "query" {
		if err := json.Unmarshal([]byte(queryIdentity), &n.TaskIDs); err != nil {
			return nil, err
		}
		return nil, nil
	}
	pin, err := m.resolveTaskRoute(eligible, chosenName, n.Path, n.Method, "native", chosenKey)
	if err != nil || pin == nil {
		return nil, errors.New("native task model unavailable")
	}
	pin.Native = n
	return pin, nil
}

func decodeTaskEntry(ctx context.Context, h *host.Handle, input map[string]any, protocol string) error {
	name := stringValue(input["upstreamModel"])
	// Immediate image results have no admitted billing/result lifecycle yet.
	if h.Identity().Key != "sunoapi" && h.Identity().Key != "happyhorse" && !SupportsProtocol(h.Meta(), name, "openai_video", "") {
		return errors.New("unsupported task result lifecycle")
	}
	request, ok := input["requestBody"].(map[string]any)
	if !ok || hasPublicTaskReference(request) {
		return errors.New("invalid task request or unsupported origin reference")
	}
	if protocol == "native" {
		n := &NativeRequest{Path: stringValue(input["nativePath"]), Method: stringValue(input["nativeMethod"])}
		n.Body, _ = input["protocolBody"].(map[string]any)
		raw, _ := json.Marshal(input["nativeQuery"])
		_ = json.Unmarshal(raw, &n.Query)
		r, intent, err := nativeIntent(ctx, h, n)
		if err != nil {
			return err
		}
		if intent["kind"] != "submit" || intent["model"] != input["model"] {
			return errors.New("native model identity changed")
		}
		if len(r.Models) > 0 && !slices.Contains(r.Models, name) {
			return errors.New("native route model not supported")
		}
		if refs, exists := intent["originTaskIds"]; exists {
			list, ok := refs.([]any)
			if !ok || len(list) != 0 {
				return errors.New("origin reference requires owned remix")
			}
		}
		request, ok = intent["requestBody"].(map[string]any)
		if !ok || hasPublicTaskReference(request) {
			return errors.New("invalid native request")
		}
		input["action"] = jsplugin.ResolveRouteAction(r, stringValue(intent["action"]))
	} else if action, ok := request["action"].(string); ok {
		input["action"] = action
	}
	if h.Identity().Key == "sunoapi" {
		var action string
		switch name {
		case "suno_music":
			action = "MUSIC"
		case "suno_lyrics":
			action = "LYRICS"
		default:
			return errors.New("unsupported Suno model")
		}
		// The action selects the upstream operation; it must agree with the
		// model whose usage schema and price the host is about to use.
		if given := stringValue(input["action"]); given != "" && !strings.EqualFold(given, action) {
			return errors.New("Suno action does not match bound model")
		}
		input["action"] = action
	}
	request["model"] = input["model"]
	// Older fixed Kling sources merge native metadata after their default
	// machine model. Lock these two identity fields before that merge; do not
	// require changing existing production source snapshots to admit aliases.
	if h.Identity().Key == "kling" {
		if metadata, ok := request["metadata"].(map[string]any); ok {
			metadata["model"], metadata["model_name"] = name, name
		}
	}
	input["requestBody"] = request
	return nil
}

func GenericTaskView(task *model.Task) map[string]any {
	view := map[string]any{"task_id": task.TaskID, "model": task.Properties.OriginModelName, "status": task.Status, "progress": task.Progress, "created_at": task.CreatedAt, "finish_time": task.FinishTime, "fail_reason": task.FailReason}
	if task.ChannelDisabled {
		view["channel_status"] = ChannelStatusDisabled
	}
	return view
}

func RenderNativeTask(ctx context.Context, h *host.Handle, task *model.Task, method, path string, query map[string][]string) (any, error) {
	r, params, ok := MatchNativeRoute(h.Meta(), method, path)
	if !ok {
		return nil, ErrNotFound
	}
	pc, view := PublicTaskView(task)
	pc["path"], pc["method"], pc["params"], pc["query"] = path, method, params, query
	// PublicTaskView hides provider terminal data while settlement is pending.
	if r.Type == jsplugin.RouteTypeDynamic {
		return h.Native(ctx, r.Render, pc, []any{view})
	}
	return h.Native(ctx, r.Render, pc, view)
}
