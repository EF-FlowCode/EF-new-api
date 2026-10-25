package controller

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// Public messages are an allowlist, never interpolated provider/SQL/JS errors.
// No retryable/refunded flag is inferred from an HTTP status code.
func publicTaskErrorFields(code string, status int, taskID string) map[string]any {
	typ, message, state, action := "server_error", "The task request could not be completed. Contact the administrator before submitting again.", "unknown", "contact_administrator"
	switch code {
	case "task_observation_limit":
		typ, message, state, action = "rate_limit_error", "Too many task response connections. This request was not submitted; try again later.", "not_submitted", "retry_later"
	case "task_route_unavailable", "task_observation_unavailable":
		typ, message, state = "server_error", "The task service is unavailable. This request was not submitted.", "not_submitted"
	case "plugin_submission_unknown":
		typ, message, state, action = "task_submission_error", "Task submission is recorded but provider acceptance is not confirmed. Retrieve the task; reuse the original Idempotency-Key if repeating the request. Do not submit with a new key.", "unknown", "retrieve_task"
	case "plugin_submission_failed":
		typ, message, state, action = "task_execution_error", "The original task failed. This Idempotency-Key cannot create another task. Retrieve the task for its status.", "failed", "retrieve_task"
	case "plugin_not_sent":
		typ, message, state, action = "task_submission_error", "The task was not sent to the provider. Retrieve the recorded task to check its status before submitting again.", "not_sent", "retrieve_task"
	case "plugin_idempotency_conflict":
		typ = "task_submission_error"
		if status == http.StatusConflict {
			action = "check_request"
			message = "The Idempotency-Key belongs to a different request. Check the original request before submitting again."
		} else {
			message = "The original request could not be checked. Retry with the same Idempotency-Key; do not use a new key."
			action = "reuse_idempotency_key"
		}
	case "task_wait_timeout":
		typ, message, state, action = "task_observation_error", "Waiting for the task timed out. The recorded task is not canceled. Retrieve its status; do not submit it again.", "recorded", "retrieve_task"
	case "task_unavailable", "task_render_failed", "render_failed":
		typ, message, state, action = "task_observation_error", "The task status or result could not be read. This does not mean the task failed or was refunded. Retrieve it later; do not submit it again.", "recorded", "retrieve_task"
	case "response_not_found":
		typ, message, state, action = "not_found_error", "The response does not exist or is not accessible to this account.", "unknown", "check_request"
	case "missing_model":
		typ, message, state, action = "invalid_request_error", "The model field is required.", "not_submitted", "check_request"
	case "plugin_idempotency_invalid":
		typ, message, state, action = "invalid_request_error", "Idempotency-Key must be at most 191 bytes and must not contain CR, LF or NUL.", "not_submitted", "check_request"
	case "plugin_request_body_invalid", "plugin_protocol_invalid", "plugin_protocol_unsupported", "plugin_origin_invalid", "plugin_usage_invalid":
		typ, message, state, action = "invalid_request_error", "The task request has invalid or unsupported fields. Check the model, input, protocol flags and task references.", "not_submitted", "check_request"
	case "plugin_reservation_denied":
		typ, message, state, action = "permission_error", "The wallet or token cannot reserve quota for this task. Check the available balance and token permissions.", "not_submitted", "check_request"
	default:
		switch status {
		case 400, 413, 422:
			typ, message, action = "invalid_request_error", "The task request cannot be processed. Check the request and model configuration.", "check_request"
		case 401:
			typ, message, action = "authentication_error", "Authentication failed. Check the API token.", "check_request"
		case 403:
			typ, message, action = "permission_error", "The task request is not permitted. Check token and model permissions.", "check_request"
		case 429:
			typ, message = "rate_limit_error", "The task service is busy. Check any recorded task before submitting again."
		}
	}
	fields := map[string]any{"code": code, "type": typ, "message": message, "param": nil, "submission_status": state, "next_action": action}
	if code == "missing_model" {
		fields["param"] = "model"
	}
	if taskID == "" && state == "recorded" {
		fields["submission_status"] = "unknown"
	}
	if taskID != "" {
		fields["task_id"], fields["retrieval_path"] = taskID, publicTaskRetrievalPath(taskID)
		// A durable identity must never be presented as proof of no submission.
		if state == "not_submitted" {
			fields["submission_status"] = "recorded"
			fields["next_action"] = "retrieve_task"
			fields["message"] = "A task is already recorded. Retrieve its status before submitting again."
		}
	}
	return fields
}

func respondPublicTaskError(c *gin.Context, status int, code string) {
	if status < 400 || status > 599 {
		status = http.StatusInternalServerError
	}
	c.JSON(status, gin.H{"error": publicTaskErrorFields(code, status, c.Writer.Header().Get("X-Task-ID"))})
}
