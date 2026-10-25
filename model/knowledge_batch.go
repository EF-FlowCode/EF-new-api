package model

import (
	"errors"
	"fmt"
	"time"

	"eflow.local/new-api/common"
	"eflow.local/new-api/constant"
	"eflow.local/new-api/pkg/knowledge"
	"gorm.io/gorm"
)

const KnowledgeBatchPlatform constant.TaskPlatform = "knowledge_batch"

type KnowledgeFile struct {
	ID         string `gorm:"primaryKey;type:varchar(64)" json:"id"`
	UserID     int    `gorm:"index" json:"-"`
	Object     string `gorm:"-" json:"object"`
	Purpose    string `gorm:"type:varchar(20)" json:"purpose"`
	Filename   string `gorm:"type:varchar(255)" json:"filename"`
	Bytes      int    `json:"bytes"`
	CreatedAt  int64  `json:"created_at"`
	Model      string `gorm:"type:varchar(191)" json:"-"`
	Contents   []byte `gorm:"type:longblob" json:"-"`
	TaskID     string `gorm:"index;type:varchar(191)" json:"-"`
	UpstreamID string `gorm:"type:varchar(191)" json:"-"`
}

// Batch state is private: neither upstream IDs nor credentials are returned by
// general task listings. Public Data contains only the OpenAI-compatible job.
type KnowledgeBatchState struct {
	InputFileID       string  `json:"input_file_id"`
	BaseURL           string  `json:"base_url"`
	InputPrice        float64 `json:"input_price"`
	QuotaPerUnit      float64 `json:"quota_per_unit"`
	GroupRatio        float64 `json:"group_ratio"`
	RequestCount      int     `json:"request_count"`
	UpstreamFileID    string  `json:"upstream_file_id,omitempty"`
	SubmissionStarted bool    `json:"submission_started,omitempty"`
	CancelRequested   bool    `json:"cancel_requested,omitempty"`
	SubmissionError   string  `json:"submission_error,omitempty"`
	OutputFileID      string  `json:"output_file_id,omitempty"`
	ErrorFileID       string  `json:"error_file_id,omitempty"`
	BilledInputTokens int     `json:"billed_input_tokens,omitempty"`
}

func CreateKnowledgeFile(file *KnowledgeFile) error {
	return DB.Transaction(func(tx *gorm.DB) error {
		var user User
		if err := lockForUpdate(tx).Select("id").First(&user, file.UserID).Error; err != nil {
			return err
		}
		var count int64
		if err := tx.Model(&KnowledgeFile{}).Where("user_id = ? AND purpose = ?", file.UserID, "batch").Count(&count).Error; err != nil {
			return err
		}
		if count >= 100 {
			return fmt.Errorf("at most 100 retained batch input files per user")
		}
		return tx.Create(file).Error
	})
}

func GetKnowledgeFile(userID int, id string) (*KnowledgeFile, error) {
	var f KnowledgeFile
	err := DB.Where("id = ? AND user_id = ?", id, userID).First(&f).Error
	f.Object = "file"
	return &f, err
}

func DeleteKnowledgeFile(userID int, id string) error {
	return DB.Transaction(func(tx *gorm.DB) error {
		var f KnowledgeFile
		if err := lockForUpdate(tx).Where("id = ? AND user_id = ?", id, userID).First(&f).Error; err != nil {
			return err
		}
		var count int64
		if err := tx.Model(&Task{}).Where("platform = ? AND user_id = ? AND status NOT IN ?", KnowledgeBatchPlatform, userID, []TaskStatus{TaskStatusSuccess, TaskStatusFailure}).Where("JSON_UNQUOTE(JSON_EXTRACT(private_data, '$.knowledge_batch.input_file_id')) = ?", id).Count(&count).Error; err != nil {
			return err
		}
		if count > 0 {
			return fmt.Errorf("file is referenced by an active batch")
		}
		return tx.Delete(&f).Error
	})
}

// Reserve and persist before the first upstream side effect. Reuse the same
// atomic wallet/token adjustment primitives as ordinary Task settlement.
func CreateKnowledgeBatch(task *Task) error {
	if task.PrivateData.KnowledgeBatch == nil || task.Quota < 0 {
		return errors.New("invalid batch reservation")
	}
	tokenKey := ""
	err := DB.Transaction(func(tx *gorm.DB) error {
		// Serialize the per-user active limit across different input files.
		var user User
		if err := lockForUpdate(tx).Select("id").First(&user, task.UserId).Error; err != nil {
			return err
		}
		var f KnowledgeFile
		if err := lockForUpdate(tx).Where("id = ? AND user_id = ?", task.PrivateData.KnowledgeBatch.InputFileID, task.UserId).First(&f).Error; err != nil {
			return err
		}
		if _, err := knowledge.ValidateBatch(f.Contents); err != nil {
			return err
		}
		var count int64
		if err := tx.Model(&Task{}).Where("platform = ? AND user_id = ? AND status NOT IN ?", KnowledgeBatchPlatform, task.UserId, []TaskStatus{TaskStatusSuccess, TaskStatusFailure}).Count(&count).Error; err != nil {
			return err
		}
		if count >= 10 {
			return fmt.Errorf("at most 10 active batches per user")
		}
		if err := adjustUserQuotaTx(tx, task.UserId, task.Quota); err != nil {
			return err
		}
		var token Token
		if err := lockForUpdate(tx).Where("id = ? AND user_id = ?", task.PrivateData.TokenId, task.UserId).First(&token).Error; err != nil {
			return err
		}
		if !token.UnlimitedQuota && token.RemainQuota < task.Quota {
			return errors.New("insufficient token quota")
		}
		tokenKey = token.Key
		if err := adjustTokenDeltaTx(tx, task.PrivateData.TokenId, task.Quota); err != nil {
			return err
		}
		return tx.Create(task).Error
	})
	if err == nil {
		updateBillingQuotaCaches(task.UserId, task.PrivateData.TokenId, tokenKey, task.Quota, task.Quota)
	}
	return err
}

func ClaimKnowledgeBatch(id int64) (bool, error) {
	now := time.Now().Unix()
	r := DB.Model(&Task{}).Where("id = ? AND platform = ? AND batch_lease_until < ? AND status NOT IN ?", id, KnowledgeBatchPlatform, now, []TaskStatus{TaskStatusSuccess, TaskStatusFailure}).Update("batch_lease_until", now+300)
	return r.RowsAffected == 1, r.Error
}

func SaveKnowledgeBatch(task *Task) error {
	r := DB.Model(&Task{}).Where("id = ? AND platform = ? AND batch_lease_until = ? AND batch_lease_until > ? AND status NOT IN ?", task.ID, KnowledgeBatchPlatform, task.BatchLeaseUntil, time.Now().Unix(), []TaskStatus{TaskStatusSuccess, TaskStatusFailure}).
		Updates(map[string]any{"private_data": task.PrivateData, "data": task.Data, "updated_at": common.GetTimestamp()})
	if r.Error != nil {
		return r.Error
	}
	if r.RowsAffected != 1 {
		// MySQL reports zero changed rows for an identical update in the same
		// second. Distinguish that no-op from losing the lease.
		var count int64
		if err := DB.Model(&Task{}).Where("id = ? AND platform = ? AND batch_lease_until = ? AND batch_lease_until > ? AND status NOT IN ?", task.ID, KnowledgeBatchPlatform, task.BatchLeaseUntil, time.Now().Unix(), []TaskStatus{TaskStatusSuccess, TaskStatusFailure}).Count(&count).Error; err != nil {
			return err
		}
		if count != 1 {
			return errors.New("batch lease lost")
		}
	}
	return nil
}

func ReleaseKnowledgeBatch(task *Task) {
	DB.Model(&Task{}).Where("id = ? AND batch_lease_until = ?", task.ID, task.BatchLeaseUntil).
		Updates(map[string]any{"batch_lease_until": 0, "updated_at": common.GetTimestamp()})
}

// Called inside the existing terminal CAS transaction. Files become visible
// only when quota settlement and the terminal task commit together.
func persistKnowledgeBatchFilesTx(tx *gorm.DB, task *Task) error {
	s := task.PrivateData.KnowledgeBatch
	if s == nil {
		return errors.New("missing batch state")
	}
	for _, entry := range []struct{ suffix, id string }{{"output", s.OutputFileID}, {"error", s.ErrorFileID}} {
		if entry.id == "" {
			continue
		}
		f := KnowledgeFile{ID: task.TaskID + "_" + entry.suffix, UserID: task.UserId, Purpose: "batch_output", Filename: entry.suffix + ".jsonl", CreatedAt: task.FinishTime, TaskID: task.TaskID, UpstreamID: entry.id}
		if err := tx.Create(&f).Error; err != nil {
			return err
		}
	}
	return nil
}
