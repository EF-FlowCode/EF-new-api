package model

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"eflow.local/new-api/common"
	"gorm.io/gorm"
)

// UsageRefundCandidateQuery lists one user's consumption so an operator can pick
// logs the batch filters cannot express and return part or all of each charge.
type UsageRefundCandidateQuery struct {
	UserID    int    `json:"user_id"`
	Username  string `json:"username"`
	Start     int64  `json:"start_timestamp"`
	End       int64  `json:"end_timestamp"`
	ModelName string `json:"model_name"`
	RequestID string `json:"request_id"`
	// RequestIDs lets an operator paste a column copied from a usage export;
	// it is merged with RequestID.
	RequestIDs        []string `json:"request_ids"`
	IncludeClientGone bool     `json:"include_client_gone"`
}

type UsageRefundCandidate struct {
	LogID            int    `json:"log_id"`
	RequestID        string `json:"request_id"`
	CreatedAt        int64  `json:"created_at"`
	ModelName        string `json:"model_name"`
	TokenName        string `json:"token_name"`
	Group            string `json:"group"`
	Channel          int    `json:"channel"`
	PromptTokens     int    `json:"prompt_tokens"`
	CompletionTokens int    `json:"completion_tokens"`
	Quota            int    `json:"quota"`
	Status           string `json:"status"`
	RefundBatchID    string `json:"refund_batch_id,omitempty"`
	RefundedQuota    int    `json:"refunded_quota,omitempty"`
}

type UsageRefundCandidates struct {
	UserID   int                    `json:"user_id"`
	Username string                 `json:"username"`
	Items    []UsageRefundCandidate `json:"items"`
	// MissingRequestIDs are requested IDs with no charged log for this user in range.
	MissingRequestIDs []string `json:"missing_request_ids,omitempty"`
}

func usageRefundCandidateRequestIDs(q UsageRefundCandidateQuery) ([]string, bool) {
	seen := map[string]bool{}
	ids := []string{}
	for _, raw := range append([]string{q.RequestID}, q.RequestIDs...) {
		id := strings.TrimSpace(raw)
		if id == "" || seen[id] {
			continue
		}
		if len(id) > 128 {
			return nil, false
		}
		seen[id] = true
		ids = append(ids, id)
	}
	return ids, len(ids) <= MaxUsageRefundItems
}

func ListUsageRefundCandidates(ctx context.Context, adminID, adminRole int, q UsageRefundCandidateQuery) (*UsageRefundCandidates, error) {
	if adminID <= 0 || adminRole != common.RoleRootUser {
		return nil, ErrUsageRefundForbidden
	}
	q.Username = strings.TrimSpace(q.Username)
	requestIDs, ok := usageRefundCandidateRequestIDs(q)
	if !ok || (q.UserID > 0) == (q.Username != "") || q.UserID < 0 || q.Start <= 0 || q.End < q.Start || q.End-q.Start > 31*86400 {
		return nil, ErrUsageRefundInvalid
	}
	var user User
	userQuery := DB.WithContext(ctx).Select("id", "username", "role")
	if q.UserID > 0 {
		userQuery = userQuery.Where("id = ?", q.UserID)
	} else {
		userQuery = userQuery.Where("username = ?", q.Username)
	}
	if err := userQuery.First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("%w: user not found", ErrUsageRefundInvalid)
		}
		return nil, err
	}
	if !canRefundUsageRole(adminRole, user.Role) {
		return nil, ErrUsageRefundForbidden
	}

	logCtx, cancel := context.WithTimeout(ctx, logQueryTimeout)
	defer cancel()
	tx := LOG_DB.WithContext(logCtx).Model(&Log{}).
		Where("user_id = ? AND type = ? AND quota > 0 AND created_at >= ? AND created_at <= ?", user.Id, LogTypeConsume, q.Start, q.End)
	tx = applyLogModelFilter(tx, "model_name", q.ModelName)
	if len(requestIDs) > 0 {
		tx = tx.Where("request_id IN ?", requestIDs)
	}
	var logs []Log
	if err := tx.Limit(MaxUsageRefundItems + 1).Find(&logs).Error; err != nil {
		return nil, err
	}
	if len(logs) > MaxUsageRefundItems {
		return nil, ErrUsageRefundLimit
	}
	sort.Slice(logs, func(i, j int) bool { return logs[i].Id > logs[j].Id })

	ids := make([]int, 0, len(logs))
	for _, log := range logs {
		ids = append(ids, log.Id)
	}
	receipts := map[int]UsageRefundReceipt{}
	if len(ids) > 0 {
		var rows []UsageRefundReceipt
		if err := DB.WithContext(ctx).Select("source_log_id", "batch_id", "quota").Where("source_log_id IN ?", ids).Find(&rows).Error; err != nil {
			return nil, err
		}
		for _, row := range rows {
			receipts[row.SourceLogID] = row
		}
	}
	result := &UsageRefundCandidates{UserID: user.Id, Username: user.Username, Items: make([]UsageRefundCandidate, 0, len(logs))}
	for _, log := range logs {
		item := UsageRefundCandidate{LogID: log.Id, RequestID: log.RequestId, CreatedAt: log.CreatedAt, ModelName: log.ModelName, TokenName: log.TokenName, Group: log.Group, Channel: log.ChannelId, PromptTokens: log.PromptTokens, CompletionTokens: log.CompletionTokens, Quota: log.Quota, Status: usageRefundEligibility(log, q.IncludeClientGone)}
		if receipt, ok := receipts[log.Id]; ok {
			item.Status, item.RefundBatchID, item.RefundedQuota = "already_refunded", receipt.BatchID, receipt.Quota
		}
		result.Items = append(result.Items, item)
	}
	found := make(map[string]bool, len(logs))
	for _, log := range logs {
		found[log.RequestId] = true
	}
	for _, id := range requestIDs {
		if !found[id] {
			result.MissingRequestIDs = append(result.MissingRequestIDs, id)
		}
	}
	return result, nil
}
