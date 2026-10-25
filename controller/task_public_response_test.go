package controller

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"

	"eflow.local/new-api/dto"
	"eflow.local/new-api/model"
	"eflow.local/new-api/plugins"
	host "eflow.local/new-api/relay/channel/task/jsplugin"
	management "eflow.local/new-api/service/taskplugin"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func publicFinalFixture(t *testing.T, payload map[string]any) *host.Handle {
	t.Helper()
	source, err := plugins.Source("doubao")
	require.NoError(t, err)
	raw, err := json.Marshal(payload)
	require.NoError(t, err)
	h, err := host.Compile(source + "\nprotocols.openai_responses.renderFinal = function () { return " + string(raw) + "; };")
	require.NoError(t, err)
	return h
}

func TestPublicResponseNormalization(t *testing.T) {
	task := observationTask("stream")
	task.Status = model.TaskStatusSuccess
	metadata := map[string]any{"task_id": "forged", "retrieval_path": "https://private.invalid", "task_status": "forged", "invalid": 42, "oversize": strings.Repeat("v", 513)}
	for i := range 20 {
		metadata[fmt.Sprintf("key%02d", i)] = "value"
	}
	for _, key := range []string{"annotations", "logprobs"} {
		for _, bad := range []any{nil, "wrong", 42, []any{42}} {
			part := map[string]any{"type": "output_text", "text": "hello", key: bad}
			h := publicFinalFixture(t, map[string]any{"output": []any{map[string]any{"type": "message", "role": "assistant", "content": []any{part}}}})
			_, err := publicResponse(t.Context(), h, task)
			require.Error(t, err)
		}
	}
	bounded := publicTaskMetadata(task, map[string]any{
		strings.Repeat("k", 65): "invalid", strings.Repeat("k", 64): "valid",
		"long_value": strings.Repeat("v", 513), "max_value": strings.Repeat("v", 512),
		"number": 42, "nested": map[string]any{"x": "y"},
	})
	require.Len(t, bounded, 5)
	require.Contains(t, bounded, strings.Repeat("k", 64))
	require.Len(t, bounded["max_value"], 512)
	part := map[string]any{"type": "output_text", "text": "hello"}
	h := publicFinalFixture(t, map[string]any{"id": "forged", "status": "failed", "usage": map[string]any{"total_tokens": 999}, "metadata": metadata, "output": []any{map[string]any{"type": "message", "role": "assistant", "content": []any{part}}}})
	out, err := publicResponse(t.Context(), h, task)
	require.NoError(t, err)
	require.Equal(t, task.TaskID, out["id"])
	require.Equal(t, "completed", out["status"])
	require.Nil(t, out["usage"])
	meta := out["metadata"].(map[string]string)
	require.Len(t, meta, 16)
	require.Equal(t, task.TaskID, meta["task_id"])
	require.Equal(t, "completed", meta["task_status"])
	require.NotContains(t, meta, "invalid")
	require.NotContains(t, meta, "oversize")
	item := out["output"].([]any)[0].(map[string]any)
	content := item["content"].([]any)[0].(map[string]any)
	require.Equal(t, []any{}, content["annotations"])
	require.Equal(t, []any{}, content["logprobs"])
	for _, status := range []model.TaskStatus{model.TaskStatusSubmitted, model.TaskStatusBillingPending, model.TaskStatusSuccess, model.TaskStatusFailure} {
		task.Status = status
		task.FailReason = "SECRET provider credential"
		base := publicTaskResponseBase(task)
		progress := publicTaskProgressResponse(task)
		for key := range base {
			require.Contains(t, progress, key)
		}
		require.True(t, base["background"].(bool))
		require.Equal(t, "in_progress", progress["status"])
		raw, err := json.Marshal(base)
		require.NoError(t, err)
		require.NotContains(t, string(raw), "SECRET")
		require.Equal(t, "SECRET provider credential", task.FailReason)
	}
}

func TestPublicErrorContract(t *testing.T) {
	for _, tc := range []struct {
		code              string
		status            int
		id, state, action string
	}{
		{"task_observation_limit", 429, "", "not_submitted", "retry_later"},
		{"plugin_submission_unknown", 409, "task1", "unknown", "retrieve_task"},
		{"plugin_submission_failed", 409, "task1", "failed", "retrieve_task"},
		{"plugin_not_sent", 503, "task1", "not_sent", "retrieve_task"},
		{"task_wait_timeout", 504, "task1", "recorded", "retrieve_task"},
		{"task_render_failed", 500, "task1", "recorded", "retrieve_task"},
		{"task_unavailable", 503, "", "unknown", "retrieve_task"},
		{"response_not_found", 404, "", "unknown", "check_request"},
		{"plugin_idempotency_conflict", 409, "", "unknown", "check_request"},
		{"plugin_idempotency_conflict", 503, "", "unknown", "reuse_idempotency_key"},
		{"missing_model", 400, "", "not_submitted", "check_request"},
	} {
		t.Run(tc.code, func(t *testing.T) {
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Set(management.PublicRouteContextKey, &management.PublicRoute{Protocol: "openai_responses"})
			if tc.id != "" {
				c.Header("X-Task-ID", tc.id)
			}
			e := &dto.TaskError{StatusCode: tc.status, Code: tc.code, Message: "SECRET provider key", Error: errors.New("SECRET SQL")}
			respondTaskError(c, e)
			require.Equal(t, tc.status, rec.Code)
			require.NotContains(t, rec.Body.String(), "SECRET")
			require.Equal(t, "SECRET provider key", e.Message)
			var body map[string]map[string]any
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
			require.Equal(t, tc.state, body["error"]["submission_status"])
			require.Equal(t, tc.action, body["error"]["next_action"])
			require.NotEmpty(t, body["error"]["message"])
			require.Contains(t, body["error"], "param")
			if tc.id != "" {
				require.Equal(t, publicTaskRetrievalPath(tc.id), body["error"]["retrieval_path"])
			}
		})
	}
}
