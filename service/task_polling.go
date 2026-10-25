package service

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"eflow.local/new-api/common"
	"eflow.local/new-api/constant"
	"eflow.local/new-api/dto"
	"eflow.local/new-api/logger"
	"eflow.local/new-api/model"
	pluginhost "eflow.local/new-api/relay/channel/task/jsplugin"
	"eflow.local/new-api/relay/channel/task/taskcommon"
	relaycommon "eflow.local/new-api/relay/common"
	management "eflow.local/new-api/service/taskplugin"
)

// sweepTimedOutTasks 在主轮询之前独立清理超时任务。
// 每次最多处理 100 条，剩余的下个周期继续处理。
// 使用 per-task CAS (UpdateWithStatus) 防止覆盖被正常轮询已推进的任务。
func sweepTimedOutTasks(ctx context.Context) {
	if constant.TaskTimeoutMinutes <= 0 {
		return
	}
	cutoff := time.Now().Unix() - int64(constant.TaskTimeoutMinutes)*60
	tasks := model.GetTimedOutUnfinishedTasks(cutoff, 100)
	if len(tasks) == 0 {
		return
	}

	const legacyTaskCutoff int64 = 1740182400 // 2026-02-22 00:00:00 UTC
	reason := fmt.Sprintf("任务超时（%d分钟）", constant.TaskTimeoutMinutes)
	legacyReason := "任务超时（旧系统遗留任务，不进行退款，请联系管理员）"
	now := time.Now().Unix()
	timedOutCount := 0

	for _, task := range tasks {
		isLegacy := task.SubmitTime > 0 && task.SubmitTime < legacyTaskCutoff

		oldStatus := task.Status
		task.Status = model.TaskStatusFailure
		task.Progress = "100%"
		task.FinishTime = now
		if isLegacy {
			task.FailReason = legacyReason
		} else {
			task.FailReason = reason
		}

		finalQuota := task.Quota
		if !isLegacy {
			finalQuota = 0
		}
		won, err := FinalizeTaskWithBilling(ctx, task, oldStatus, finalQuota, task.FailReason)
		if err != nil {
			logger.LogError(ctx, fmt.Sprintf("sweepTimedOutTasks CAS update error for task %s: %v", task.TaskID, err))
			continue
		}
		if !won {
			logger.LogInfo(ctx, fmt.Sprintf("sweepTimedOutTasks: task %s already transitioned, skip", task.TaskID))
			continue
		}
		timedOutCount++
	}

	if timedOutCount > 0 {
		logger.LogInfo(ctx, fmt.Sprintf("sweepTimedOutTasks: timed out %d tasks", timedOutCount))
	}
}

// TaskPollingLoop 按指定间隔检查未完成任务，直到 ctx 被取消。
func TaskPollingLoop(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = 15 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		if ctx.Err() != nil {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}

		common.SysLog("任务进度轮询开始")
		recoverPluginSubmissions(ctx)
		sweepTimedOutTasks(ctx)
		allTasks := model.GetAllUnFinishSyncTasks(constant.TaskQueryLimit)
		platformTask := make(map[constant.TaskPlatform][]*model.Task)
		for _, t := range allTasks {
			platformTask[t.Platform] = append(platformTask[t.Platform], t)
		}
		for platform, tasks := range platformTask {
			if len(tasks) == 0 {
				continue
			}
			taskChannelM := make(map[int][]string)
			taskM := make(map[string]*model.Task)
			for _, task := range tasks {
				upstreamID := task.GetUpstreamTaskID()
				if upstreamID == "" {
					if task.Status == model.TaskStatusBillingPending {
						_ = model.DeferPluginBillingRetry(ctx, task.ID, time.Now().Add(time.Minute).Unix())
						continue
					}
					oldStatus := task.Status
					task.Status = model.TaskStatusFailure
					task.Progress = "100%"
					task.FailReason = "任务缺少上游任务 ID"
					if _, err := FinalizeTaskWithBilling(ctx, task, oldStatus, 0, task.FailReason); err != nil {
						logger.LogError(ctx, fmt.Sprintf("修复缺少上游 ID 的任务失败 %d: %v", task.ID, err))
					}
					continue
				}
				mapKey := upstreamID
				if task.PrivateData.Plugin != nil {
					mapKey = task.TaskID
				}
				taskM[mapKey] = task
				taskChannelM[task.ChannelId] = append(taskChannelM[task.ChannelId], mapKey)
			}
			if len(taskChannelM) == 0 {
				continue
			}

			DispatchPlatformUpdate(platform, taskChannelM, taskM)
		}
		common.SysLog("任务进度轮询完成")
	}
}

// DispatchPlatformUpdate 按平台分发轮询更新
func DispatchPlatformUpdate(platform constant.TaskPlatform, taskChannelM map[int][]string, taskM map[string]*model.Task) {
	switch platform {
	case constant.TaskPlatformMidjourney:
		// MJ 轮询由其自身处理，这里预留入口
	case constant.TaskPlatformSuno:
		_ = UpdateVideoTasks(context.Background(), taskChannelM, taskM)
	default:
		if err := UpdateVideoTasks(context.Background(), taskChannelM, taskM); err != nil {
			common.SysLog(fmt.Sprintf("UpdateVideoTasks fail: %s", err))
		}
	}
}

// UpdateVideoTasks 按渠道更新所有视频任务
func UpdateVideoTasks(ctx context.Context, taskChannelM map[int][]string, taskM map[string]*model.Task) error {
	for channelId, taskIds := range taskChannelM {
		if err := updateVideoTasks(ctx, channelId, taskIds, taskM); err != nil {
			logger.LogError(ctx, fmt.Sprintf("Channel #%d failed to update video async tasks: %s", channelId, err.Error()))
		}
	}
	return nil
}

func updateVideoTasks(ctx context.Context, channelId int, taskIds []string, taskM map[string]*model.Task) error {
	logger.LogInfo(ctx, fmt.Sprintf("Channel #%d pending video tasks: %d", channelId, len(taskIds)))
	if len(taskIds) == 0 {
		return nil
	}
	cacheGetChannel, err := model.CacheGetChannel(channelId)
	if err != nil {
		// A missing channel is not proof the upstream job failed: it may be a
		// transient lookup error or a new channel not yet in this node's cache,
		// while the provider keeps generating (and billing) the job. Leave the
		// tasks in place; the timeout sweep fails and refunds them if the channel
		// never comes back.
		for _, upstreamID := range taskIds {
			if task, ok := taskM[upstreamID]; ok && task != nil && task.Status == model.TaskStatusBillingPending {
				_ = model.DeferPluginBillingRetry(ctx, task.ID, time.Now().Add(time.Minute).Unix())
			}
		}
		return fmt.Errorf("CacheGetChannel failed: %w", err)
	}
	if model.TaskPluginKeyForChannel(cacheGetChannel.Type) == "sunoapi" {
		return updatePluginBatchTasks(ctx, cacheGetChannel, taskIds, taskM)
	}
	for _, taskId := range taskIds {
		if err := updateVideoSingleTask(ctx, cacheGetChannel, taskId, taskM); err != nil {
			logger.LogError(ctx, fmt.Sprintf("Failed to update video task %s: %s", taskId, err.Error()))
		}
		// sleep 1 second between each task to avoid hitting rate limits of upstream platforms
		time.Sleep(1 * time.Second)
	}
	return nil
}

func updateVideoSingleTask(ctx context.Context, ch *model.Channel, taskId string, taskM map[string]*model.Task) error {
	executor, err := NewTaskPluginHTTPExecutorForProxy(ch.GetSetting().Proxy)
	if err != nil {
		return err
	}
	if ch.GetSetting().Proxy != "" {
		defer executor.Client.CloseIdleConnections()
	}
	return updateVideoSingleTaskWithExecutor(ctx, ch, taskId, taskM, executor)
}

func updateVideoSingleTaskWithExecutor(ctx context.Context, ch *model.Channel, taskId string, taskM map[string]*model.Task, executor pluginhost.Executor) error {
	baseURL := constant.GetChannelBaseURL(ch.Type)
	if ch.GetBaseURL() != "" {
		baseURL = ch.GetBaseURL()
	}
	task := taskM[taskId]
	if task == nil {
		logger.LogError(ctx, fmt.Sprintf("Task %s not found in taskM", taskId))
		return fmt.Errorf("task %s not found", taskId)
	}
	if task.Status == model.TaskStatusBillingPending {
		// Back off even when validation or provider querying fails, so a
		// paused/broken pending task cannot occupy every polling slot.
		if err := model.DeferPluginBillingRetry(ctx, task.ID, time.Now().Add(time.Minute).Unix()); err != nil {
			return err
		}
		if task.PrivateData.PluginContext != nil && (task.PrivateData.PluginContext.CompletionReady || task.PrivateData.PluginContext.CompletionTokens != nil) {
			return resumePluginCompletion(ctx, task, ch, baseURL)
		}
	}
	if task.PrivateData.Plugin == nil {
		return fmt.Errorf("legacy task %s requires explicit plugin identity migration", task.TaskID)
	}
	taskResult, responseBody, err := pollPluginTask(ctx, task, ch, taskId, executor)
	if err != nil {
		return fmt.Errorf("parse task result failed for task %s: %w", taskId, err)
	}

	return applyPluginTaskResult(ctx, task, taskResult, responseBody)
}

func applyPluginTaskResult(ctx context.Context, task *model.Task, taskResult *relaycommon.TaskInfo, responseBody []byte) error {
	taskId := task.GetUpstreamTaskID()
	snap := task.Snapshot()
	if snap.Status == model.TaskStatusSuccess || snap.Status == model.TaskStatusFailure {
		return nil
	}
	if snap.Status == model.TaskStatusBillingPending && taskResult.Status != model.TaskStatusSuccess {
		return nil
	}
	if taskResult.SubmitTime > 0 {
		task.SubmitTime = taskResult.SubmitTime
	}
	if taskResult.StartTime > 0 {
		task.StartTime = taskResult.StartTime
	}
	if taskResult.FinishTime > 0 {
		task.FinishTime = taskResult.FinishTime
	}

	if task.PrivateData.Plugin != nil && task.PrivateData.Plugin.Key == "vertex-ai" {
		task.Data = responseBody
	} else {
		task.Data = redactVideoResponseBody(responseBody)
	}

	logger.LogDebug(ctx, "updateVideoSingleTask taskResult: %+v", taskResult)

	now := time.Now().Unix()
	if taskResult.Status == "" {
		//taskResult = relaycommon.FailTaskInfo("upstream returned empty status")
		errorResult := &dto.GeneralErrorResponse{}
		if err := common.Unmarshal(responseBody, &errorResult); err == nil {
			openaiError := errorResult.TryToOpenAIError()
			if openaiError != nil {
				// 返回规范的 OpenAI 错误格式，提取错误信息，判断错误是否为任务失败
				if openaiError.Code == "429" {
					// 429 错误通常表示请求过多或速率限制，暂时不认为是任务失败，保持原状态等待下一轮轮询
					return nil
				}

				// 其他错误认为是任务失败，记录错误信息并更新任务状态
				taskResult = relaycommon.FailTaskInfo("upstream returned error")
			} else {
				// unknown error format, log original response
				logger.LogError(ctx, fmt.Sprintf("Task %s returned empty status with unrecognized error format, response: %s", taskId, string(responseBody)))
				taskResult = relaycommon.FailTaskInfo("upstream returned unrecognized message")
			}
		}
	}

	quota := task.Quota

	task.Status = model.TaskStatus(taskResult.Status)
	switch taskResult.Status {
	case model.TaskStatusSubmitted:
		task.Progress = taskcommon.ProgressSubmitted
	case model.TaskStatusQueued:
		task.Progress = taskcommon.ProgressQueued
	case model.TaskStatusInProgress:
		task.Progress = taskcommon.ProgressInProgress
		if task.StartTime == 0 {
			task.StartTime = now
		}
	case model.TaskStatusSuccess:
		task.Progress = taskcommon.ProgressComplete
		if task.FinishTime == 0 {
			task.FinishTime = now
		}
		if strings.HasPrefix(taskResult.Url, "data:") || (task.PrivateData.Plugin != nil && task.PrivateData.Plugin.Key == "google") {
			// data: URI (e.g. Vertex base64 encoded video) — keep in Data, not in ResultURL
			task.PrivateData.ResultURL = taskcommon.BuildProxyURL(task.TaskID)
		} else if taskResult.Url != "" {
			// Direct upstream URL (e.g. Kling, Ali, Doubao, etc.)
			task.PrivateData.ResultURL = taskResult.Url
		} else {
			// No result URL — construct a proxy URL using the public task ID
			task.PrivateData.ResultURL = taskcommon.BuildProxyURL(task.TaskID)
		}
	case model.TaskStatusFailure:
		logger.LogJson(ctx, fmt.Sprintf("Task %s failed", taskId), task)
		task.Status = model.TaskStatusFailure
		task.Progress = taskcommon.ProgressComplete
		if task.FinishTime == 0 {
			task.FinishTime = now
		}
		task.FailReason = taskResult.Reason
		logger.LogInfo(ctx, fmt.Sprintf("Task %s failed: %s", task.TaskID, task.FailReason))
		taskResult.Progress = taskcommon.ProgressComplete
	default:
		return fmt.Errorf("unknown task status %s for task %s", taskResult.Status, task.TaskID)
	}
	if taskResult.Progress != "" {
		task.Progress = taskResult.Progress
	}
	if task.PrivateData.Plugin != nil && taskResult.Status == model.TaskStatusSuccess {
		if TaskPluginRequiresCompletion(task.PrivateData.Plugin.Key, task.Properties.UpstreamModelName) || (task.PrivateData.PluginContext != nil && task.PrivateData.PluginContext.TokenBilling != nil) {
			return checkpointPluginCompletion(ctx, task, snap.Status, taskResult)
		}
		pc := task.PrivateData.PluginContext
		if pc == nil {
			pc = &model.TaskPluginContext{}
		}
		copy := *pc
		copy.CompletionUsage, copy.CompletionUsageInvalid = taskResult.CompletionUsage, taskResult.CompletionUsageInvalid
		task.PrivateData.PluginContext = &copy
	}

	isDone := task.Status == model.TaskStatusSuccess || task.Status == model.TaskStatusFailure
	if isDone && snap.Status != task.Status {
		finalQuota := quota
		reason := "任务完成，无差额调整"
		if task.Status == model.TaskStatusFailure {
			finalQuota = 0
			reason = task.FailReason
		} else if bc := task.PrivateData.BillingContext; bc == nil || !bc.PerCallBilling {
			if calculated, ok := calculateTaskQuotaByTokens(task, taskResult.TotalTokens); ok {
				finalQuota = calculated
				reason = fmt.Sprintf("token重算：tokens=%d", taskResult.TotalTokens)
			}
		}
		won, err := FinalizeTaskWithBilling(ctx, task, snap.Status, finalQuota, reason)
		if err != nil {
			return fmt.Errorf("finalize task %s with billing: %w", task.TaskID, err)
		} else if !won {
			logger.LogWarn(ctx, fmt.Sprintf("Task %s already transitioned by another process, skip billing", task.TaskID))
		}
	} else if !snap.Equal(task.Snapshot()) {
		if _, err := task.UpdateWithStatus(snap.Status); err != nil {
			logger.LogError(ctx, fmt.Sprintf("Failed to update task %s: %s", task.TaskID, err.Error()))
		}
	} else {
		// No changes, skip update
		logger.LogDebug(ctx, "No update needed for task %s", task.TaskID)
	}

	return nil
}

func pollPluginTask(ctx context.Context, task *model.Task, ch *model.Channel, taskID string, executor pluginhost.Executor) (*relaycommon.TaskInfo, []byte, error) {
	baseURL := constant.GetChannelBaseURL(ch.Type)
	if ch.GetBaseURL() != "" {
		baseURL = ch.GetBaseURL()
	}
	key := strings.TrimSpace(ch.Key)
	if task.PrivateData.Key != "" {
		key = strings.TrimSpace(task.PrivateData.Key)
	}
	if err := management.Default.CheckProductionEnabled(ctx, ch.Id); err != nil {
		return nil, nil, err
	}
	env, err := PrepareTaskPluginAuth(ctx, ch.Type, baseURL, key, ch.Other, executor)
	if err != nil {
		return nil, nil, err
	}
	baseURL = env.BaseURL
	plan, err := management.Default.PrepareTaskQueries(ctx, []int64{task.ID}, map[int]management.TaskQueryEnvironment{ch.Id: env})
	if err != nil {
		return nil, nil, err
	}
	if len(plan.LegacyTaskIDs) != 0 || len(plan.Queries) != 1 || len(plan.Queries[0].TaskIDs()) != 1 {
		return nil, nil, fmt.Errorf("plugin task %s produced invalid query plan", taskID)
	}
	response, err := plan.Queries[0].Execute(ctx, executor, baseURL, nil)
	if err != nil {
		return nil, nil, err
	}
	if response.Status < 200 || response.Status >= 300 {
		return nil, nil, fmt.Errorf("plugin query returned HTTP %d", response.Status)
	}
	candidates, err := plan.Queries[0].ParseProduction(ctx, response)
	if err != nil {
		return nil, nil, err
	}
	if len(candidates) != 1 || candidates[0].TaskID != task.ID || candidates[0].Identity != *task.PrivateData.Plugin {
		return nil, nil, fmt.Errorf("plugin task %s returned unexpected candidate count or identity", taskID)
	}
	result := candidates[0].Result
	getString := func(name string) string { value, _ := result[name].(string); return value }
	taskResult := &relaycommon.TaskInfo{TaskID: getString("taskId"), Status: getString("status"), Url: getString("url"), RemoteUrl: getString("remoteUrl"), Reason: getString("reason"), Progress: getString("progress")}
	taskResult.CompletionUsage, taskResult.CompletionUsageInvalid = candidates[0].CompletionUsage, candidates[0].CompletionUsageInvalid
	if taskResult.Url == "" && taskResult.RemoteUrl != "" {
		taskResult.Url = taskResult.RemoteUrl
	}
	if n, ok := result["totalTokens"].(float64); ok {
		if math.IsNaN(n) || math.IsInf(n, 0) || n < 0 || n >= float64(int(^uint(0)>>1)) {
			return nil, nil, errors.New("invalid plugin completion tokens")
		}
		taskResult.TotalTokens = int(n)
	}
	if taskResult.TaskID == "" {
		taskResult.TaskID = task.GetUpstreamTaskID()
	}
	data := response.Body
	if value, ok := result["data"]; ok {
		data = value
	}
	body, err := common.Marshal(data)
	if err != nil {
		return nil, nil, err
	}
	return taskResult, body, nil
}

func redactVideoResponseBody(body []byte) []byte {
	var m map[string]any
	if err := common.Unmarshal(body, &m); err != nil {
		return body
	}
	resp, _ := m["response"].(map[string]any)
	if resp != nil {
		delete(resp, "bytesBase64Encoded")
		if v, ok := resp["video"].(string); ok {
			resp["video"] = truncateBase64(v)
		}
		if vs, ok := resp["videos"].([]any); ok {
			for i := range vs {
				if vm, ok := vs[i].(map[string]any); ok {
					delete(vm, "bytesBase64Encoded")
				}
			}
		}
	}
	b, err := common.Marshal(m)
	if err != nil {
		return body
	}
	return b
}

func truncateBase64(s string) string {
	const maxKeep = 256
	if len(s) <= maxKeep {
		return s
	}
	return s[:maxKeep] + "..."
}
