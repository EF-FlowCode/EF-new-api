package model

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"eflow.local/new-api/common"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// The outbox is committed with the balance return in the primary database.
type UsageRefundNotice struct {
	BatchID   string `gorm:"type:varchar(36);primaryKey"`
	UserID    int    `gorm:"primaryKey;autoIncrement:false"`
	Payload   string `gorm:"type:text"`
	Delivered bool   `gorm:"index"`
}

// The delivery marker is committed with the user-visible log in LOG_DB. It
// survives log cleanup so a retry cannot recreate an old notice or duplicate it.
type UsageRefundLogDelivery struct {
	BatchID string `gorm:"type:varchar(36);primaryKey"`
	UserID  int    `gorm:"primaryKey;autoIncrement:false"`
	LogID   int
}

func DeliverUsageRefundNotices(ctx context.Context, batchID string) error {
	var notices []UsageRefundNotice
	query := DB.WithContext(ctx).Where("delivered = ?", false)
	if batchID != "" {
		query = query.Where("batch_id = ?", batchID)
	}
	if err := query.Order("batch_id, user_id").Limit(100).Find(&notices).Error; err != nil {
		return err
	}
	// One undeliverable notice must not block every later batch: record the
	// failure, keep delivering the rest, and report the first error.
	var firstErr error
	for _, notice := range notices {
		err := LOG_DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			var delivery UsageRefundLogDelivery
			err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("batch_id = ? AND user_id = ?", notice.BatchID, notice.UserID).First(&delivery).Error
			if err == nil {
				return nil
			}
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
			delivery = UsageRefundLogDelivery{BatchID: notice.BatchID, UserID: notice.UserID}
			if err := tx.Create(&delivery).Error; err != nil {
				return err
			}
			var entry Log
			if err := json.Unmarshal([]byte(notice.Payload), &entry); err != nil {
				return err
			}
			if err := tx.Create(&entry).Error; err != nil {
				return err
			}
			return tx.Model(&delivery).Update("log_id", entry.Id).Error
		})
		if err == nil {
			err = DB.WithContext(ctx).Model(&notice).Update("delivered", true).Error
		}
		if err != nil {
			common.SysError(fmt.Sprintf("usage refund log delivery failed batch=%s user=%d: %s", notice.BatchID, notice.UserID, err.Error()))
			if firstErr == nil {
				firstErr = err
			}
		}
	}
	return firstErr
}
