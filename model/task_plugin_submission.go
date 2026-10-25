package model

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"eflow.local/new-api/common"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var ErrPluginSubmissionConflict = errors.New("plugin task state or idempotency key conflicts")
var ErrPluginReservationDenied = errors.New("account or token is unavailable or quota is insufficient")

const (
	TaskStatusSubmitting    = "SUBMITTING"
	TaskStatusSubmitUnknown = "SUBMIT_UNKNOWN"
)

// Existing tasks keep their pinned source across channel rebindings, while
// still respecting the channel's production switch and plugin family.
func CheckPluginTaskEnabled(control TaskPluginSnapshot, task *Task) error {
	if task == nil || task.PrivateData.Plugin == nil {
		return ErrTaskPluginIdentity
	}
	if err := CheckTaskPluginEnabled(control, *task.PrivateData.Plugin); err != nil {
		return err
	}
	for _, binding := range control.Production {
		if binding.ChannelID == task.ChannelId && binding.Enabled && binding.Key == task.PrivateData.Plugin.Key {
			return nil
		}
	}
	return ErrTaskPluginDisabled
}

// Private evidence for a single attempt. Never expose this row through public DTOs.
type TaskPluginSubmission struct {
	TaskID      int64  `gorm:"primaryKey;autoIncrement:false"`
	ScopeKey    string `gorm:"type:char(64);uniqueIndex"`
	Fingerprint string `gorm:"type:char(64);not null"`
	Phase       string `gorm:"type:varchar(20);index"`
	Deadline    int64  `gorm:"index"`
	RetryAfter  int64  `gorm:"index"`
	ParseInput  string `gorm:"type:mediumtext"`
	Response    string `gorm:"type:mediumtext"`
}

func PluginSubmissionScope(user, token int, operation, key string) string {
	raw, _ := json.Marshal([]any{user, token, operation, key})
	return fmt.Sprintf("%x", sha256.Sum256(raw))
}

// BeginPluginSubmission owns reservation, immutable source/reference and idempotency
// in one transaction. Control locking serializes duplicate keys across processes.
func (s SQLTaskPluginStore) BeginPluginSubmission(ctx context.Context, task Task, source TaskPluginSourceSnapshot, sub TaskPluginSubmission) (*Task, bool, error) {
	if task.ID != 0 || task.UserId <= 0 || task.Quota < 0 || task.PrivateData.Plugin == nil || !source.Matches(*task.PrivateData.Plugin) || len(sub.ScopeKey) != 64 || len(sub.Fingerprint) != 64 || len(sub.ParseInput) > 2<<20 {
		return nil, false, ErrTaskPluginIdentity
	}
	db, err := s.database(ctx)
	if err != nil {
		return nil, false, err
	}
	created := false
	tokenKey := ""
	err = db.Transaction(func(tx *gorm.DB) error {
		control, err := readTaskPluginSnapshot(tx, true)
		if err != nil {
			return err
		}
		var existing TaskPluginSubmission
		err = tx.Where("scope_key = ?", sub.ScopeKey).First(&existing).Error
		if err == nil {
			if existing.Fingerprint != sub.Fingerprint {
				return ErrPluginSubmissionConflict
			}
			return tx.First(&task, existing.TaskID).Error
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if err := CheckPluginTaskEnabled(control, &task); err != nil {
			return err
		}
		binding, found := TaskPluginProductionBinding{}, false
		for _, b := range control.Production {
			if b.ChannelID == task.ChannelId {
				binding, found = b, true
				break
			}
		}
		id := task.PrivateData.Plugin
		if !found || !binding.Enabled || binding.Key != id.Key || binding.Version != id.Version || binding.Factory != id.Factory || binding.SourceHash != id.SourceHash {
			return ErrTaskPluginDisabled
		}
		var user User
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&user, task.UserId).Error; err != nil {
			return err
		}
		if user.Status != common.UserStatusEnabled || user.Quota < task.Quota {
			return ErrPluginReservationDenied
		}
		if err := adjustUserQuotaTx(tx, task.UserId, task.Quota); err != nil {
			return err
		}
		if task.PrivateData.TokenId > 0 {
			var token Token
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND user_id = ?", task.PrivateData.TokenId, task.UserId).First(&token).Error; err != nil {
				return err
			}
			if token.Status != common.TokenStatusEnabled || token.ExpiredTime != -1 && token.ExpiredTime < time.Now().Unix() || !token.UnlimitedQuota && token.RemainQuota < task.Quota {
				return ErrPluginReservationDenied
			}
			tokenKey = token.Key
			if err := adjustTokenDeltaTx(tx, token.Id, task.Quota); err != nil {
				return err
			}
		}
		var saved TaskPluginSourceSnapshot
		err = tx.First(&saved, "source_hash = ?", source.SourceHash).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			if err = tx.Create(&source).Error; err != nil {
				return err
			}
		} else if err != nil {
			return err
		} else if saved != source {
			return ErrTaskPluginIdentity
		}
		task.Status, task.Progress, task.Revision = TaskStatusSubmitting, "0%", 1
		task.Data = json.RawMessage(`{}`)
		if err := tx.Create(&task).Error; err != nil {
			return err
		}
		if err := tx.Create(&TaskPluginTaskReference{TaskID: task.ID, TaskPluginIdentity: *id}).Error; err != nil {
			return err
		}
		sub.TaskID, sub.Phase, sub.Deadline = task.ID, "prepared", time.Now().Add(time.Minute).Unix()
		if err := tx.Create(&sub).Error; err != nil {
			return err
		}
		if err := tx.Model(&User{}).Where("id = ?", task.UserId).Updates(map[string]any{"used_quota": gorm.Expr("used_quota + ?", task.Quota), "request_count": gorm.Expr("request_count + 1")}).Error; err != nil {
			return err
		}
		if err := tx.Model(&Channel{}).Where("id = ?", task.ChannelId).UpdateColumn("used_quota", gorm.Expr("used_quota + ?", task.Quota)).Error; err != nil {
			return err
		}
		if err := tx.Create(&TaskPluginReview{OperationID: fmt.Sprintf("reserve:%d", task.ID), TaskID: task.ID, Action: "reserve", Applied: true, AfterStatus: task.Status, AfterQuota: task.Quota, CreatedAt: time.Now().Unix()}).Error; err != nil {
			return err
		}
		created = true
		return nil
	})
	if err != nil {
		return nil, false, err
	}
	if created {
		updateBillingQuotaCaches(task.UserId, task.PrivateData.TokenId, tokenKey, task.Quota, task.Quota)
		recordPluginQuotaLog(&task, task.Quota, "plugin reservation")
	}
	return &task, created, nil
}

func FindPluginSubmission(ctx context.Context, scope, fingerprint string) (*Task, error) {
	var sub TaskPluginSubmission
	if err := DB.WithContext(ctx).Where("scope_key = ?", scope).First(&sub).Error; err != nil {
		return nil, err
	}
	if sub.Fingerprint != fingerprint {
		return nil, ErrPluginSubmissionConflict
	}
	var task Task
	if err := DB.WithContext(ctx).First(&task, sub.TaskID).Error; err != nil {
		return nil, err
	}
	return &task, nil
}

func ClaimPluginSubmission(ctx context.Context, id int64) error {
	return DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var task Task
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&task, id).Error; err != nil {
			return err
		}
		if task.Status != TaskStatusSubmitting {
			return ErrPluginSubmissionConflict
		}
		r := tx.Model(&TaskPluginSubmission{}).Where("task_id = ? AND phase = ? AND deadline > ?", id, "prepared", time.Now().Unix()).Updates(map[string]any{"phase": "sending", "deadline": time.Now().Add(time.Minute).Unix()})
		if r.Error != nil {
			return r.Error
		}
		if r.RowsAffected != 1 {
			return ErrPluginSubmissionConflict
		}
		return tx.Model(&task).Updates(map[string]any{"status": TaskStatusSubmitUnknown, "revision": gorm.Expr("revision + 1")}).Error
	})
}

func SavePluginSubmissionResponse(ctx context.Context, id int64, response string) error {
	if len(response) > 2<<20 {
		return errors.New("submission response exceeds evidence limit")
	}
	r := DB.WithContext(ctx).Model(&TaskPluginSubmission{}).Where("task_id = ? AND phase = ?", id, "sending").Updates(map[string]any{"response": response, "phase": "response"})
	if r.Error != nil {
		return r.Error
	}
	if r.RowsAffected != 1 {
		return ErrPluginSubmissionConflict
	}
	return nil
}

// Acceptance does not touch funds. A crash after this transaction is replayable
// from the public ID without a second charge or provider request.
func AcceptPluginSubmission(ctx context.Context, id int64, upstream string, data []byte, state map[string]any) (*Task, error) {
	if strings.TrimSpace(upstream) == "" || len(upstream) > 2048 || !json.Valid(data) {
		return nil, ErrTaskPluginIdentity
	}
	var task Task
	err := DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		control, err := readTaskPluginSnapshot(tx, true)
		if err != nil {
			return err
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&task, id).Error; err != nil {
			return err
		}
		if task.Status != TaskStatusSubmitUnknown {
			return ErrPluginSubmissionConflict
		}
		if task.PrivateData.Plugin == nil {
			return ErrTaskPluginIdentity
		}
		if err := CheckPluginTaskEnabled(control, &task); err != nil {
			return err
		}
		task.PrivateData.UpstreamTaskID = upstream
		if err := checkRecoveredIdentity(tx, &task); err != nil {
			return err
		}
		if task.PrivateData.PluginContext == nil {
			return ErrTaskPluginIdentity
		}
		task.PrivateData.PluginContext.State = state
		task.Data, task.Status, task.Revision = data, TaskStatusSubmitted, task.Revision+1
		if err := tx.Save(&task).Error; err != nil {
			return err
		}
		return tx.Model(&TaskPluginSubmission{}).Where("task_id = ?", id).Updates(map[string]any{"phase": "accepted", "deadline": 0, "response": "", "parse_input": ""}).Error
	})
	return &task, err
}

func GetPluginSubmission(ctx context.Context, id int64) (TaskPluginSubmission, error) {
	var sub TaskPluginSubmission
	err := DB.WithContext(ctx).First(&sub, "task_id = ?", id).Error
	return sub, err
}

// Called under the plugin control lock so concurrent recoveries cannot attach
// the same provider task to two local reservations.
func checkRecoveredIdentity(tx *gorm.DB, task *Task) error {
	var count int64
	err := tx.Model(&Task{}).Where("id <> ? AND channel_id = ? AND JSON_UNQUOTE(JSON_EXTRACT(private_data, '$.upstream_task_id')) = ? AND JSON_UNQUOTE(JSON_EXTRACT(private_data, '$.key')) = ?", task.ID, task.ChannelId, task.PrivateData.UpstreamTaskID, task.PrivateData.Key).Count(&count).Error
	if err != nil {
		return err
	}
	if count > 0 {
		return ErrPluginSubmissionConflict
	}
	return nil
}
