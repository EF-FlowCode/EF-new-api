package model

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"eflow.local/new-api/common"
	"eflow.local/new-api/constant"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Previews and applied audits live in the financial database. An applied row
// and its task/wallet/token transition commit together, independently of LOG_DB.
type TaskPluginReview struct {
	OperationID  string     `json:"operation_id" gorm:"type:varchar(96);primaryKey"`
	ActorID      int        `json:"actor_id"`
	TaskID       int64      `json:"task_database_id" gorm:"index"`
	Revision     uint64     `json:"revision"`
	Action       string     `json:"action" gorm:"type:varchar(32)"`
	Payload      string     `json:"-" gorm:"type:text"`
	Reason       string     `json:"reason" gorm:"type:text"`
	Evidence     string     `json:"evidence" gorm:"type:text"`
	Expires      int64      `json:"expires"`
	Applied      bool       `json:"applied"`
	BeforeStatus TaskStatus `json:"before_status"`
	AfterStatus  TaskStatus `json:"after_status"`
	BeforeQuota  int        `json:"before_quota"`
	AfterQuota   int        `json:"after_quota"`
	CreatedAt    int64      `json:"created_at"`
}

func requirePluginRoot(tx *gorm.DB, actor int) error {
	var user User
	if actor <= 0 {
		return errors.New("root permission required")
	}
	if err := tx.Select("id", "role", "status").First(&user, actor).Error; err != nil {
		return err
	}
	if user.Role != common.RoleRootUser || user.Status != common.UserStatusEnabled {
		return errors.New("root permission required")
	}
	return nil
}

func CreatePluginReview(ctx context.Context, review TaskPluginReview) error {
	return DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := requirePluginRoot(tx, review.ActorID); err != nil {
			return err
		}
		return tx.Create(&review).Error
	})
}

// pluginQuotaDeltaTx reuses wallet/token primitives and keeps task accounting
// counters in the same financial transaction. Tokens keep original ownership.
func pluginQuotaDeltaTx(tx *gorm.DB, task *Task, delta int) error {
	if delta == 0 {
		return nil
	}
	if err := settleUserQuotaTx(tx, task.UserId, delta); err != nil {
		return err
	}
	if task.PrivateData.TokenId > 0 {
		var token Token
		if err := tx.Unscoped().Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND user_id = ?", task.PrivateData.TokenId, task.UserId).First(&token).Error; err != nil {
			return err
		}
		if err := adjustTokenDeltaTx(tx.Unscoped(), token.Id, delta); err != nil {
			return err
		}
	}
	if err := tx.Model(&User{}).Where("id = ?", task.UserId).UpdateColumn("used_quota", gorm.Expr("used_quota + ?", delta)).Error; err != nil {
		return err
	}
	return tx.Model(&Channel{}).Where("id = ?", task.ChannelId).UpdateColumn("used_quota", gorm.Expr("used_quota + ?", delta)).Error
}

func invalidatePluginBilling(task *Task, delta int) {
	tokenKey := ""
	if task.PrivateData.TokenId > 0 {
		var token Token
		if DB.Unscoped().First(&token, task.PrivateData.TokenId).Error == nil {
			tokenKey = token.Key
		}
	}
	updateBillingQuotaCaches(task.UserId, task.PrivateData.TokenId, tokenKey, delta, delta)
}

// Logs are best effort, after the financial transaction. The same-database
// review audit is authoritative if LOG_DB is unavailable or the process exits.
func recordPluginQuotaLog(task *Task, delta int, reason string) {
	logType, quota := LogTypeConsume, delta
	if delta < 0 {
		logType, quota = LogTypeRefund, -delta
	}
	name := task.Properties.OriginModelName
	other := map[string]any{"is_task": true, "task_id": task.TaskID, "pre_consumed_quota": task.Quota - delta, "actual_quota": task.Quota, "reason": reason}
	if bc := task.PrivateData.BillingContext; bc != nil {
		if bc.OriginModelName != "" {
			name = bc.OriginModelName
		}
		other["model_price"], other["group_ratio"], other["model_ratio"] = bc.ModelPrice, bc.GroupRatio, bc.ModelRatio
		for key, value := range bc.OtherRatios {
			other[key] = value
		}
		if seconds := bc.OtherRatios["seconds"]; seconds > 0 && !common.StringsContains(constant.TaskPricePatches, name) {
			other["dur_sec"], other["billing_mode"] = seconds, "duration_seconds"
		}
	}
	if upstream := task.Properties.UpstreamModelName; upstream != "" && upstream != name {
		other["is_model_mapped"], other["upstream_model_name"] = true, upstream
	}
	RecordTaskBillingLog(RecordTaskBillingLogParams{
		UserId: task.UserId, TokenId: task.PrivateData.TokenId, ChannelId: task.ChannelId,
		ModelName: name, Group: task.Group,
		LogType: logType, Quota: quota, Content: reason,
		Other: other,
	})
}

// Unsent cancellation can only win before the send claim. Once a request may
// have crossed the network, only reconciliation or a Root review may release it.
func RejectUnsentPluginSubmission(ctx context.Context, id int64) error {
	var task Task
	delta := 0
	err := DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&task, id).Error; err != nil {
			return err
		}
		if task.Status != TaskStatusSubmitting {
			return ErrPluginSubmissionConflict
		}
		var sub TaskPluginSubmission
		if err := tx.First(&sub, "task_id = ?", id).Error; err != nil {
			return err
		}
		if sub.Phase != "prepared" {
			return ErrPluginSubmissionConflict
		}
		delta = -task.Quota
		audit := TaskPluginReview{OperationID: fmt.Sprintf("unsent:%d", id), TaskID: id, Revision: task.Revision, Action: "not_sent", Applied: true, BeforeStatus: task.Status, AfterStatus: TaskStatusFailure, BeforeQuota: task.Quota, AfterQuota: 0, Reason: "request was not sent", CreatedAt: time.Now().Unix()}
		if err := pluginQuotaDeltaTx(tx, &task, delta); err != nil {
			return err
		}
		task.Status, task.Progress, task.Quota, task.FinishTime, task.Revision = TaskStatusFailure, "100%", 0, time.Now().Unix(), task.Revision+1
		task.FailReason = "Plugin task was not sent"
		if err := tx.Save(&task).Error; err != nil {
			return err
		}
		if err := tx.Model(&sub).Updates(map[string]any{"phase": "rejected", "parse_input": "", "response": "", "deadline": 0}).Error; err != nil {
			return err
		}
		return tx.Create(&audit).Error
	})
	if err == nil {
		invalidatePluginBilling(&task, delta)
		if delta != 0 {
			recordPluginQuotaLog(&task, delta, "plugin request not sent")
		}
	}
	return err
}

type PluginReviewMutation func(*Task, TaskPluginReview) error

func ApplyPluginReview(ctx context.Context, actor int, operation string, mutate PluginReviewMutation) (*Task, error) {
	var task Task
	delta := 0
	applied := false
	err := DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := requirePluginRoot(tx, actor); err != nil {
			return err
		}
		control, err := readTaskPluginSnapshot(tx, true)
		if err != nil {
			return err
		}
		var review TaskPluginReview
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&review, "operation_id = ? AND actor_id = ?", operation, actor).Error; err != nil {
			return err
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&task, review.TaskID).Error; err != nil {
			return err
		}
		if review.Applied {
			return nil
		}
		if review.Expires < time.Now().Unix() || task.Revision != review.Revision || task.Status != review.BeforeStatus || task.Quota != review.BeforeQuota {
			return ErrPluginSubmissionConflict
		}
		if task.PrivateData.Plugin == nil {
			return ErrTaskPluginIdentity
		}
		if task.Status == TaskStatusSubmitUnknown || task.Status == TaskStatusSubmitting {
			var sub TaskPluginSubmission
			if err := tx.First(&sub, "task_id = ?", task.ID).Error; err != nil {
				return err
			}
			if sub.Deadline > time.Now().Unix() {
				return ErrPluginSubmissionConflict
			}
		}
		if err := CheckPluginTaskEnabled(control, &task); err != nil {
			return err
		}
		before := task.Quota
		if err := mutate(&task, review); err != nil {
			return err
		}
		if task.Status != review.AfterStatus || task.Quota != review.AfterQuota || task.Quota < 0 {
			return ErrPluginSubmissionConflict
		}
		if review.Action == "attach" {
			if err := checkRecoveredIdentity(tx, &task); err != nil {
				return err
			}
		}
		delta = task.Quota - before
		if err := pluginQuotaDeltaTx(tx, &task, delta); err != nil {
			return err
		}
		task.Revision++
		if err := tx.Save(&task).Error; err != nil {
			return err
		}
		if review.BeforeStatus == TaskStatusSubmitUnknown || review.BeforeStatus == TaskStatusSubmitting {
			phase := "accepted"
			if task.Status == TaskStatusFailure {
				phase = "rejected"
			}
			if err := tx.Model(&TaskPluginSubmission{}).Where("task_id = ?", task.ID).Updates(map[string]any{"phase": phase, "deadline": 0, "parse_input": "", "response": ""}).Error; err != nil {
				return err
			}
		}
		review.Applied = true
		if err := tx.Save(&review).Error; err != nil {
			return err
		}
		applied = true
		return nil
	})
	if err == nil && applied {
		invalidatePluginBilling(&task, delta)
		if delta != 0 {
			recordPluginQuotaLog(&task, delta, "plugin review: "+operation)
		}
	}
	return &task, err
}

func ReadPluginReviews(ctx context.Context, id int64) ([]TaskPluginReview, error) {
	var rows []TaskPluginReview
	err := DB.WithContext(ctx).Where("task_id = ? AND applied = ?", id, true).Order("created_at DESC, operation_id").Limit(50).Find(&rows).Error
	return rows, err
}

func ClonePluginTask(task *Task) (*Task, error) {
	// Task.PrivateData is json:"-"; clone it separately for isolated previews.
	copy := *task
	raw, err := json.Marshal(task.PrivateData)
	if err != nil {
		return nil, err
	}
	copy.PrivateData = TaskPrivateData{}
	if err := json.Unmarshal(raw, &copy.PrivateData); err != nil {
		return nil, err
	}
	return &copy, nil
}
