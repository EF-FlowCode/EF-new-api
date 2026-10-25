package service

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"time"

	"eflow.local/new-api/common"
	"eflow.local/new-api/constant"
	"eflow.local/new-api/model"
	management "eflow.local/new-api/service/taskplugin"
)

type PluginReviewRequest struct {
	Revision   uint64                   `json:"revision"`
	Action     string                   `json:"action"`
	UpstreamID string                   `json:"upstream_id,omitempty"`
	Seconds    *float64                 `json:"seconds,omitempty"`
	Resolution string                   `json:"resolution,omitempty"`
	Billing    *model.TaskPluginBilling `json:"billing,omitempty"`
	Quota      *int                     `json:"quota,omitempty"` // reviewed total when facts cannot price a pending task; <= reservation
	Reason     string                   `json:"reason"`
	Evidence   string                   `json:"evidence"`
	Confirmed  bool                     `json:"confirmed"`
}

type PluginReviewDetail struct {
	TokenBilling     *model.TaskPluginTokenBilling `json:"token_billing,omitempty"`
	CompletionTokens *int                          `json:"completion_tokens,omitempty"`
	ID               int64                         `json:"id"`
	TaskID           string                        `json:"task_id"`
	Status           model.TaskStatus              `json:"status"`
	Revision         uint64                        `json:"revision"`
	Quota            int                           `json:"quota"`
	Reason           string                        `json:"reason"`
	Billing          *model.TaskPluginBilling      `json:"billing"`
	Seconds          any                           `json:"seconds"`
	Resolution       any                           `json:"resolution"`
	Ready            bool                          `json:"ready"`
	ManualQuota      bool                          `json:"manual_quota"`
	EditSeconds      bool                          `json:"edit_seconds"`
	EditResolution   bool                          `json:"edit_resolution"`
	LockedUntil      int64                         `json:"locked_until"`
	Audits           []model.TaskPluginReview      `json:"audits,omitempty"`
}

func PluginTaskReviewDetail(ctx context.Context, id int64) (PluginReviewDetail, error) {
	var task model.Task
	if err := model.DB.WithContext(ctx).First(&task, id).Error; err != nil {
		return PluginReviewDetail{}, err
	}
	if task.PrivateData.Plugin == nil {
		return PluginReviewDetail{}, model.ErrTaskPluginIdentity
	}
	out := PluginReviewDetail{ID: id, TaskID: task.TaskID, Status: task.Status, Revision: task.Revision, Quota: task.Quota}
	if pc := task.PrivateData.PluginContext; pc != nil {
		out.Reason = pc.BillingPendingReason
		out.Billing = pc.Billing
		out.TokenBilling, out.CompletionTokens = pc.TokenBilling, pc.CompletionTokens
		out.Ready = pc.CompletionReady
		out.EditSeconds, out.EditResolution = editableCompletionFacts(pc)
		if task.Status == model.TaskStatusBillingPending {
			_, reason := pluginCompletionQuota(&task)
			out.ManualQuota = reason != ""
		}
		out.Seconds = pc.CompletionUsage["seconds"]
		out.Resolution = pc.CompletionUsage["resolution"]
	}
	if task.Status == model.TaskStatusSubmitting || task.Status == model.TaskStatusSubmitUnknown {
		if sub, err := model.GetPluginSubmission(ctx, id); err == nil {
			out.LockedUntil = sub.Deadline
		}
	}
	var err error
	out.Audits, err = model.ReadPluginReviews(ctx, id)
	return out, err
}

func editableCompletionFacts(pc *model.TaskPluginContext) (bool, bool) {
	if pc.CompletionReady || (pc.TokenBilling != nil && pc.Billing == nil) {
		return false, false
	}
	seconds, ok := pc.CompletionUsage["seconds"].(float64)
	editSeconds := pc.CompletionUsageInvalid || !ok || math.IsNaN(seconds) || math.IsInf(seconds, 0) || seconds <= 0 || seconds > 3600
	resolution, ok := pc.CompletionUsage["resolution"].(string)
	editResolution := pc.CompletionUsageInvalid || !ok || strings.TrimSpace(resolution) == ""
	if pc.Billing != nil && pc.Billing.Mode == "duration" {
		_, known := pc.Billing.ResolutionRatios[strings.ToUpper(strings.TrimSpace(resolution))]
		editResolution = editResolution || !known
	}
	return editSeconds, editResolution
}

func prepareReviewedTask(task *model.Task, req PluginReviewRequest) error {
	if !req.Confirmed || strings.TrimSpace(req.Reason) == "" || strings.TrimSpace(req.Evidence) == "" || len(req.Reason) > 1000 || len(req.Evidence) > 4000 {
		return errors.New("review requires confirmation, reason and evidence")
	}
	pc := task.PrivateData.PluginContext
	if pc == nil {
		return model.ErrTaskPluginIdentity
	}
	switch req.Action {
	case "refund":
		switch task.Status {
		case model.TaskStatusSubmitUnknown, model.TaskStatusSubmitting:
			task.FailReason = "Provider non-acceptance confirmed by review"
		case model.TaskStatusBillingPending:
			// Releases the whole reservation. The task row is locked and
			// revision-checked, and automatic settlement only moves a task out of
			// BillingPending, so the two can never both apply.
			pc.BillingPendingReason, pc.BillingRetryAfter = "", 0
			task.FailReason = "Pending settlement refunded by review"
		default:
			return model.ErrPluginSubmissionConflict
		}
		task.Status, task.Progress, task.Quota, task.FinishTime = model.TaskStatusFailure, "100%", 0, time.Now().Unix()
	case "attach":
		if task.Status != model.TaskStatusSubmitUnknown || strings.TrimSpace(req.UpstreamID) == "" || len(req.UpstreamID) > 2048 {
			return model.ErrPluginSubmissionConflict
		}
		task.PrivateData.UpstreamTaskID = req.UpstreamID
		task.Status = model.TaskStatusSubmitted
	case "settle":
		if task.Status != model.TaskStatusBillingPending {
			return model.ErrPluginSubmissionConflict
		}
		if pc.CompletionReady && (req.Seconds != nil || req.Resolution != "" || req.Billing != nil || req.Quota != nil) {
			return errors.New("verified completion facts cannot be replaced")
		}
		if req.Quota != nil {
			// Only when the recorded facts cannot produce a quota (e.g. an
			// implausible or overflowing token count); never above the reservation.
			if req.Seconds != nil || req.Resolution != "" || req.Billing != nil {
				return errors.New("reviewed quota replaces other completion facts")
			}
			if _, reason := pluginCompletionQuota(task); reason == "" {
				return errors.New("computed completion quota cannot be replaced")
			}
			if *req.Quota < 0 || *req.Quota > task.Quota {
				return errors.New("reviewed quota must be between 0 and the reserved quota")
			}
			pc.CompletionReady, pc.BillingPendingReason, pc.BillingRetryAfter = true, "", 0
			task.Status, task.Progress, task.Quota = model.TaskStatusSuccess, "100%", *req.Quota
			return nil
		}
		if req.Billing != nil {
			if pc.Billing != nil || pc.TokenBilling != nil {
				return errors.New("recorded price snapshot cannot be replaced")
			}
			pc.Billing = req.Billing
		}
		if req.Seconds != nil || req.Resolution != "" {
			editSeconds, editResolution := editableCompletionFacts(pc)
			if pc.CompletionUsage == nil {
				pc.CompletionUsage = map[string]any{}
			}
			if req.Seconds != nil {
				if !editSeconds {
					return errors.New("recorded usage cannot be replaced")
				}
				pc.CompletionUsage["seconds"] = *req.Seconds
			}
			if req.Resolution != "" {
				if !editResolution {
					return errors.New("recorded resolution cannot be replaced")
				}
				pc.CompletionUsage["resolution"] = req.Resolution
			}
			pc.CompletionUsageInvalid = false
		}
		quota, reason := pluginCompletionQuota(task)
		if reason != "" {
			return errors.New(reason)
		}
		pc.CompletionReady, pc.BillingPendingReason, pc.BillingRetryAfter = true, "", 0
		task.Status, task.Progress, task.Quota = model.TaskStatusSuccess, "100%", quota
	default:
		return errors.New("unknown review action")
	}
	return nil
}

func PreviewPluginTaskReview(ctx context.Context, m *management.Manager, actor int, id int64, req PluginReviewRequest) (model.TaskPluginReview, error) {
	_, task, err := m.StoredSubmitHandle(ctx, id)
	if err != nil {
		return model.TaskPluginReview{}, err
	}
	if req.Revision != task.Revision {
		return model.TaskPluginReview{}, model.ErrPluginSubmissionConflict
	}
	if task.Status == model.TaskStatusSubmitUnknown || task.Status == model.TaskStatusSubmitting {
		sub, err := model.GetPluginSubmission(ctx, id)
		if err != nil {
			return model.TaskPluginReview{}, err
		}
		if sub.Deadline > time.Now().Unix() {
			return model.TaskPluginReview{}, model.ErrPluginSubmissionConflict
		}
	}
	next, err := model.ClonePluginTask(task)
	if err != nil {
		return model.TaskPluginReview{}, err
	}
	if err := prepareReviewedTask(next, req); err != nil {
		return model.TaskPluginReview{}, err
	}
	if req.Action == "attach" {
		ch, err := model.GetChannelById(task.ChannelId, true)
		if err != nil {
			return model.TaskPluginReview{}, err
		}
		executor, err := NewTaskPluginHTTPExecutorForProxy(ch.GetSetting().Proxy)
		if err != nil {
			return model.TaskPluginReview{}, err
		}
		if ch.GetSetting().Proxy != "" {
			defer executor.Client.CloseIdleConnections()
		}
		base := ch.GetBaseURL()
		if base == "" {
			base = constant.GetChannelBaseURL(ch.Type)
		}
		env, err := PrepareTaskPluginAuth(ctx, ch.Type, base, task.PrivateData.Key, ch.Other, executor)
		if err != nil {
			return model.TaskPluginReview{}, err
		}
		if err := m.ProbeRecovery(ctx, id, req.UpstreamID, env, executor); err != nil {
			return model.TaskPluginReview{}, errors.New("provider task verification failed")
		}
	}
	payload, err := json.Marshal(req)
	if err != nil {
		return model.TaskPluginReview{}, err
	}
	review := model.TaskPluginReview{OperationID: common.GetUUID(), ActorID: actor, TaskID: id, Revision: task.Revision, Action: req.Action, Payload: string(payload), Reason: req.Reason, Evidence: req.Evidence, Expires: time.Now().Add(5 * time.Minute).Unix(), BeforeStatus: task.Status, AfterStatus: next.Status, BeforeQuota: task.Quota, AfterQuota: next.Quota, CreatedAt: time.Now().Unix()}
	err = model.CreatePluginReview(ctx, review)
	return review, err
}

func ConfirmPluginTaskReview(ctx context.Context, actor int, operation string) (*model.Task, error) {
	return model.ApplyPluginReview(ctx, actor, operation, func(task *model.Task, review model.TaskPluginReview) error {
		var req PluginReviewRequest
		if err := json.Unmarshal([]byte(review.Payload), &req); err != nil {
			return err
		}
		return prepareReviewedTask(task, req)
	})
}
