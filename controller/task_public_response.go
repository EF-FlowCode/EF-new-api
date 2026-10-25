package controller

import (
	"net/url"
	"sort"

	"eflow.local/new-api/model"
	management "eflow.local/new-api/service/taskplugin"
)

func publicTaskStatus(task *model.Task) string {
	switch task.Status {
	case model.TaskStatusSuccess:
		return "completed"
	case model.TaskStatusFailure:
		return "failed"
	case model.TaskStatusNotStart, model.TaskStatusSubmitted, model.TaskStatusQueued:
		return "queued"
	default:
		return "in_progress"
	}
}

func publicTaskRetrievalPath(id string) string { return "/v1/responses/" + url.PathEscape(id) }

// Identity, lifecycle and retrieval metadata always come from the durable task.
func publicTaskMetadata(task *model.Task, plugin map[string]any) map[string]string {
	metadata := map[string]string{"task_id": task.TaskID, "task_status": publicTaskStatus(task), "retrieval_path": publicTaskRetrievalPath(task.TaskID)}
	if task.ChannelDisabled {
		metadata["channel_status"] = management.ChannelStatusDisabled
	}
	keys := make([]string, 0, len(plugin))
	for key := range plugin {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if len(metadata) >= 16 {
			break
		}
		if _, owned := metadata[key]; owned || key == "channel_status" {
			continue
		}
		value, ok := plugin[key].(string)
		if !ok || len(key) > 64 || len(value) > 512 {
			continue
		}
		metadata[key] = value
	}
	return metadata
}

func publicTaskResponseBase(task *model.Task) map[string]any {
	status := publicTaskStatus(task)
	background := false
	if pc := task.PrivateData.PluginContext; pc != nil {
		background = pc.PublicBackground || pc.PublicMode == "background"
	}
	out := map[string]any{
		"id": task.TaskID, "object": "response", "created_at": task.CreatedAt, "model": task.Properties.OriginModelName,
		"status": status, "output": []any{}, "error": nil, "background": background, "completed_at": nil,
		"usage": nil, "incomplete_details": nil, "instructions": nil, "parallel_tool_calls": true,
		"temperature": 1, "tool_choice": "auto", "tools": []any{}, "top_p": 1,
		"metadata": publicTaskMetadata(task, nil),
	}
	if status == "completed" {
		out["completed_at"] = task.FinishTime
	}
	if status == "failed" {
		// FailReason remains in the internal task record. Provider errors are not
		// a safe public message and do not prove that a refund succeeded.
		out["error"] = map[string]any{"code": "task_failed", "message": "The task failed. Retrieve the task or contact the administrator with the task ID."}
	}
	return out
}

func publicTaskProgressResponse(task *model.Task) map[string]any {
	out := publicTaskResponseBase(task)
	out["status"], out["error"], out["completed_at"] = "in_progress", nil, nil
	return out
}
