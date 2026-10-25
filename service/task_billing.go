package service

import (
	"context"
	"fmt"
	"strings"

	"eflow.local/new-api/common"
	"eflow.local/new-api/constant"
	"eflow.local/new-api/logger"
	"eflow.local/new-api/model"
	relaycommon "eflow.local/new-api/relay/common"
	"eflow.local/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
)

// LogTaskConsumption 记录任务消费日志和统计信息（仅记录，不涉及实际扣费）。
// 实际扣费已由 BillingSession（PreConsumeBilling + SettleBilling）完成。
func LogTaskConsumption(c *gin.Context, info *relaycommon.RelayInfo) {
	tokenName := c.GetString("token_name")
	logContent := fmt.Sprintf("操作 %s", info.Action)
	// 支持任务仅按次计费
	if common.StringsContains(constant.TaskPricePatches, info.OriginModelName) {
		logContent = fmt.Sprintf("%s，按次计费", logContent)
	} else {
		if len(info.PriceData.OtherRatios) > 0 {
			var contents []string
			for key, ra := range info.PriceData.OtherRatios {
				if 1.0 != ra {
					contents = append(contents, fmt.Sprintf("%s: %.2f", key, ra))
				}
			}
			if len(contents) > 0 {
				logContent = fmt.Sprintf("%s, 计算参数：%s", logContent, strings.Join(contents, ", "))
			}
		}
	}
	other := make(map[string]any)
	other["is_task"] = true
	other["request_path"] = c.Request.URL.Path
	other["model_price"] = info.PriceData.ModelPrice
	if info.PriceData.ModelRatio > 0 {
		other["model_ratio"] = info.PriceData.ModelRatio
	}
	other["group_ratio"] = info.PriceData.GroupRatioInfo.GroupRatio
	if info.PriceData.GroupRatioInfo.HasSpecialRatio {
		other["user_group_ratio"] = info.PriceData.GroupRatioInfo.GroupSpecialRatio
	}
	if info.IsModelMapped {
		other["is_model_mapped"] = true
		other["upstream_model_name"] = info.UpstreamModelName
	}
	// eflow: 按时长计费的视频任务（happyhorse 等，model_price 为每秒价），
	// 把实际计费秒数 + 标记写进日志，前端用量日志据此把"详情/Tokens"按秒
	// 渲染（`$model_price/秒` + `N秒`）。按次计费(TaskPricePatches)不算时长，跳过。
	if !common.StringsContains(constant.TaskPricePatches, info.OriginModelName) {
		if secs, ok := info.PriceData.OtherRatios["seconds"]; ok && secs > 0 {
			other["dur_sec"] = secs
			other["billing_mode"] = "duration_seconds"
		}
	}
	model.RecordConsumeLog(c, info.UserId, model.RecordConsumeLogParams{
		ChannelId: info.ChannelId,
		ModelName: info.OriginModelName,
		TokenName: tokenName,
		Quota:     info.PriceData.Quota,
		Content:   logContent,
		TokenId:   info.TokenId,
		Group:     info.UsingGroup,
		Other:     other,
	})
	model.UpdateUserUsedQuotaAndRequestCount(info.UserId, info.PriceData.Quota)
	model.UpdateChannelUsedQuota(info.ChannelId, info.PriceData.Quota)
}

// ---------------------------------------------------------------------------
// 异步任务计费辅助函数
// ---------------------------------------------------------------------------

// resolveTokenKey 通过 TokenId 运行时获取令牌 Key（用于 Redis 缓存操作）。
// 如果令牌已被删除或查询失败，返回空字符串。
func resolveTokenKey(ctx context.Context, tokenId int, taskID string) string {
	token, err := model.GetTokenById(tokenId)
	if err != nil {
		logger.LogWarn(ctx, fmt.Sprintf("获取令牌 key 失败 (tokenId=%d, task=%s): %s", tokenId, taskID, err.Error()))
		return ""
	}
	return token.Key
}

// taskBillingOther 从 task 的 BillingContext 构建日志 Other 字段。
func taskBillingOther(task *model.Task) map[string]any {
	other := make(map[string]any)
	// 与提交时的消费日志一致，结算差额 / 退款日志也标记 is_task，供前端与退款判定识别任务计费
	other["is_task"] = true
	if s := task.PrivateData.KnowledgeBatch; s != nil {
		other["billing_mode"] = "batch_file"
		other["input_tokens"] = s.BilledInputTokens
		other["input_price"] = s.InputPrice
	}
	if bc := task.PrivateData.BillingContext; bc != nil {
		other["model_price"] = bc.ModelPrice
		if bc.ModelRatio > 0 {
			other["model_ratio"] = bc.ModelRatio
		}
		other["group_ratio"] = bc.GroupRatio
		if len(bc.OtherRatios) > 0 {
			for k, v := range bc.OtherRatios {
				other[k] = v
			}
		}
	}
	props := task.Properties
	if props.UpstreamModelName != "" && props.UpstreamModelName != props.OriginModelName {
		other["is_model_mapped"] = true
		other["upstream_model_name"] = props.UpstreamModelName
	}
	return other
}

// taskModelName 从 BillingContext 或 Properties 中获取模型名称。
func taskModelName(task *model.Task) string {
	if bc := task.PrivateData.BillingContext; bc != nil && bc.OriginModelName != "" {
		return bc.OriginModelName
	}
	return task.Properties.OriginModelName
}

// FinalizeTaskWithBilling 将任务终态 CAS、差额扣费/退款和任务 Quota 持久化放在同一事务中。
func FinalizeTaskWithBilling(ctx context.Context, task *model.Task, fromStatus model.TaskStatus, finalQuota int, reason string) (bool, error) {
	if task == nil {
		return false, fmt.Errorf("task is nil")
	}
	if finalQuota < 0 {
		return false, fmt.Errorf("final quota cannot be negative")
	}
	oldQuota := task.Quota
	delta := finalQuota - oldQuota
	task.Quota = finalQuota

	tokenKey := ""
	if task.PrivateData.TokenId > 0 {
		tokenKey = resolveTokenKey(ctx, task.PrivateData.TokenId, task.TaskID)
	}
	won, err := model.UpdateTaskWithBillingAtomic(task, fromStatus, model.BillingQuotaAdjustment{
		UserID:   task.UserId,
		TokenID:  task.PrivateData.TokenId,
		TokenKey: tokenKey,
		Delta:    delta,
	})
	if err != nil || !won {
		task.Quota = oldQuota
		return won, err
	}
	if delta == 0 {
		return true, nil
	}

	// The submit path already counted the request. Async settlement must adjust
	// only accumulated usage, in either direction.
	model.UpdateUserUsedQuota(task.UserId, delta)
	model.UpdateChannelUsedQuota(task.ChannelId, delta)

	logType := model.LogTypeConsume
	logQuota := delta
	if delta < 0 {
		logType = model.LogTypeRefund
		logQuota = -delta
	}
	other := taskBillingOther(task)
	other["task_id"] = task.TaskID
	other["pre_consumed_quota"] = oldQuota
	other["actual_quota"] = finalQuota
	other["reason"] = reason
	model.RecordTaskBillingLog(model.RecordTaskBillingLogParams{
		UserId:    task.UserId,
		LogType:   logType,
		Content:   reason,
		ChannelId: task.ChannelId,
		ModelName: taskModelName(task),
		Quota:     logQuota,
		TokenId:   task.PrivateData.TokenId,
		Group:     task.Group,
		Other:     other,
	})
	return true, nil
}

func calculateTaskQuotaByTokens(task *model.Task, totalTokens int) (int, bool) {
	if totalTokens <= 0 {
		return 0, false
	}

	modelName := taskModelName(task)

	// 获取模型价格和倍率
	modelRatio, hasRatioSetting, _ := ratio_setting.GetModelRatio(modelName)
	// 只有配置了倍率(非固定价格)时才按 token 重新计费
	if !hasRatioSetting || modelRatio <= 0 {
		return 0, false
	}

	// 获取用户和组的倍率信息
	group := task.Group
	if group == "" {
		user, err := model.GetUserById(task.UserId, false)
		if err == nil {
			group = user.Group
		}
	}
	if group == "" {
		return 0, false
	}

	groupRatio := ratio_setting.GetGroupRatio(group)
	userGroupRatio, hasUserGroupRatio := ratio_setting.GetGroupGroupRatio(group, group)

	var finalGroupRatio float64
	if hasUserGroupRatio {
		finalGroupRatio = userGroupRatio
	} else {
		finalGroupRatio = groupRatio
	}

	// 计算 OtherRatios 乘积（视频折扣、时长等）
	otherMultiplier := 1.0
	if bc := task.PrivateData.BillingContext; bc != nil {
		for _, r := range bc.OtherRatios {
			if r != 1.0 && r > 0 {
				otherMultiplier *= r
			}
		}
	}

	// 计算实际应扣费额度: totalTokens * modelRatio * groupRatio * otherMultiplier
	return int(float64(totalTokens) * modelRatio * finalGroupRatio * otherMultiplier), true
}
