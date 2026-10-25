package model

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"gorm.io/gorm"
)

type UserUsageSummary struct {
	Quota         int64 `json:"quota"`
	UsedQuota     int64 `json:"used_quota"`
	RefundedQuota int64 `json:"refunded_quota"`
	NetUsedQuota  int64 `json:"net_used_quota"`
	RequestCount  int64 `json:"request_count"`
}

type UserRefundRecord struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"`
	CreatedAt int64  `json:"created_at"`
	Quota     int64  `json:"quota"`
	Reason    string `json:"reason"`
	Status    string `json:"status"`
	RequestID string `json:"request_id,omitempty"`
	TicketID  int    `json:"ticket_id,omitempty"`
}

type UserRefundRecords struct {
	Items   []UserRefundRecord `json:"items"`
	HasMore bool               `json:"has_more"`
}

// Each source keeps its accounting meaning. Ticket quota is the requested
// withdrawal, not proof of an external payment. No private operator/payee data.
func GetUserRefundRecords(ctx context.Context, userID int, kind string, page, pageSize int) (*UserRefundRecords, error) {
	if userID <= 0 || page < 1 || page > 10000 || pageSize < 1 || pageSize > 100 {
		return nil, errors.New("invalid refund history query")
	}
	ctx, cancel := context.WithTimeout(ctx, logQueryTimeout)
	defer cancel()
	result := &UserRefundRecords{Items: []UserRefundRecord{}}
	offset := (page - 1) * pageSize
	switch kind {
	case "usage":
		err := DB.WithContext(ctx).Table("usage_refund_receipts AS r").
			Select("r.batch_id AS id, SUM(r.quota) AS quota, MAX(r.created_at) AS created_at, b.reason").
			Joins("JOIN usage_refund_batches AS b ON b.id = r.batch_id").Where("r.user_id = ?", userID).
			Group("r.batch_id, b.reason").Order("created_at DESC, r.batch_id DESC").
			Offset(offset).Limit(pageSize + 1).Scan(&result.Items).Error
		if err != nil {
			return nil, err
		}
		for i := range result.Items {
			result.Items[i].Status = "completed"
		}
	case "task":
		var logs []Log
		err := LOG_DB.WithContext(ctx).Where("user_id = ? AND type = ?", userID, LogTypeRefund).
			Where("JSON_EXTRACT(IF(JSON_VALID(other), other, '{}'), '$.usage_refund_batch_id') IS NULL").
			Order("id DESC").Offset(offset).Limit(pageSize + 1).Find(&logs).Error
		if err != nil {
			return nil, err
		}
		for _, log := range logs {
			reason := log.Content
			if reason == "" {
				var metadata struct {
					Reason string `json:"reason"`
				}
				if json.Unmarshal([]byte(log.Other), &metadata) == nil {
					reason = metadata.Reason
				}
			}
			result.Items = append(result.Items, UserRefundRecord{ID: fmt.Sprint(log.Id), CreatedAt: log.CreatedAt, Quota: int64(log.Quota), Reason: reason, Status: "completed", RequestID: log.RequestId})
		}
	case "ticket":
		var refunds []TicketRefund
		err := DB.WithContext(ctx).Select("id", "ticket_id", "refund_quota", "created_time", "processed_time", "reason", "refund_status").
			Where("user_id = ?", userID).Order("id DESC").Offset(offset).Limit(pageSize + 1).Find(&refunds).Error
		if err != nil {
			return nil, err
		}
		for _, refund := range refunds {
			status := "pending"
			if refund.RefundStatus == RefundStatusRefunded {
				status = "completed"
			} else if refund.RefundStatus == RefundStatusRejected {
				status = "rejected"
			}
			result.Items = append(result.Items, UserRefundRecord{ID: fmt.Sprint(refund.Id), TicketID: refund.TicketId, CreatedAt: refund.CreatedTime, Quota: int64(refund.RefundQuota), Reason: refund.Reason, Status: status})
		}
	default:
		return nil, errors.New("invalid refund history query")
	}
	result.HasMore = len(result.Items) > pageSize
	if result.HasMore {
		result.Items = result.Items[:pageSize]
	}
	for i := range result.Items {
		result.Items[i].Kind = kind
	}
	return result, nil
}

func GetUserUsageSummary(ctx context.Context, userID int) (*UserUsageSummary, error) {
	if userID <= 0 {
		return nil, gorm.ErrRecordNotFound
	}
	ctx, cancel := context.WithTimeout(ctx, logQueryTimeout)
	defer cancel()
	// One statement observes the wallet and committed receipts in one snapshot.
	// Task refunds already reduce used_quota; only manual consumption returns
	// need subtracting here. User-facing log delivery may still be pending.
	refunds := DB.Model(&UsageRefundReceipt{}).Select("COALESCE(SUM(quota), 0)").Where("user_id = ?", userID)
	var summary UserUsageSummary
	result := DB.WithContext(ctx).Model(&User{}).
		Select("quota, used_quota, request_count, (?) AS refunded_quota", refunds).
		Where("id = ?", userID).Take(&summary)
	if result.Error != nil {
		return nil, result.Error
	}
	summary.NetUsedQuota = summary.UsedQuota - summary.RefundedQuota
	return &summary, nil
}
