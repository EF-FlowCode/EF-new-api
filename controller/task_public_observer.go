package controller

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"net/http"
	"time"

	"eflow.local/new-api/constant"
	"eflow.local/new-api/dto"
	"eflow.local/new-api/model"
	host "eflow.local/new-api/relay/channel/task/jsplugin"
	"eflow.local/new-api/service"
	management "eflow.local/new-api/service/taskplugin"
	"github.com/gin-gonic/gin"
)

type publicObservation struct {
	task     *model.Task
	previous any
	progress bool
	batch    *publicEventBatch
	terminal bool
	response map[string]any
	err      *dto.TaskError
}

func runPublicRefresh(ctx context.Context, timeout time.Duration, initial bool, previous any, refresh func(context.Context, bool, any) publicObservation) (result publicObservation) {
	readCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	// This work used to run under Gin's recovery. Keep a rendering panic from
	// escaping the new worker goroutine or mutating accepted task state.
	defer func() {
		if recover() != nil {
			result = publicObservation{err: service.TaskErrorWrapperLocal(errors.New("task renderer failed; query using X-Task-ID"), "task_render_failed", 500)}
		}
	}()
	return refresh(readCtx, initial, previous)
}

func publicReadTimeout(cfg constant.TaskProtocolConfig) time.Duration {
	d := min(5*time.Second, cfg.Heartbeat/2, cfg.Timeout)
	return max(time.Nanosecond, d)
}

func publicTickDelay(cfg constant.TaskProtocolConfig, id string, tick uint64) time.Duration {
	if cfg.Jitter == 0 {
		return cfg.Tick
	}
	h := fnv.New64a()
	_, _ = fmt.Fprintf(h, "%s:%d", id, tick)
	return cfg.Tick + time.Duration(h.Sum64()%(uint64(cfg.Jitter)+1))
}

func refreshPublicObservation(ctx context.Context, task *model.Task, h *host.Handle, stream, initial bool, previous any) publicObservation {
	r := publicObservation{task: task}
	fail := func(code string, status int) publicObservation {
		r.err = service.TaskErrorWrapperLocal(errors.New("query the accepted task using X-Task-ID"), code, status)
		return r
	}
	if !initial {
		latest, _, err := management.Default.PublicTask(ctx, task.TaskID, task.UserId, "openai_responses", h)
		if err != nil {
			return fail("task_unavailable", 503)
		}
		task, r.task = latest, latest
	}
	r.terminal = task.Status == model.TaskStatusSuccess || task.Status == model.TaskStatusFailure
	if stream && task.Status != model.TaskStatusFailure {
		state, _ := previous.(publicProtocolState)
		batch, err := renderPublicEventBatch(ctx, h, task, state)
		if err != nil {
			return fail("task_render_failed", 500)
		}
		r.previous, r.batch = batch.State, &batch
		if r.terminal {
			r.response = publicTaskResponseBase(task)
		}
		return r
	}
	if r.terminal {
		out, err := publicResponse(ctx, h, task)
		if err != nil {
			return fail("task_render_failed", 500)
		}
		r.response = out
	}
	return r
}

func observePublicTask(c *gin.Context, task *model.Task, cfg constant.TaskProtocolConfig, refresh func(context.Context, bool, any) publicObservation) *dto.TaskError {
	ctx, cancel := context.WithTimeout(c.Request.Context(), cfg.Timeout)
	defer cancel()
	var writer *publicEventWriter
	var outputs publicStreamOutputs
	if task.PrivateData.PluginContext.PublicMode == "stream" {
		c.Header("Content-Type", "text/event-stream")
		c.Header("Cache-Control", "no-cache")
		c.Header("X-Accel-Buffering", "no")
		writer = &publicEventWriter{c: c}
		initial := publicTaskProgressResponse(task)
		if writer.send("response.created", map[string]any{"response": initial}) != nil {
			return nil
		}
	}
	// One worker owns all DB/JS work and prior renderer state. It never touches
	// Gin. Every read/render has a deadline and no refreshes can overlap.
	results, done := make(chan publicObservation), make(chan struct{})
	go func() {
		defer close(done)
		var previous any
		for tick := uint64(0); ; tick++ {
			r := runPublicRefresh(ctx, publicReadTimeout(cfg), tick == 0, previous, refresh)
			select {
			case results <- r:
			case <-ctx.Done():
				return
			}
			if r.err != nil || r.terminal {
				return
			}
			previous = r.previous
			timer := time.NewTimer(publicTickDelay(cfg, task.TaskID, tick))
			select {
			case <-timer.C:
			case <-ctx.Done():
				timer.Stop()
				return
			}
		}
	}()
	defer func() { cancel(); <-done }()
	var heartbeat <-chan time.Time
	if writer != nil {
		ticker := time.NewTicker(cfg.Heartbeat)
		defer ticker.Stop()
		heartbeat = ticker.C
	}
	failure := func(err *dto.TaskError) *dto.TaskError {
		if writer == nil {
			return err
		}
		if c.Request.Context().Err() == nil {
			_ = writer.send("error", publicTaskErrorFields(err.Code, err.StatusCode, task.TaskID))
		}
		return nil
	}
	for {
		select {
		case <-ctx.Done():
			return failure(service.TaskErrorWrapperLocal(errors.New("task continues; query using X-Task-ID"), "task_wait_timeout", 504))
		case <-heartbeat:
			// A comment line needs no blank event delimiter. The pinned official
			// SDK treats a blank delimiter after a comment as an empty JSON event.
			if _, err := fmt.Fprint(c.Writer, ": heartbeat\n"); err != nil {
				return nil
			}
			if flushPublicWriter(c.Writer) != nil {
				return nil
			}
		case r := <-results:
			if ctx.Err() != nil {
				return failure(service.TaskErrorWrapperLocal(errors.New("task continues; query using X-Task-ID"), "task_wait_timeout", 504))
			}
			if r.err != nil {
				return failure(r.err)
			}
			if writer != nil && r.batch != nil {
				items, progress, err := outputs.apply(r.task, *r.batch)
				if err != nil {
					return failure(service.TaskErrorWrapperLocal(err, "task_render_failed", 500))
				}
				if writer.writeOutputs(items) != nil {
					return nil
				}
				r.progress = progress
				if r.terminal {
					r.response["output"] = outputs.items
					r.response["metadata"] = publicTaskMetadata(r.task, outputs.metadata)
				}
			}
			if r.terminal {
				if writer != nil {
					_ = writer.finish(r.response)
				} else {
					c.JSON(http.StatusOK, r.response)
				}
				return nil
			}
			if writer != nil && r.progress {
				response := publicTaskProgressResponse(r.task)
				response["metadata"] = publicTaskMetadata(r.task, outputs.metadata)
				if writer.send("response.in_progress", map[string]any{"response": response}) != nil {
					return nil
				}
			}
		}
	}
}
