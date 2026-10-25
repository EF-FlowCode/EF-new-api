package service

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"eflow.local/new-api/common"
	"eflow.local/new-api/model"
	relaycommon "eflow.local/new-api/relay/common"
	management "eflow.local/new-api/service/taskplugin"
	"eflow.local/new-api/setting/billing_setting"
)

func TaskPluginRequiresCompletion(key, name string) bool {
	return key == "alibaba" && (name == "wan2.2-s2v" || name == "wan3.0-video" || name == "wan3.0-video-prime")
}

// Capture the already group-adjusted unit quota before duration multiplication.
// A configured model price is a per-second price for these WAN contracts;
// UsePrice alone does not mean that the total price is fixed.
func NewTaskPluginBillingSnapshot(key, name string, baseQuota int, fixedTotal, usePrice bool) (*model.TaskPluginBilling, error) {
	if !TaskPluginRequiresCompletion(key, name) {
		return nil, nil
	}
	if baseQuota < 0 {
		return nil, errors.New("invalid plugin unit price")
	}
	p := &model.TaskPluginBilling{Version: 1, Mode: "fixed", BaseQuota: baseQuota}
	if fixedTotal {
		return p, nil
	}
	if !usePrice {
		return nil, errors.New("WAN completion billing requires a configured per-second model price")
	}
	p.Mode = "duration"
	p.ResolutionRatios = map[string]float64{}
	resolutions := []string{"480P", "720P", "1080P"}
	if name == "wan2.2-s2v" {
		resolutions = resolutions[:2]
	}
	for _, resolution := range resolutions {
		ratio, ok := billing_setting.GetTaskVideoResolutionRatio(name, resolution)
		if !ok || math.IsNaN(ratio) || math.IsInf(ratio, 0) || ratio <= 0 {
			return nil, errors.New("invalid WAN resolution price")
		}
		p.ResolutionRatios[resolution] = ratio
	}
	return p, nil
}

// A provider-reported count above this bound is not a plausible single task;
// it is rejected with an explicit reason instead of overflowing the quota.
const maxTaskPluginCompletionTokens = 100_000_000

func pluginCompletionQuota(task *model.Task) (int, string) {
	pc := task.PrivateData.PluginContext
	if pc != nil && pc.Billing == nil && pc.TokenBilling != nil {
		if pc.CompletionTokens == nil {
			return 0, "missing_completion_tokens"
		}
		if pc.TokenBilling.Mode == "tokens" && *pc.CompletionTokens > maxTaskPluginCompletionTokens {
			return 0, "completion_tokens_exceed_limit"
		}
		quota, recalculate, err := taskCompletionTokenQuota(task, *pc.CompletionTokens)
		var clamp *common.QuotaClamp
		if errors.As(err, &clamp) {
			return 0, "completion_quota_overflow"
		}
		if err != nil {
			return 0, "invalid_token_price_snapshot"
		}
		if !recalculate {
			return task.Quota, ""
		}
		return quota, ""
	}
	if pc == nil || pc.Billing == nil || pc.Billing.Version != 1 || pc.Billing.BaseQuota < 0 {
		return 0, "missing_price_snapshot"
	}
	p := pc.Billing
	if p.Mode == "fixed" {
		return p.BaseQuota, ""
	}
	if p.Mode != "duration" {
		return 0, "invalid_price_snapshot"
	}
	if pc.CompletionUsageInvalid {
		return 0, "invalid_completion_usage"
	}
	seconds, ok := pc.CompletionUsage["seconds"].(float64)
	if !ok {
		return 0, "missing_completion_usage"
	}
	if math.IsNaN(seconds) || math.IsInf(seconds, 0) || seconds <= 0 || seconds > 3600 {
		return 0, "invalid_completion_usage"
	}
	resolutionValue, exists := pc.CompletionUsage["resolution"]
	if !exists {
		resolutionValue = pc.Usage["resolution"]
	}
	resolution, ok := resolutionValue.(string)
	if !ok {
		return 0, "invalid_completion_resolution"
	}
	ratio, ok := p.ResolutionRatios[strings.ToUpper(strings.TrimSpace(resolution))]
	if !ok {
		return 0, "invalid_completion_resolution"
	}
	quota, err := TaskPluginQuota(p.BaseQuota, map[string]float64{"seconds": seconds, "resolution": ratio})
	if err != nil {
		return 0, "invalid_completion_quota"
	}
	return quota, ""
}

func checkpointPluginCompletion(ctx context.Context, task *model.Task, from model.TaskStatus, result *relaycommon.TaskInfo) error {
	pc := task.PrivateData.PluginContext
	if pc == nil {
		pc = &model.TaskPluginContext{}
	}
	copy := *pc // Do not mutate another polling worker's copied snapshot.
	pc = &copy
	task.PrivateData.PluginContext = pc
	pc.CompletionUsage, pc.CompletionUsageInvalid = result.CompletionUsage, result.CompletionUsageInvalid
	if pc.TokenBilling != nil && pc.Billing == nil && pc.CompletionTokens == nil {
		tokens := result.TotalTokens
		pc.CompletionTokens = &tokens
	}
	_, reason := pluginCompletionQuota(task)
	pc.CompletionReady = reason == ""
	pc.BillingPendingReason = reason
	if pc.CompletionReady {
		pc.BillingPendingReason = "settlement_pending"
	}
	pc.BillingRetryAfter = time.Now().Add(time.Minute).Unix()
	task.Status, task.Progress = model.TaskStatusBillingPending, "99%"
	won, err := model.StorePluginBillingPending(ctx, task, from)
	if err != nil || !won {
		return err
	}
	if !pc.CompletionReady {
		return nil
	}
	return settlePluginCompletion(ctx, task)
}

func settlePluginCompletion(ctx context.Context, task *model.Task) error {
	quota, reason := pluginCompletionQuota(task)
	if reason != "" {
		return errors.New(reason)
	}
	// Move only the retry deadline on failures; valid completion facts and
	// the success checkpoint remain durable and cannot be replaced.
	retryAfter := time.Now().Add(time.Minute).Unix()
	if err := model.DeferPluginBillingRetry(ctx, task.ID, retryAfter); err != nil {
		return err
	}
	pc := *task.PrivateData.PluginContext
	task.PrivateData.PluginContext = &pc
	pc.BillingPendingReason, pc.BillingRetryAfter = "", 0
	task.Status, task.Progress = model.TaskStatusSuccess, "100%"
	reasonText := "插件完成用量结算（提交价格快照）"
	if pc.Billing == nil && pc.CompletionTokens != nil {
		reasonText += fmt.Sprintf("：tokens=%d", *pc.CompletionTokens)
	}
	_, err := FinalizeTaskWithBilling(ctx, task, model.TaskStatusBillingPending, quota, reasonText)
	if err != nil {
		task.Status, task.Progress = model.TaskStatusBillingPending, "99%"
		pc.BillingPendingReason = "settlement_pending"
		pc.BillingRetryAfter = retryAfter
		return fmt.Errorf("plugin completion settlement pending: %w", err)
	}
	return nil
}

func resumePluginCompletion(ctx context.Context, task *model.Task, ch *model.Channel, base string) error {
	// Stored completion facts are immutable: a quota that cannot be computed
	// now never will be. It waits for review instead of revalidating the
	// provider source every retry.
	if _, reason := pluginCompletionQuota(task); reason != "" {
		return errors.New(reason)
	}
	// Reuse source/reference and control-plane validation without OAuth or
	// provider HTTP: the immutable completion has already been saved.
	plan, err := management.Default.PrepareTaskQueries(ctx, []int64{task.ID}, map[int]management.TaskQueryEnvironment{ch.Id: {BaseURL: base}})
	if err != nil {
		return err
	}
	if len(plan.Queries) != 1 {
		return model.ErrTaskPluginIdentity
	}
	if err := plan.Queries[0].ValidateContent(ctx); err != nil {
		return err
	}
	return settlePluginCompletion(ctx, task)
}
