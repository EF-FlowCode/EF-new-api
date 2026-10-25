package model

import (
	"context"
	"sort"
	"strings"

	"eflow.local/new-api/common"
)

// eflow: 异常计费审核。按 logs.billing_anomaly 列出计费用量不可靠的消费日志，
// 供超级管理员审核；按估算扣费的记录可转入批量返还，免单记录（上游无用量）仅审核。

// BillingAnomalyQuery 是异常计费审核的筛选条件。Anomaly 为 0 表示全部异常类别。
// LowOutput 改为按「扣了费但输出不超过 1 Token」查找，不看 billing_anomaly：
// 标记于 2026-09-28 才上线，之前的日志全是 0，只能靠这个兜底找出疑似异常。
type BillingAnomalyQuery struct {
	Start     int64  `json:"start_timestamp"`
	End       int64  `json:"end_timestamp"`
	Anomaly   int    `json:"anomaly"`
	UserID    int    `json:"user_id"`
	Channel   int    `json:"channel"`
	ModelName string `json:"model_name"`
	LowOutput bool   `json:"low_output"`
}

type BillingAnomalyItem struct {
	LogID            int    `json:"log_id"`
	RequestID        string `json:"request_id"`
	CreatedAt        int64  `json:"created_at"`
	UserID           int    `json:"user_id"`
	Username         string `json:"username"`
	ModelName        string `json:"model_name"`
	Group            string `json:"group"`
	Channel          int    `json:"channel"`
	PromptTokens     int    `json:"prompt_tokens"`
	CompletionTokens int    `json:"completion_tokens"`
	UseTime          int    `json:"use_time"`
	Quota            int    `json:"quota"`
	Anomaly          int    `json:"anomaly"`
	Status           string `json:"status"`
	RefundBatchID    string `json:"refund_batch_id,omitempty"`
	RefundedQuota    int    `json:"refunded_quota,omitempty"`
}

// BillingAnomalyUserSummary 按用户汇总，用于发现反复首字前断开或集中被估算扣费的用户。
type BillingAnomalyUserSummary struct {
	UserID        int         `json:"user_id"`
	Username      string      `json:"username"`
	Count         int         `json:"count"`
	Quota         int         `json:"quota"`
	RefundedQuota int         `json:"refunded_quota"`
	ByAnomaly     map[int]int `json:"by_anomaly"`
}

type BillingAnomalyResult struct {
	Items []BillingAnomalyItem        `json:"items"`
	Users []BillingAnomalyUserSummary `json:"users"`
}

func ListBillingAnomalies(ctx context.Context, adminID, adminRole int, q BillingAnomalyQuery) (*BillingAnomalyResult, error) {
	if adminID <= 0 || adminRole != common.RoleRootUser {
		return nil, ErrUsageRefundForbidden
	}
	if q.Start <= 0 || q.End < q.Start || q.End-q.Start > 31*86400 || q.UserID < 0 || q.Channel < 0 ||
		q.Anomaly < LogBillingAnomalyNone || q.Anomaly > LogBillingAnomalyNoUpstreamUsage ||
		(q.LowOutput && q.Anomaly != LogBillingAnomalyNone) {
		return nil, ErrUsageRefundInvalid
	}

	logCtx, cancel := context.WithTimeout(ctx, logQueryTimeout)
	defer cancel()
	tx := LOG_DB.WithContext(logCtx).Model(&Log{})
	// 先按 billing_anomaly 等值/IN 命中 (billing_anomaly, created_at) 索引，再收窄时间。
	if q.LowOutput {
		tx = tx.Where("quota > 0 AND completion_tokens <= ?", 1)
	} else if q.Anomaly != LogBillingAnomalyNone {
		tx = tx.Where("billing_anomaly = ?", q.Anomaly)
	} else {
		tx = tx.Where("billing_anomaly IN ?", []int{LogBillingAnomalyInputEstimated, LogBillingAnomalyOutputEstimated, LogBillingAnomalyNoUpstreamUsage})
	}
	tx = tx.Where("type = ? AND created_at >= ? AND created_at <= ?", LogTypeConsume, q.Start, q.End)
	if q.UserID > 0 {
		tx = tx.Where("user_id = ?", q.UserID)
	}
	if q.Channel > 0 {
		tx = tx.Where("channel_id = ?", q.Channel)
	}
	tx = applyLogModelFilter(tx, "model_name", strings.TrimSpace(q.ModelName))
	var logs []Log
	if err := tx.Limit(MaxUsageRefundItems + 1).Find(&logs).Error; err != nil {
		return nil, err
	}
	if len(logs) > MaxUsageRefundItems {
		return nil, ErrUsageRefundLimit
	}
	sort.Slice(logs, func(i, j int) bool { return logs[i].Id > logs[j].Id })

	receipts := map[int]UsageRefundReceipt{}
	ids := make([]int, 0, len(logs))
	for _, log := range logs {
		if log.Quota > 0 {
			ids = append(ids, log.Id)
		}
	}
	if len(ids) > 0 {
		var rows []UsageRefundReceipt
		if err := DB.WithContext(ctx).Select("source_log_id", "batch_id", "quota").Where("source_log_id IN ?", ids).Find(&rows).Error; err != nil {
			return nil, err
		}
		for _, row := range rows {
			receipts[row.SourceLogID] = row
		}
	}

	result := &BillingAnomalyResult{Items: make([]BillingAnomalyItem, 0, len(logs))}
	users := map[int]*BillingAnomalyUserSummary{}
	for _, log := range logs {
		// 审核场景下异常本身就是返还依据，client_gone 不再作为跳过理由。
		item := BillingAnomalyItem{
			LogID: log.Id, RequestID: log.RequestId, CreatedAt: log.CreatedAt,
			UserID: log.UserId, Username: log.Username, ModelName: log.ModelName,
			Group: log.Group, Channel: log.ChannelId, PromptTokens: log.PromptTokens,
			CompletionTokens: log.CompletionTokens, UseTime: log.UseTime, Quota: log.Quota,
			Anomaly: log.BillingAnomaly, Status: usageRefundEligibility(log, true),
		}
		if receipt, ok := receipts[log.Id]; ok {
			item.Status, item.RefundBatchID, item.RefundedQuota = "already_refunded", receipt.BatchID, receipt.Quota
		}
		result.Items = append(result.Items, item)

		summary := users[log.UserId]
		if summary == nil {
			summary = &BillingAnomalyUserSummary{UserID: log.UserId, Username: log.Username, ByAnomaly: map[int]int{}}
			users[log.UserId] = summary
		}
		summary.Count++
		summary.Quota += log.Quota
		summary.RefundedQuota += item.RefundedQuota
		summary.ByAnomaly[log.BillingAnomaly]++
	}
	result.Users = make([]BillingAnomalyUserSummary, 0, len(users))
	for _, summary := range users {
		result.Users = append(result.Users, *summary)
	}
	sort.Slice(result.Users, func(i, j int) bool {
		if result.Users[i].Count != result.Users[j].Count {
			return result.Users[i].Count > result.Users[j].Count
		}
		return result.Users[i].UserID < result.Users[j].UserID
	})
	return result, nil
}
