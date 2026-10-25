package taskplugin

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"

	"eflow.local/new-api/common"
	"eflow.local/new-api/model"
	host "eflow.local/new-api/relay/channel/task/jsplugin"
	"gorm.io/gorm"
)

// PublicTask always reads the persisted identity, even after a channel rebind.
// Public presentation never receives credentials or host financial fields.
func (m *Manager) PublicTask(ctx context.Context, taskID string, userID int, protocol string, pinned ...*host.Handle) (*model.Task, *host.Handle, error) {
	var task model.Task
	if err := model.DB.WithContext(ctx).Where("task_id = ? AND user_id = ?", taskID, userID).First(&task).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil, ErrNotFound
		}
		return nil, nil, err
	}
	store, err := m.taskStore()
	if err != nil {
		return nil, nil, err
	}
	read, err := store.ReadPluginTasks(ctx, []int64{task.ID})
	if err != nil {
		return nil, nil, err
	}
	if len(read.Tasks) != 1 {
		return nil, nil, ErrNotFound
	}
	row := read.Tasks[0]
	task = row.Task
	i := task.PrivateData.Plugin
	if task.UserId != userID || i == nil || task.PrivateData.PluginContext == nil || protocol != "" && task.PrivateData.PluginContext.PublicProtocol != protocol || !row.Source.Matches(*i) {
		return nil, nil, ErrNotFound
	}
	if err := model.CheckTaskPluginEnabled(read.Control, *i); err != nil {
		return nil, nil, err
	}
	available, err := publicChannelAvailable(ctx, read.Control, &task)
	if err != nil {
		return nil, nil, err
	}
	if !available {
		// A disabled channel or binding stops new work, but a paid terminal
		// result stays readable as a historical record. It is still rendered
		// only by its own hash-checked source; unsettled tasks fail closed.
		if task.Status != model.TaskStatusSuccess && task.Status != model.TaskStatusFailure {
			return nil, nil, model.ErrTaskPluginDisabled
		}
		task.ChannelDisabled = true
	}
	h, err := m.publicHandle(host.Identity{Key: i.Key, Version: i.Version, SourceHash: i.SourceHash}, task.ChannelId, row.Source.Source, pinned...)
	if err != nil {
		return nil, nil, err
	}
	return &task, h, nil
}

// ChannelStatusDisabled marks a terminal result served after its channel or
// production binding was turned off (manually or by automatic disabling).
const ChannelStatusDisabled = "disabled"

func publicChannelAvailable(ctx context.Context, control model.TaskPluginSnapshot, task *model.Task) (bool, error) {
	b, bound := productionBindingFor(control, task.ChannelId)
	if !bound || !b.Enabled || b.Key != task.PrivateData.Plugin.Key {
		return false, nil
	}
	var channel model.Channel
	err := model.DB.WithContext(ctx).Select("id", "status").First(&channel, task.ChannelId).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return channel.Status == common.ChannelStatusEnabled, nil
}

func PublicTaskView(task *model.Task) (map[string]any, map[string]any) {
	var data any
	_ = json.Unmarshal(task.Data, &data)
	status := task.Status
	// Upstream success may precede the local funds transaction. No renderer
	// (native, video, Responses stream/final) receives provider terminal data
	// while the host still has a pending settlement.
	if status == model.TaskStatusBillingPending {
		data, status = map[string]any{}, model.TaskStatusInProgress
	}
	view := map[string]any{"task_id": task.TaskID, "status": status, "progress": task.Progress, "created_at": task.CreatedAt, "updated_at": task.UpdatedAt, "finish_time": task.FinishTime, "fail_reason": task.FailReason, "data": data, "properties": map[string]any{"origin_model_name": task.Properties.OriginModelName, "upstream_model_name": task.Properties.UpstreamModelName}}
	ctx := map[string]any{"model": task.Properties.OriginModelName, "upstreamModel": task.Properties.UpstreamModelName, "publicTaskId": task.TaskID, "artifacts": map[string]any{"video": map[string]any{"url": "/v1/videos/" + url.PathEscape(task.TaskID) + "/content"}}}
	return ctx, view
}

// Suno outputs can contain several songs. Keys come from the fixed source;
// the host supplies owner-authenticated URLs without exposing provider URLs.
func PublicTaskPresentation(ctx context.Context, h *host.Handle, task *model.Task) (map[string]any, map[string]any, error) {
	pc, view := PublicTaskView(task)
	if h.Identity().Key == "sunoapi" && task.Status == model.TaskStatusSuccess {
		value, err := h.Invoke(ctx, "listArtifacts", view)
		if err != nil {
			return nil, nil, err
		}
		items, ok := value.([]any)
		if !ok || len(items) > 128 {
			return nil, nil, errors.New("invalid task artifacts")
		}
		artifacts := map[string]any{}
		for _, item := range items {
			artifact, ok := item.(map[string]any)
			if !ok {
				return nil, nil, errors.New("invalid artifact")
			}
			key, ok := artifact["key"].(string)
			if !ok || len(key) == 0 || len(key) > 191 {
				return nil, nil, errors.New("invalid artifact key")
			}
			for _, r := range key {
				if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-') {
					return nil, nil, errors.New("invalid artifact key")
				}
			}
			if _, duplicate := artifacts[key]; duplicate {
				return nil, nil, errors.New("duplicate artifact key")
			}
			artifacts[key] = map[string]any{"url": "/v1/responses/" + url.PathEscape(task.TaskID) + "/content/" + key}
		}
		pc["artifacts"] = artifacts
	}
	return pc, view, nil
}

func RenderPublicVideo(ctx context.Context, h *host.Handle, task *model.Task) (map[string]any, error) {
	pc, view := PublicTaskView(task)
	value, err := h.Protocol(ctx, "openai_video", "render", pc, view)
	if err != nil {
		return nil, err
	}
	out, ok := value.(map[string]any)
	if !ok {
		return nil, errors.New("invalid video renderer output")
	}
	// Source rendering may add protocol fields, but cannot replace ownership
	// identity or turn a locally unsettled task into a successful response.
	out["id"], out["model"], out["object"], out["status"] = task.TaskID, task.Properties.OriginModelName, "video", task.Status.ToVideoStatus()
	delete(out, "channel_status")
	if task.ChannelDisabled {
		out["channel_status"] = ChannelStatusDisabled
	}
	return out, nil
}
