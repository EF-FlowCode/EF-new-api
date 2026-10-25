package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"eflow.local/new-api/common"
	"eflow.local/new-api/constant"
	"eflow.local/new-api/logger"
	"eflow.local/new-api/model"
	host "eflow.local/new-api/relay/channel/task/jsplugin"
	relaycommon "eflow.local/new-api/relay/common"
	management "eflow.local/new-api/service/taskplugin"
)

func updatePluginBatchTasks(ctx context.Context, channel *model.Channel, ids []string, tasks map[string]*model.Task) error {
	executor, err := NewTaskPluginHTTPExecutorForProxy(channel.GetSetting().Proxy)
	if err != nil {
		return err
	}
	if channel.GetSetting().Proxy != "" {
		defer executor.Client.CloseIdleConnections()
	}
	return updatePluginBatchTasksWithExecutor(ctx, channel, ids, tasks, executor)
}

func updatePluginBatchTasksWithExecutor(ctx context.Context, channel *model.Channel, ids []string, tasks map[string]*model.Task, executor host.Executor) error {
	if err := management.Default.CheckProductionEnabled(ctx, channel.Id); err != nil {
		return err
	}
	base := channel.GetBaseURL()
	if base == "" {
		base = constant.GetChannelBaseURL(channel.Type)
	}
	for start := 0; start < len(ids); start += 100 {
		end := min(start+100, len(ids))
		var databaseIDs []int64
		byID := map[int64]*model.Task{}
		for _, id := range ids[start:end] {
			task := tasks[id]
			if task == nil || task.ChannelId != channel.Id || task.PrivateData.Plugin == nil {
				return errors.New("batch contains an unadopted or mismatched task")
			}
			if task.Status == model.TaskStatusBillingPending {
				if err := model.DeferPluginBillingRetry(ctx, task.ID, time.Now().Add(time.Minute).Unix()); err != nil {
					return err
				}
				if pc := task.PrivateData.PluginContext; pc != nil && (pc.CompletionReady || pc.CompletionTokens != nil) {
					if err := resumePluginCompletion(ctx, task, channel, base); err != nil {
						logger.LogError(ctx, fmt.Sprintf("resume batch task %s billing: %v", task.TaskID, err))
					}
					continue
				}
			}
			databaseIDs = append(databaseIDs, task.ID)
			byID[task.ID] = task
		}
		if len(databaseIDs) == 0 {
			continue
		}
		plan, err := management.Default.PrepareTaskQueries(ctx, databaseIDs, map[int]management.TaskQueryEnvironment{channel.Id: {BaseURL: base}})
		if err != nil {
			return err
		}
		for _, query := range plan.Queries {
			response, err := query.Execute(ctx, executor, base, nil)
			if err != nil {
				return err
			}
			if response.Status < 200 || response.Status >= 300 {
				return fmt.Errorf("batch query returned HTTP %d", response.Status)
			}
			candidates, err := query.ParseProduction(ctx, response)
			if err != nil {
				return err
			}
			for _, candidate := range candidates {
				task := byID[candidate.TaskID]
				if task == nil || *task.PrivateData.Plugin != candidate.Identity {
					return errors.New("batch task identity changed")
				}
				get := func(key string) string { v, _ := candidate.Result[key].(string); return v }
				result := &relaycommon.TaskInfo{TaskID: task.GetUpstreamTaskID(), Status: get("status"), Reason: get("reason"), Progress: get("progress"), Url: get("url")}
				result.CompletionUsage, result.CompletionUsageInvalid = candidate.CompletionUsage, candidate.CompletionUsageInvalid
				for field, target := range map[string]*int64{"submitTime": &result.SubmitTime, "startTime": &result.StartTime, "finishTime": &result.FinishTime} {
					if n, ok := candidate.Result[field].(float64); ok && n > 0 && n < 1e12 {
						*target = int64(n)
					}
				}
				data, err := common.Marshal(candidate.Result["data"])
				if err != nil {
					return err
				}
				if err := applyPluginTaskResult(ctx, task, result, data); err != nil {
					// One bad result must not block every other task on the channel.
					logger.LogError(ctx, fmt.Sprintf("apply batch task %s result: %v", task.TaskID, err))
				}
			}
		}
	}
	return nil
}
