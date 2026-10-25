package relay

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"eflow.local/new-api/common"
	"eflow.local/new-api/constant"
	"eflow.local/new-api/dto"
	"eflow.local/new-api/model"
	host "eflow.local/new-api/relay/channel/task/jsplugin"
	relaycommon "eflow.local/new-api/relay/common"
	"eflow.local/new-api/service"
	management "eflow.local/new-api/service/taskplugin"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func pluginRequestIdentity(c *gin.Context, info *relaycommon.RelayInfo, body map[string]any, files map[string]host.File) (string, string, error) {
	key := c.GetHeader("Idempotency-Key")
	if len(key) > 191 || strings.ContainsAny(key, "\r\n\x00") {
		return "", "", errors.New("invalid Idempotency-Key")
	}
	digests := map[string]any{}
	for name, file := range files {
		digests[name] = []any{file.Name, file.ContentType, fmt.Sprintf("%x", sha256.Sum256(file.Data))}
	}
	identity := []any{info.OriginModelName, c.Request.URL.Path, body, digests}
	if route := service.TaskPublicRoute(c); route != nil && route.Protocol == "native" {
		identity = append(identity, c.Request.URL.Query())
	}
	raw, err := json.Marshal(identity)
	if err != nil {
		return "", "", err
	}
	scope := ""
	if key != "" {
		scope = model.PluginSubmissionScope(info.UserId, info.TokenId, c.Request.URL.Path, key)
	}
	return scope, fmt.Sprintf("%x", sha256.Sum256(raw)), nil
}

func pluginSavedResult(c *gin.Context, task *model.Task, replay bool) (*TaskSubmitResult, *dto.TaskError) {
	c.Header("X-Task-ID", task.TaskID)
	if task.Status == model.TaskStatusSubmitting || task.Status == model.TaskStatusSubmitUnknown {
		e := service.TaskErrorWrapperLocal(errors.New("submission outcome is pending reconciliation; reuse the same Idempotency-Key"), "plugin_submission_unknown", http.StatusConflict)
		e.SkipRetry = true
		e.Data = map[string]any{"task_id": task.TaskID}
		return nil, e
	}
	if task.Status == model.TaskStatusFailure {
		e := service.TaskErrorWrapperLocal(errors.New("original task failed; this key cannot create a new task"), "plugin_submission_failed", http.StatusConflict)
		e.SkipRetry = true
		e.Data = map[string]any{"task_id": task.TaskID}
		return nil, e
	}
	return &TaskSubmitResult{SavedTask: task, Replayed: replay, UpstreamTaskID: task.PrivateData.UpstreamTaskID, TaskData: task.Data, Platform: task.Platform, Quota: task.Quota, Plugin: task.PrivateData.Plugin, PluginContext: task.PrivateData.PluginContext}, nil
}

func replayPluginSubmission(c *gin.Context, info *relaycommon.RelayInfo, body map[string]any, files map[string]host.File) (*TaskSubmitResult, *dto.TaskError, bool) {
	scope, fingerprint, err := pluginRequestIdentity(c, info, body, files)
	if err != nil {
		return nil, service.TaskErrorWrapperLocal(err, "plugin_idempotency_invalid", 400), true
	}
	if scope == "" {
		return nil, nil, false
	}
	task, err := model.FindPluginSubmission(c.Request.Context(), scope, fingerprint)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil, false
	}
	if err != nil {
		status := 503
		if errors.Is(err, model.ErrPluginSubmissionConflict) {
			status = 409
		}
		return nil, service.TaskErrorWrapperLocal(errors.New("idempotency lookup failed or request conflicts"), "plugin_idempotency_conflict", status), true
	}
	result, e := pluginSavedResult(c, task, true)
	return result, e, true
}

func submitDurablePluginTask(c *gin.Context, info *relaycommon.RelayInfo, platform constant.TaskPlatform, manager *management.Manager, prepared *management.ProductionPrepared, executor host.Executor, files map[string]host.File, body map[string]any, billing *model.TaskPluginBilling) (*TaskSubmitResult, *dto.TaskError) {
	scope, fingerprint, err := pluginRequestIdentity(c, info, body, files)
	if err != nil {
		return nil, service.TaskErrorWrapperLocal(err, "plugin_idempotency_invalid", 400)
	}
	if scope == "" {
		scope = model.PluginSubmissionScope(info.UserId, info.TokenId, "generated", info.PublicTaskID)
	}
	task := model.InitTask(platform, info)
	task.Quota, task.Action = info.PriceData.Quota, info.Action
	task.PrivateData.TokenId = info.TokenId
	if info.IsPlayground {
		task.PrivateData.TokenId = 0
	}
	task.PrivateData.BillingContext = &model.TaskBillingContext{ModelPrice: info.PriceData.ModelPrice, GroupRatio: info.PriceData.GroupRatioInfo.GroupRatio, ModelRatio: info.PriceData.ModelRatio, OtherRatios: info.PriceData.OtherRatios, OriginModelName: info.OriginModelName, PerCallBilling: info.PriceData.TaskFixedTotal || info.PriceData.UsePrice || common.StringsContains(constant.TaskPricePatches, info.OriginModelName)}
	tokenBilling, err := service.NewTaskPluginTokenBilling(info.PriceData, common.StringsContains(constant.TaskPricePatches, info.OriginModelName))
	if err != nil {
		return nil, service.TaskErrorWrapperLocal(err, "plugin_token_price_snapshot_invalid", 400)
	}
	task.PrivateData.PluginContext = &model.TaskPluginContext{Usage: prepared.Facts, Billing: billing, Expression: info.TieredBillingSnapshot, TokenBilling: tokenBilling, PriceRule: info.PriceData.TaskRuleSnapshot}
	task.PrivateData.PluginContext.PublicProtocol, _ = prepared.Input["publicProtocol"].(string)
	task.PrivateData.PluginContext.NativePath, _ = prepared.Input["nativePath"].(string)
	task.PrivateData.PluginContext.NativeMethod, _ = prepared.Input["nativeMethod"].(string)
	task.PrivateData.PluginContext.PublicMode, _ = prepared.Input["publicMode"].(string)
	task.PrivateData.PluginContext.PublicBackground, _ = prepared.Input["publicBackground"].(bool)
	if prepared.Identity.Key == "jimeng" {
		var wire map[string]any
		if raw, ok := prepared.Descriptor.Body.(string); ok {
			_ = json.Unmarshal([]byte(raw), &wire)
		} else {
			raw, _ := json.Marshal(prepared.Descriptor.Body)
			_ = json.Unmarshal(raw, &wire)
		}
		if key, ok := wire["req_key"].(string); ok && key != "" {
			task.PrivateData.PluginContext.State = map[string]any{"req_key": key}
		}
	}
	input, err := json.Marshal(prepared.Input)
	if err != nil {
		return nil, service.TaskErrorWrapperLocal(err, "plugin_prepare_failed", 400)
	}
	task, created, err := manager.BeginSubmission(c.Request.Context(), *task, prepared, model.TaskPluginSubmission{ScopeKey: scope, Fingerprint: fingerprint, ParseInput: string(input)})
	if err != nil {
		status := 503
		if errors.Is(err, model.ErrPluginSubmissionConflict) {
			status = 409
		}
		if errors.Is(err, model.ErrPluginReservationDenied) {
			return nil, service.TaskErrorWrapperLocal(err, "plugin_reservation_denied", 403)
		}
		return nil, service.TaskErrorWrapperLocal(errors.New("cannot reserve plugin task"), "plugin_reservation_failed", status)
	}
	if !created {
		return pluginSavedResult(c, task, true)
	}
	info.PublicTaskID = task.TaskID
	c.Header("X-Task-ID", task.TaskID)
	// The durable reservation owns all subsequent money movements, including
	// cancellation. It is intentionally not a process-local BillingSession.
	info.Billing = nil
	if err := prepared.ValidateExecution(c.Request.Context()); err != nil {
		finishCtx, cancel := context.WithTimeout(context.WithoutCancel(c.Request.Context()), 10*time.Second)
		defer cancel()
		_ = model.RejectUnsentPluginSubmission(finishCtx, task.ID)
		return nil, service.TaskErrorWrapperLocal(errors.New("plugin task was not sent"), "plugin_not_sent", 503)
	}
	if err := model.ClaimPluginSubmission(c.Request.Context(), task.ID); err != nil {
		return pluginSavedResult(c, task, true)
	}
	response, sendErr := prepared.Execute(c.Request.Context(), executor, info.ChannelBaseUrl, files)
	finishCtx, cancel := context.WithTimeout(context.WithoutCancel(c.Request.Context()), 15*time.Second)
	defer cancel()
	if sendErr == nil {
		raw, marshalErr := json.Marshal(response)
		if marshalErr == nil {
			marshalErr = model.SavePluginSubmissionResponse(finishCtx, task.ID, string(raw))
		}
		if marshalErr == nil {
			if accepted, err := service.RecoverPluginSubmission(finishCtx, manager, task.ID); err == nil && accepted != nil {
				return pluginSavedResult(c, accepted, false)
			}
		}
	}
	task.Status = model.TaskStatusSubmitUnknown
	return pluginSavedResult(c, task, false)
}
