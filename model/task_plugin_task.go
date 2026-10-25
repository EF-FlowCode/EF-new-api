package model

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"

	"gorm.io/gorm"
)

var (
	ErrTaskPluginIdentity   = errors.New("invalid task plugin identity")
	ErrTaskPluginMissing    = errors.New("task plugin source or version is missing")
	ErrTaskPluginDisabled   = errors.New("task plugin execution is disabled")
	ErrTaskPluginReferenced = errors.New("plugin version is referenced by a task")
)

// TaskPluginIdentity belongs to private_data, never to public Task DTOs.
// Factory is provenance, not a request to resolve today's embedded version.
type TaskPluginIdentity struct {
	IdentityVersion int    `json:"identity_version"`
	APIVersion      int    `json:"api_version"`
	Key             string `json:"key" gorm:"size:128;index:idx_task_plugin_ref_version,priority:1"`
	Version         string `json:"version" gorm:"type:varbinary(64);index:idx_task_plugin_ref_version,priority:2"`
	SourceHash      string `json:"source_hash" gorm:"type:varbinary(64);index"`
	Factory         bool   `json:"factory" gorm:"index:idx_task_plugin_ref_version,priority:3"`
}

func (i TaskPluginIdentity) Validate() error {
	if i.IdentityVersion != 1 || i.APIVersion != 1 || len(i.Key) == 0 || len(i.Key) > 128 || len(i.Version) == 0 || len(i.Version) > 64 || len(i.SourceHash) != 64 {
		return ErrTaskPluginIdentity
	}
	for _, c := range i.SourceHash {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return ErrTaskPluginIdentity
		}
	}
	return nil
}

// Source is deduplicated by content hash, including the metadata in the script.
// No cleanup API is provided: historical references are retained deliberately.
type TaskPluginSourceSnapshot struct {
	SourceHash string `json:"source_hash" gorm:"primaryKey;type:varbinary(64)"`
	Key        string `json:"key" gorm:"size:128;not null"`
	Version    string `json:"version" gorm:"type:varbinary(64);not null"`
	APIVersion int    `json:"api_version" gorm:"not null"`
	Source     string `json:"-" gorm:"type:mediumtext;not null"`
}

func (s TaskPluginSourceSnapshot) Matches(i TaskPluginIdentity) bool {
	return i.Validate() == nil && len(s.Source) > 0 && len(s.Source) <= 1<<20 && s.Key == i.Key && s.Version == i.Version && s.APIVersion == i.APIVersion && s.SourceHash == i.SourceHash && fmt.Sprintf("%x", sha256.Sum256([]byte(s.Source))) == i.SourceHash
}

type TaskPluginTaskReference struct {
	TaskID             int64 `gorm:"primaryKey;autoIncrement:false"`
	TaskPluginIdentity `gorm:"embedded"`
}

type TaskPluginStoredTask struct {
	Task   Task
	Source TaskPluginSourceSnapshot
}

type TaskPluginTaskRead struct {
	Control TaskPluginSnapshot
	Tasks   []TaskPluginStoredTask
}

// CheckTaskPluginEnabled is shared by transactional insertion and query
// preparation. Inactivation does not invalidate a pinned historical version.
func CheckTaskPluginEnabled(s TaskPluginSnapshot, i TaskPluginIdentity) error {
	if err := i.Validate(); err != nil {
		return err
	}
	if !s.Enabled {
		return ErrTaskPluginDisabled
	}
	for _, p := range s.Plugins {
		if p.Key == i.Key && p.Active && !p.Enabled {
			return ErrTaskPluginDisabled
		}
	}
	if i.Factory {
		if !s.FactoryEnabled {
			return ErrTaskPluginDisabled
		}
		if slices.Contains(s.DisabledFactory, i.Key) {
			return ErrTaskPluginDisabled
		}
		return nil
	}
	for _, p := range s.Plugins {
		if p.Key == i.Key && p.Version == i.Version {
			if p.SourceHash != i.SourceHash || p.APIVersion != i.APIVersion {
				return ErrTaskPluginIdentity
			}
			if !p.Enabled {
				return ErrTaskPluginDisabled
			}
			return nil
		}
	}
	return ErrTaskPluginMissing
}

// CreatePluginTask is an internal persistence boundary, not provider submission.
// It accepts a NEW task only; rollback leaves the caller's object untouched.
func (s SQLTaskPluginStore) CreatePluginTask(ctx context.Context, task Task, source TaskPluginSourceSnapshot) (*Task, error) {
	i := task.PrivateData.Plugin
	if task.ID != 0 || task.UserId <= 0 || task.ChannelId <= 0 || strings.TrimSpace(task.TaskID) == "" || len(task.TaskID) > 191 || strings.TrimSpace(task.PrivateData.UpstreamTaskID) == "" || i == nil || !source.Matches(*i) {
		return nil, ErrTaskPluginIdentity
	}
	identity := *i
	task.PrivateData.Plugin = &identity
	db, err := s.database(ctx)
	if err != nil {
		return nil, err
	}
	err = db.Transaction(func(tx *gorm.DB) error {
		control, err := readTaskPluginSnapshot(tx, true)
		if err != nil {
			return err
		}
		if err := CheckTaskPluginEnabled(control, identity); err != nil {
			return err
		}
		var existing TaskPluginSourceSnapshot
		err = tx.First(&existing, "source_hash = ?", source.SourceHash).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			if err := tx.Create(&source).Error; err != nil {
				return err
			}
		} else if err != nil {
			return err
		} else if existing != source {
			return ErrTaskPluginIdentity
		}
		if err := tx.Create(&task).Error; err != nil {
			return err
		}
		return tx.Create(&TaskPluginTaskReference{TaskID: task.ID, TaskPluginIdentity: identity}).Error
	})
	if err != nil {
		return nil, err
	}
	return &task, nil
}

// ReadPluginTasks reads control, tasks, references and source from one MySQL
// snapshot. It validates persisted associations rather than trusting caller JSON.
func (s SQLTaskPluginStore) ReadPluginTasks(ctx context.Context, ids []int64) (TaskPluginTaskRead, error) {
	var out TaskPluginTaskRead
	if len(ids) == 0 || len(ids) > 100 {
		return out, ErrTaskPluginIdentity
	}
	seen := map[int64]bool{}
	for _, id := range ids {
		if id <= 0 || seen[id] {
			return out, ErrTaskPluginIdentity
		}
		seen[id] = true
	}
	db, err := s.database(ctx)
	if err != nil {
		return out, err
	}
	err = db.Transaction(func(tx *gorm.DB) error {
		var err error
		out.Control, err = readTaskPluginSnapshot(tx, false)
		if err != nil {
			return err
		}
		var tasks []Task
		if err := tx.Where("id IN ?", ids).Find(&tasks).Error; err != nil {
			return err
		}
		if len(tasks) != len(ids) {
			return gorm.ErrRecordNotFound
		}
		var refs []TaskPluginTaskReference
		if err := tx.Where("task_id IN ?", ids).Find(&refs).Error; err != nil {
			return err
		}
		refMap := map[int64]TaskPluginIdentity{}
		for _, ref := range refs {
			refMap[ref.TaskID] = ref.TaskPluginIdentity
		}
		taskMap := map[int64]Task{}
		for _, task := range tasks {
			taskMap[task.ID] = task
		}
		sources := map[string]TaskPluginSourceSnapshot{}
		for _, id := range ids {
			task := taskMap[id]
			ref, referenced := refMap[id]
			row := TaskPluginStoredTask{Task: task}
			if task.PrivateData.Plugin == nil {
				if referenced {
					return ErrTaskPluginIdentity
				}
			} else {
				if !referenced || ref != *task.PrivateData.Plugin || ref.Validate() != nil {
					return ErrTaskPluginIdentity
				}
				source, exists := sources[ref.SourceHash]
				if !exists {
					if err := tx.First(&source, "source_hash = ?", ref.SourceHash).Error; err != nil {
						if errors.Is(err, gorm.ErrRecordNotFound) {
							return ErrTaskPluginMissing
						}
						return err
					}
					sources[ref.SourceHash] = source
				}
				if !source.Matches(ref) {
					return ErrTaskPluginIdentity
				}
				row.Source = source
			}
			out.Tasks = append(out.Tasks, row)
		}
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return TaskPluginTaskRead{}, err
	}
	return out, nil
}
