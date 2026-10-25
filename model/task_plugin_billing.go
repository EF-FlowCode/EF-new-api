package model

import (
	"context"
	"errors"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func DeferPluginBillingRetry(ctx context.Context, id int64, retryAfter int64) error {
	return DB.WithContext(ctx).Model(&Task{}).Where("id = ? AND status = ?", id, TaskStatusBillingPending).
		UpdateColumn("private_data", gorm.Expr("JSON_SET(private_data, '$.plugin_context.billing_retry_after', ?)", retryAfter)).Error
}

// StorePluginBillingPending is a monotonic checkpoint of provider success.
// A valid completion becomes immutable before any wallet transaction. A stale
// worker cannot replace it with missing usage or a later provider failure.
func StorePluginBillingPending(ctx context.Context, task *Task, from TaskStatus) (bool, error) {
	if task == nil || task.PrivateData.Plugin == nil || task.PrivateData.PluginContext == nil || task.Status != TaskStatusBillingPending {
		return false, ErrTaskPluginIdentity
	}
	if from == TaskStatusSuccess || from == TaskStatusFailure {
		return false, nil
	}
	won := false
	originalRevision := task.Revision
	err := DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var current Task
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&current, task.ID).Error; err != nil {
			return err
		}
		if current.Status != from || current.Revision != task.Revision {
			return nil
		}
		if current.PrivateData.Plugin == nil || *current.PrivateData.Plugin != *task.PrivateData.Plugin || current.Quota != task.Quota || current.UserId != task.UserId || current.ChannelId != task.ChannelId || current.TaskID != task.TaskID || current.PrivateData.TokenId != task.PrivateData.TokenId {
			return ErrTaskPluginIdentity
		}
		if from == TaskStatusBillingPending && current.PrivateData.PluginContext != nil && (current.PrivateData.PluginContext.CompletionReady || current.PrivateData.PluginContext.CompletionTokens != nil) {
			return nil
		}
		if task.PrivateData.PluginContext.BillingRetryAfter <= 0 {
			return errors.New("pending billing requires a retry deadline")
		}
		task.Revision++
		result := tx.Model(task).Where("status = ? AND revision = ?", from, current.Revision).Select("*").Updates(task)
		won = result.RowsAffected > 0
		return result.Error
	})
	if err != nil || !won {
		task.Revision = originalRevision
		return false, err
	}
	return true, nil
}
