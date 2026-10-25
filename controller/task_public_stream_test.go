package controller

import (
	"context"
	"eflow.local/new-api/constant"
	"eflow.local/new-api/model"
	"eflow.local/new-api/plugins"
	host "eflow.local/new-api/relay/channel/task/jsplugin"
	"encoding/json"
	"github.com/gin-gonic/gin"
	"github.com/openai/openai-go/responses"
	"github.com/stretchr/testify/require"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func publicStreamFixture(t *testing.T, hook string, only bool) *host.Handle {
	t.Helper()
	source, err := plugins.Source("doubao")
	require.NoError(t, err)
	if only {
		source = strings.Replace(source, `["stream", "sync", "background"]`, `["stream"]`, 1)
		source += "\ndelete protocols.openai_responses.renderFinal;"
	}
	source += "\nprotocols.openai_responses.renderEvents = function(ctx, task, previous) {" + hook + "};"
	h, err := host.Compile(source)
	require.NoError(t, err)
	return h
}

func TestPublicStreamStateArguments(t *testing.T) {
	task := observationTask("stream")
	for _, tc := range []struct{ name, state, check string }{
		{"omitted", "", `arguments.length !== 2`},
		{"null", ",state:null", `arguments.length !== 3 || previous !== null`},
		{"value", ",state:{seen:1}", `arguments.length !== 3 || previous.seen !== 1`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			producer := publicStreamFixture(t, `return {events:[],done:false`+tc.state+`};`, true)
			first := refreshPublicObservation(t.Context(), task, producer, true, true, nil)
			require.Nil(t, first.err)
			consumer := publicStreamFixture(t, `if (`+tc.check+`) throw new Error("wrong argument"); if (previous) previous.seen = 9; return {events:[],done:false};`, true)
			second := refreshPublicObservation(t.Context(), task, consumer, true, true, first.previous)
			require.Nil(t, second.err)
			// Mutating the hook argument cannot mutate the saved, detached state.
			again := refreshPublicObservation(t.Context(), task, consumer, true, true, first.previous)
			require.Nil(t, again.err)
			require.False(t, second.previous.(publicProtocolState).Present, "omission replaces, rather than retains, the previous state")
		})
	}
}

func TestPublicStreamAtomicBoundsAndLifecycle(t *testing.T) {
	task := observationTask("stream")
	decode := func(events []any, done bool) publicEventBatch {
		b, err := decodePublicEventBatch(map[string]any{"events": events, "done": done}, defaultPublicProtocolLimits())
		require.NoError(t, err)
		return b
	}
	output := func(text string) any { return map[string]any{"type": "output", "data": text} }
	for _, status := range []model.TaskStatus{model.TaskStatusSubmitted, model.TaskStatusBillingPending} {
		task.Status = status
		var s publicStreamOutputs
		_, _, err := s.apply(task, decode([]any{output("early")}, true))
		require.Error(t, err)
		require.Empty(t, s.items)
		require.Equal(t, status, task.Status)
	}
	var s publicStreamOutputs
	_, _, err := s.apply(task, decode([]any{output("discard me"), map[string]any{"type": "error", "message": "SECRET", "code": "SECRET"}}, false))
	require.Error(t, err)
	require.NotContains(t, err.Error(), "SECRET")
	require.Empty(t, s.items)
	for range 64 {
		_, _, err = s.apply(task, decode([]any{output("x")}, false))
		require.NoError(t, err)
	}
	_, _, err = s.apply(task, decode([]any{output("overflow")}, false))
	require.Error(t, err)
	require.Len(t, s.items, 64)
	require.Equal(t, 64, s.bytes)
	// MaxOutputs is inclusive also when several outputs share one tick:
	// 4 full ticks reach exactly 64 items, and the rejected tick is atomic.
	tick := make([]any, 16)
	for i := range tick {
		tick[i] = output("y")
	}
	var batched publicStreamOutputs
	for range 4 {
		added, _, err := batched.apply(task, decode(tick, false))
		require.NoError(t, err)
		require.Len(t, added, 16)
	}
	_, _, err = batched.apply(task, decode([]any{output("overflow")}, false))
	require.Error(t, err)
	require.Len(t, batched.items, defaultPublicProtocolLimits().MaxOutputs)
	var total publicStreamOutputs
	large := decode([]any{output(strings.Repeat("x", 30<<10))}, false)
	for range 34 {
		_, _, err = total.apply(task, large)
		require.NoError(t, err)
	}
	_, _, err = total.apply(task, large)
	require.Error(t, err)
	require.Len(t, total.items, 34)
	require.Equal(t, 34*(30<<10), total.bytes)
}

func TestPublicStreamSDKObserverAndRetrieval(t *testing.T) {
	for _, fixture := range []string{"only_stream", "doubao", "kling", "empty", "early_done", "error", "failure"} {
		t.Run(fixture, func(t *testing.T) {
			hook := `if (task.status === "SUCCESS") return {events:[{type:"output",data: arguments.length === 2 ? "first\n" : ""},{type:"output",data:{last:true}}],done:true}; if (previous) return {events:[{type:"progress",progress:50,message:"working"}],state:previous,done:false}; return {events:[{type:"output",data:"first\n"}],state:{seen:1},done:false};`
			switch fixture {
			case "empty":
				hook = `return {events:[],done:task.status === "SUCCESS"};`
			case "early_done":
				hook = `return {events:[{type:"output",data:"must not leak"}],done:true};`
			case "error":
				hook = `return {events:[{type:"output",data:"must not leak"},{type:"error",message:"SECRET",code:"SECRET"}],done:false};`
			case "failure":
				hook = `throw new Error("SECRET");`
			}
			var h *host.Handle
			if fixture == "doubao" || fixture == "kling" {
				source, err := plugins.Source(fixture)
				require.NoError(t, err)
				h, err = host.Compile(source)
				require.NoError(t, err)
			} else {
				h = publicStreamFixture(t, hook, true)
			}
			// Each callback owns a fresh task snapshot, as the real DB refresh does.
			snapshot := func(status model.TaskStatus) *model.Task {
				task := observationTask("stream")
				task.Status = status
				return task
			}
			srv := httptest.NewServer(http.HandlerFunc(func(raw http.ResponseWriter, req *http.Request) {
				c, _ := gin.CreateTestContext(raw)
				c.Request = req
				if req.Method == "GET" {
					out, err := publicResponse(req.Context(), h, snapshot(model.TaskStatusSuccess))
					if err != nil {
						http.Error(raw, "render failed", 500)
						return
					}
					c.JSON(200, out)
					return
				}
				cfg := constant.TaskProtocolConfig{Timeout: time.Second, Tick: time.Millisecond, Heartbeat: 100 * time.Millisecond}
				tick := 0
				_ = observePublicTask(c, snapshot(model.TaskStatusSubmitted), cfg, func(ctx context.Context, initial bool, previous any) publicObservation {
					status := model.TaskStatus(model.TaskStatusBillingPending)
					if tick == 2 {
						status = model.TaskStatusSuccess
					}
					if fixture == "failure" {
						status = model.TaskStatusFailure
					}
					tick++
					return refreshPublicObservation(ctx, snapshot(status), h, true, true, previous)
				})
			}))
			defer srv.Close()
			client := sdkTaskClient(srv)
			stream := client.Responses.NewStreaming(t.Context(), sdkTaskParams(false))
			defer stream.Close()
			var final responses.Response
			seq, completed, failures, observationErrors := int64(0), 0, 0, 0
			var deltas strings.Builder
			for stream.Next() {
				event := stream.Current()
				require.Equal(t, seq, event.SequenceNumber)
				seq++
				require.NotContains(t, event.RawJSON(), "SECRET")
				require.NotContains(t, event.RawJSON(), "must not leak")
				switch event.Type {
				case "response.output_text.delta":
					var wire map[string]any
					require.NoError(t, json.Unmarshal([]byte(event.RawJSON()), &wire))
					deltas.WriteString(wire["delta"].(string))
				case "response.in_progress":
					require.Empty(t, event.Response.Output)
				case "response.completed":
					final = event.Response
					completed++
				case "response.failed":
					failures++
				case "error":
					observationErrors++
					require.Contains(t, event.RawJSON(), "retrieve_task")
				}
			}
			require.NoError(t, stream.Err())
			if fixture == "early_done" || fixture == "error" {
				require.Equal(t, 1, observationErrors)
				require.Zero(t, completed)
				return
			}
			if fixture == "failure" {
				require.Equal(t, 1, failures)
				require.Zero(t, observationErrors)
				return
			}
			require.Equal(t, 1, completed)
			require.Equal(t, final.OutputText(), deltas.String(), "no output is emitted again at completion")
			fetched, err := client.Responses.Get(t.Context(), "observe", responses.ResponseGetParams{})
			require.NoError(t, err)
			require.Equal(t, final.OutputText(), fetched.OutputText())
			if fixture == "only_stream" {
				require.Equal(t, "first\n{\"last\":true}", deltas.String())
				require.Equal(t, "50", final.Metadata["task_progress"])
			}
			if fixture == "empty" {
				require.Empty(t, final.Output)
			}
			require.Equal(t, "observe", final.ID)
		})
	}
}

func TestPublicStreamOnlyDelivery(t *testing.T) {
	h := publicStreamFixture(t, `if (arguments.length !== 2) throw new Error("unexpected state"); return {events:[{type:"output",data:"verbatim"}],done:true};`, true)
	task := observationTask("stream")
	task.Status = model.TaskStatusSuccess
	out, err := publicResponse(t.Context(), h, task)
	require.NoError(t, err)
	require.Contains(t, out, "output")
	r := refreshPublicObservation(t.Context(), task, h, true, true, nil)
	require.Nil(t, r.err)
	require.True(t, r.terminal)
}

func TestPublicStreamDurableFailureSkipsHook(t *testing.T) {
	h := publicStreamFixture(t, `throw new Error("SECRET");`, false)
	task := observationTask("stream")
	task.Status = model.TaskStatusFailure
	task.FailReason = "SECRET"
	r := refreshPublicObservation(t.Context(), task, h, true, true, nil)
	require.Nil(t, r.err)
	require.True(t, r.terminal)
	require.Equal(t, "failed", r.response["status"])
}
