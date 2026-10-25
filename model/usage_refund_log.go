package model

import "context"

type UsageRefundLogMark struct {
	BatchID    string `json:"batch_id"`
	Quota      int    `json:"quota"`
	RefundedAt int64  `json:"refunded_at"`
}

// Enrich only the current page from the primary receipt ledger, before user log
// IDs are anonymized. No cross-database join or source-log mutation is needed.
func enrichUsageRefundLogs(ctx context.Context, logs []*Log) error {
	ids := make([]int, 0, len(logs))
	for _, log := range logs {
		log.UsageRefund = nil
		if log.Type == LogTypeConsume {
			ids = append(ids, log.Id)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	var receipts []UsageRefundReceipt
	if err := DB.WithContext(ctx).Select("source_log_id", "user_id", "batch_id", "quota", "created_at").Where("source_log_id IN ?", ids).Find(&receipts).Error; err != nil {
		return err
	}
	byID := make(map[int]UsageRefundReceipt, len(receipts))
	for _, receipt := range receipts {
		byID[receipt.SourceLogID] = receipt
	}
	for _, log := range logs {
		if receipt, ok := byID[log.Id]; ok && receipt.UserID == log.UserId && log.Type == LogTypeConsume {
			log.UsageRefund = &UsageRefundLogMark{BatchID: receipt.BatchID, Quota: receipt.Quota, RefundedAt: receipt.CreatedAt}
		}
	}
	return nil
}
