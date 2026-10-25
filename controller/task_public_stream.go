package controller

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"strconv"

	"eflow.local/new-api/model"
	host "eflow.local/new-api/relay/channel/task/jsplugin"
	management "eflow.local/new-api/service/taskplugin"
)

func renderPublicEventBatch(ctx context.Context, h *host.Handle, task *model.Task, previous publicProtocolState) (publicEventBatch, error) {
	pc, view, err := management.PublicTaskPresentation(ctx, h, task)
	if err != nil {
		return publicEventBatch{}, err
	}
	args := []any{pc, view}
	if previous.Present {
		value, err := previous.PluginValue()
		if err != nil {
			return publicEventBatch{}, err
		}
		args = append(args, value)
	}
	value, err := h.Protocol(ctx, "openai_responses", "renderEvents", args...)
	if err != nil {
		return publicEventBatch{}, err
	}
	return decodePublicEventBatch(value, defaultPublicProtocolLimits())
}

// This state exists only for one HTTP observation. It never writes the task or
// settles billing. Retrieval reconstructs a final snapshot from the saved task.
type publicStreamOutputs struct {
	items    []any
	bytes    int
	metadata map[string]any
}

func (s *publicStreamOutputs) apply(task *model.Task, batch publicEventBatch) ([]any, bool, error) {
	limits := defaultPublicProtocolLimits()
	if batch.Done && task.Status != model.TaskStatusSuccess {
		return nil, false, errors.New("renderer ended before durable success")
	}
	added := make([]any, 0)
	size, progress := s.bytes, false
	metadata := map[string]any{}
	maps.Copy(metadata, s.metadata)
	for _, event := range batch.Events {
		switch event.Type {
		case "error":
			return nil, false, errors.New("renderer reported an observation error")
		case "progress":
			progress = true
			if event.Progress != nil {
				metadata["task_progress"] = strconv.FormatFloat(*event.Progress, 'f', -1, 64)
			}
			if event.Message != nil {
				metadata["task_message"] = *event.Message
			}
		case "output":
			text := string(event.Data)
			if len(event.Data) > 0 && event.Data[0] == '"' {
				if err := json.Unmarshal(event.Data, &text); err != nil {
					return nil, false, err
				}
			}
			size += len(text)
			if size > limits.MaxTotalOutputBytes || len(s.items)+len(added) >= limits.MaxOutputs {
				return nil, false, errors.New("renderer output limit exceeded")
			}
			added = append(added, map[string]any{
				"id":   fmt.Sprintf("msg_%s_%d", task.TaskID, len(s.items)+len(added)),
				"type": "message", "role": "assistant", "status": "completed",
				"content": []any{map[string]any{"type": "output_text", "text": text, "annotations": []any{}, "logprobs": []any{}}},
			})
		}
	}
	// Commit only after the entire tick satisfies semantic and cumulative limits.
	if s.items == nil {
		s.items = []any{}
	}
	s.items = append(s.items, added...)
	s.bytes = size
	s.metadata = metadata
	return added, progress, nil
}
