package controller

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"eflow.local/new-api/common"
	"eflow.local/new-api/constant"
	"eflow.local/new-api/dto"
	"eflow.local/new-api/model"
	host "eflow.local/new-api/relay/channel/task/jsplugin"
	"eflow.local/new-api/service"
	management "eflow.local/new-api/service/taskplugin"
	"eflow.local/new-api/types"
	"github.com/gin-gonic/gin"
)

func RelayPublicResponses(c *gin.Context) {
	if service.TaskPublicRoute(c) == nil {
		Relay(c, types.RelayFormatOpenAIResponses)
		return
	}
	relayPublicTaskWithAdmission(c, pluginProtocolObservationAdmissions, RelayTask)
}

func relayPublicTaskWithAdmission(c *gin.Context, limiter *pluginProtocolObservationLimiter, submit gin.HandlerFunc) {
	route := service.TaskPublicRoute(c)
	key := ""
	for _, target := range route.Targets {
		if target.Binding.Key == "" || (key != "" && key != target.Binding.Key) {
			respondPublicTaskError(c, 503, "task_route_unavailable")
			return
		}
		key = target.Binding.Key
	}
	release, err := limiter.acquire(key, common.GetContextKeyInt(c, constant.ContextKeyUserId), common.GetContextKeyInt(c, constant.ContextKeyTokenId))
	if err != nil {
		status, code := 503, "task_route_unavailable"
		if errors.Is(err, errPluginProtocolObservationLimitExceeded) {
			status, code = 429, "task_observation_limit"
		}
		respondPublicTaskError(c, status, code)
		return
	}
	defer release()
	w, err := newPublicDeadlineWriter(c.Request.Context(), c.Writer, publicWriteTimeout)
	if err != nil {
		respondPublicTaskError(c, 503, "task_observation_unavailable")
		return
	}
	c.Writer = w
	defer w.close()
	submit(c)
}

func RetrievePublicTaskResponse(c *gin.Context) {
	task, h, err := management.Default.PublicTask(c.Request.Context(), c.Param("response_id"), common.GetContextKeyInt(c, constant.ContextKeyUserId), "openai_responses")
	if err != nil {
		if errors.Is(err, management.ErrNotFound) {
			respondPublicTaskError(c, http.StatusNotFound, "response_not_found")
		} else {
			respondPublicTaskError(c, http.StatusServiceUnavailable, "task_unavailable")
		}
		return
	}
	c.Header("X-Task-ID", task.TaskID)
	response, err := publicResponse(c.Request.Context(), h, task)
	if err != nil {
		respondPublicTaskError(c, http.StatusInternalServerError, "task_render_failed")
		return
	}
	c.JSON(http.StatusOK, response)
}

func RetrievePublicTaskContent(c *gin.Context) {
	task, h, err := management.Default.PublicTask(c.Request.Context(), c.Param("response_id"), common.GetContextKeyInt(c, constant.ContextKeyUserId), "openai_responses")
	if err != nil || task.Status != model.TaskStatusSuccess {
		videoProxyError(c, 404, "artifact_not_found", "Content is unavailable")
		return
	}
	pc, _, err := management.PublicTaskPresentation(c.Request.Context(), h, task)
	if err != nil {
		videoProxyError(c, 503, "artifact_unavailable", "Content is unavailable")
		return
	}
	artifacts, _ := pc["artifacts"].(map[string]any)
	key := c.Param("artifact_key")
	if _, ok := artifacts[key]; !ok || h.Identity().Key != "sunoapi" {
		videoProxyError(c, 404, "artifact_not_found", "Content is unavailable")
		return
	}
	channel, err := model.GetChannelById(task.ChannelId, false)
	if err != nil {
		videoProxyError(c, 503, "artifact_unavailable", "Content is unavailable")
		return
	}
	serveTaskPluginContent(c, task, channel, key)
}

func publicResponse(ctx context.Context, h *host.Handle, task *model.Task) (map[string]any, error) {
	out := publicTaskResponseBase(task)
	status := publicTaskStatus(task)
	if status != "completed" {
		return out, nil
	}
	meta := h.Meta()
	if !management.SupportsProtocol(meta, task.Properties.UpstreamModelName, "openai_responses", "sync") && !management.SupportsProtocol(meta, task.Properties.UpstreamModelName, "openai_responses", "background") {
		batch, err := renderPublicEventBatch(ctx, h, task, publicProtocolState{})
		if err != nil {
			return nil, err
		}
		var outputs publicStreamOutputs
		if _, _, err = outputs.apply(task, batch); err != nil {
			return nil, err
		}
		out["output"] = outputs.items
		out["metadata"] = publicTaskMetadata(task, outputs.metadata)
		return out, nil
	}
	pc, view, err := management.PublicTaskPresentation(ctx, h, task)
	if err != nil {
		return nil, err
	}
	value, err := h.Protocol(ctx, "openai_responses", "renderFinal", pc, view)
	if err != nil {
		return nil, err
	}
	rendered, ok := value.(map[string]any)
	if !ok {
		return nil, errors.New("invalid final response")
	}
	items, ok := rendered["output"].([]any)
	if !ok {
		return nil, errors.New("invalid response output")
	}
	for i, value := range items {
		item, ok := value.(map[string]any)
		if !ok {
			return nil, errors.New("invalid response item")
		}
		item["id"] = fmt.Sprintf("msg_%s_%d", task.TaskID, i)
		if item["type"] != "message" || item["role"] != "assistant" {
			return nil, errors.New("unsupported response item")
		}
		parts, ok := item["content"].([]any)
		if !ok {
			return nil, errors.New("invalid response content")
		}
		for _, value := range parts {
			part, ok := value.(map[string]any)
			if !ok || part["type"] != "output_text" {
				return nil, errors.New("unsupported response content")
			}
			if _, ok := part["text"].(string); !ok {
				return nil, errors.New("invalid response text")
			}
			for _, key := range []string{"annotations", "logprobs"} {
				value, exists := part[key]
				if !exists {
					part[key] = []any{}
					continue
				}
				items, ok := value.([]any)
				if !ok {
					return nil, errors.New("invalid response content array")
				}
				for _, item := range items {
					if _, ok := item.(map[string]any); !ok {
						return nil, errors.New("invalid response content array item")
					}
				}
			}
		}
		item["status"] = "completed"
	}
	out["output"] = items
	if metadata, ok := rendered["metadata"].(map[string]any); ok {
		out["metadata"] = publicTaskMetadata(task, metadata)
	}
	return out, nil
}

type publicEventWriter struct {
	c           *gin.Context
	sequence    int
	outputCount int
}

func (w *publicEventWriter) send(kind string, fields map[string]any) error {
	fields["type"], fields["sequence_number"] = kind, w.sequence
	w.sequence++
	raw, err := json.Marshal(fields)
	if err != nil {
		return err
	}
	if _, err = fmt.Fprintf(w.c.Writer, "event: %s\ndata: %s\n\n", kind, raw); err != nil {
		return err
	}
	return flushPublicWriter(w.c.Writer)
}

func (w *publicEventWriter) finish(response map[string]any) error {
	if response["status"] == "failed" {
		return w.send("response.failed", map[string]any{"response": response})
	}
	items, _ := response["output"].([]any)
	if err := w.writeOutputs(items[w.outputCount:]); err != nil {
		return err
	}
	return w.send("response.completed", map[string]any{"response": response})
}

// Each output event is emitted once, at its original tick, with a host-owned ID.
func (w *publicEventWriter) writeOutputs(items []any) error {
	for _, value := range items {
		i := w.outputCount
		item, ok := value.(map[string]any)
		if !ok {
			return errors.New("invalid output item")
		}
		parts, _ := item["content"].([]any)
		start := map[string]any{"id": item["id"], "type": item["type"], "role": item["role"], "status": "in_progress", "content": []any{}}
		if err := w.send("response.output_item.added", map[string]any{"output_index": i, "item": start}); err != nil {
			return err
		}
		for j, value := range parts {
			part, ok := value.(map[string]any)
			if !ok {
				return errors.New("invalid output content")
			}
			if part["type"] != "output_text" {
				return errors.New("unsupported public output content")
			}
			text, _ := part["text"].(string)
			fields := func() map[string]any {
				return map[string]any{"output_index": i, "content_index": j, "item_id": item["id"]}
			}
			f := fields()
			f["part"] = map[string]any{"type": "output_text", "text": "", "annotations": []any{}, "logprobs": []any{}}
			if err := w.send("response.content_part.added", f); err != nil {
				return err
			}
			f = fields()
			f["delta"] = text
			f["logprobs"] = []any{}
			if err := w.send("response.output_text.delta", f); err != nil {
				return err
			}
			f = fields()
			f["text"] = text
			f["logprobs"] = []any{}
			if err := w.send("response.output_text.done", f); err != nil {
				return err
			}
			f = fields()
			f["part"] = part
			if err := w.send("response.content_part.done", f); err != nil {
				return err
			}
		}
		if err := w.send("response.output_item.done", map[string]any{"output_index": i, "item": item}); err != nil {
			return err
		}
		w.outputCount++
	}
	return nil
}

// Waiting observes the durable worker's result; disconnects never cancel,
// resubmit or refund accepted work. No second provider polling path is added.
func publishPublicTask(c *gin.Context, accepted *model.Task) *dto.TaskError {
	protocol := accepted.PrivateData.PluginContext.PublicProtocol
	readCtx := c.Request.Context()
	if protocol == "openai_responses" {
		var cancel context.CancelFunc
		readCtx, cancel = context.WithTimeout(readCtx, publicReadTimeout(constant.TaskProtocol))
		defer cancel()
	}
	task, h, err := management.Default.PublicTask(readCtx, accepted.TaskID, accepted.UserId, protocol)
	if err != nil {
		return service.TaskErrorWrapperLocal(errors.New("accepted task is unavailable; query using X-Task-ID"), "task_unavailable", 503)
	}
	if protocol == "task" {
		c.JSON(http.StatusOK, management.GenericTaskView(task))
		return nil
	}
	if protocol == "native" {
		pc := task.PrivateData.PluginContext
		out, err := management.RenderNativeTask(c.Request.Context(), h, task, pc.NativeMethod, pc.NativePath, c.Request.URL.Query())
		if err != nil {
			return service.TaskErrorWrapperLocal(errors.New("accepted task render failed; query using X-Task-ID"), "task_render_failed", 500)
		}
		c.JSON(http.StatusOK, out)
		return nil
	}
	if protocol == "openai_video" {
		out, err := management.RenderPublicVideo(c.Request.Context(), h, task)
		if err != nil {
			return service.TaskErrorWrapperLocal(errors.New("accepted task render failed"), "task_render_failed", 500)
		}
		c.JSON(http.StatusOK, out)
		return nil
	}
	mode := task.PrivateData.PluginContext.PublicMode
	if protocol != "openai_responses" || (mode != "background" && mode != "sync" && mode != "stream") {
		return service.TaskErrorWrapperLocal(errors.New("invalid saved public protocol"), "task_render_failed", 500)
	}
	if mode == "background" {
		out, err := publicResponse(readCtx, h, task)
		if err != nil {
			return service.TaskErrorWrapperLocal(err, "task_render_failed", 500)
		}
		c.JSON(http.StatusOK, out)
		return nil
	}
	return observePublicTask(c, task, constant.TaskProtocol, func(ctx context.Context, initial bool, previous any) publicObservation {
		return refreshPublicObservation(ctx, task, h, mode == "stream", initial, previous)
	})
}
