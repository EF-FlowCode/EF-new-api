package model

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"reflect"
	"slices"
	"time"

	"eflow.local/new-api/common"
	"eflow.local/new-api/constant"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// TaskPlugin stores immutable source for a key/version. Activation and remarks
// are mutable; Source is only returned through the root-only detail endpoint.
type TaskPlugin struct {
	Id         int64  `json:"id"`
	Key        string `json:"key" gorm:"size:128;not null;uniqueIndex:uk_task_plugin_key_version,priority:1"`
	APIVersion int    `json:"api_version" gorm:"not null"`
	Version    string `json:"version" gorm:"type:varbinary(64);not null;uniqueIndex:uk_task_plugin_key_version,priority:2"`
	Source     string `json:"-" gorm:"type:mediumtext;not null"`
	SourceHash string `json:"source_hash" gorm:"size:64;not null"`
	Icon       string `json:"-" gorm:"type:mediumtext"`
	Enabled    bool   `json:"enabled" gorm:"not null"`
	Active     bool   `json:"active" gorm:"not null;index"`
	CreatedAt  int64  `json:"created_at" gorm:"not null;autoCreateTime:false"`
	Remark     string `json:"remark" gorm:"type:text"`
}

// All writers lock this singleton before reading versions. Locking only the
// target version would allow simultaneous uploads to create two active rows.
type TaskPluginControl struct {
	Id              int    `gorm:"primaryKey;autoIncrement:false"`
	Revision        uint64 `gorm:"not null"`
	Enabled         bool   `gorm:"not null"`
	FactoryEnabled  bool   `gorm:"not null"`
	DisabledFactory string `gorm:"type:text;not null"`
}

type TaskPluginSnapshot struct {
	Revision        uint64                        `json:"revision"`
	Enabled         bool                          `json:"enabled"`
	FactoryEnabled  bool                          `json:"factory_enabled"`
	DisabledFactory []string                      `json:"disabled_factory"`
	Plugins         []TaskPlugin                  `json:"-"`
	Bindings        []TaskPluginShadowBinding     `json:"-"`
	Production      []TaskPluginProductionBinding `json:"-"`
}

// Shadow bindings are separate from Channel JSON, so ordinary channel edits,
// cloning and imports cannot write or inherit this Root-only configuration.
type TaskPluginShadowBinding struct {
	ChannelID     int    `json:"channel_id" gorm:"primaryKey;autoIncrement:false"`
	Key           string `json:"key" gorm:"size:128;not null"`
	Version       string `json:"version" gorm:"type:varbinary(64);not null"`
	SourceHash    string `json:"source_hash" gorm:"size:64;not null"`
	Factory       bool   `json:"factory"`
	FactorySource string `json:"-" gorm:"type:mediumtext"`
	Enabled       bool   `json:"enabled"`
	SampleEvery   uint32 `json:"sample_every"`
}

// Production bindings are intentionally separate from shadow observations.
// They are disabled by default; admitted providers are listed below.
type TaskPluginProductionBinding struct {
	ChannelID  int    `json:"channel_id" gorm:"primaryKey;autoIncrement:false"`
	Key        string `json:"key" gorm:"size:128;not null"`
	Version    string `json:"version" gorm:"type:varbinary(64);not null"`
	SourceHash string `json:"source_hash" gorm:"size:64;not null"`
	Factory    bool   `json:"factory"`
	Enabled    bool   `json:"enabled"`
}

// TaskPluginChannelType maps the eleven bundled providers to their primary
// legacy channel types. TaskPluginKeyForChannel also recognizes aliases.
func TaskPluginChannelType(key string) int {
	switch key {
	case "alibaba":
		return constant.ChannelTypeAli
	case "happyhorse":
		return constant.ChannelTypeAliHappyhorse
	case "doubao":
		return constant.ChannelTypeDoubaoVideo
	case "google":
		return constant.ChannelTypeGemini
	case "hailuo":
		return constant.ChannelTypeMiniMax
	case "jimeng":
		return constant.ChannelTypeJimeng
	case "kling":
		return constant.ChannelTypeKling
	case "sora":
		return constant.ChannelTypeSora
	case "sunoapi":
		return constant.ChannelTypeSunoAPI
	case "vertex-ai":
		return constant.ChannelTypeVertexAi
	case "vidu":
		return constant.ChannelTypeVidu
	default:
		return 0
	}
}

func TaskPluginKeyForChannel(channelType int) string {
	if channelType == constant.ChannelTypeVolcEngine {
		return "doubao"
	}
	if channelType == constant.ChannelTypeOpenAI {
		return "sora"
	}
	for _, key := range []string{"alibaba", "happyhorse", "doubao", "google", "hailuo", "jimeng", "kling", "sora", "sunoapi", "vertex-ai", "vidu"} {
		if TaskPluginChannelType(key) == channelType {
			return key
		}
	}
	return ""
}

func (s TaskPluginSnapshot) Clone() TaskPluginSnapshot {
	s.Plugins = slices.Clone(s.Plugins)
	s.DisabledFactory = slices.Clone(s.DisabledFactory)
	s.Bindings = slices.Clone(s.Bindings)
	s.Production = slices.Clone(s.Production)
	return s
}

func ensureTaskPluginControl(db *gorm.DB) error {
	return db.Clauses(clause.OnConflict{DoNothing: true}).Create(&TaskPluginControl{
		Id: 1, Revision: 1, Enabled: false, FactoryEnabled: true, DisabledFactory: "[]",
	}).Error
}

// SQLTaskPluginStore uses the configured application database. Tests supply an
// isolated MySQL handle; it never opens or migrates a database on its own.
type SQLTaskPluginStore struct {
	DB func() *gorm.DB
}

func (s SQLTaskPluginStore) database(ctx context.Context) (*gorm.DB, error) {
	if s.DB == nil {
		return nil, errors.New("task plugin database is unavailable")
	}
	db := s.DB()
	if db == nil {
		return nil, errors.New("task plugin database is unavailable")
	}
	return db.WithContext(ctx), nil
}

func readTaskPluginSnapshot(tx *gorm.DB, lock bool) (TaskPluginSnapshot, error) {
	var state TaskPluginControl
	query := tx
	if lock {
		query = query.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	if err := query.First(&state, 1).Error; err != nil {
		return TaskPluginSnapshot{}, err
	}
	snapshot := TaskPluginSnapshot{Revision: state.Revision, Enabled: state.Enabled, FactoryEnabled: state.FactoryEnabled}
	if err := common.Unmarshal([]byte(state.DisabledFactory), &snapshot.DisabledFactory); err != nil {
		return TaskPluginSnapshot{}, err
	}
	if err := tx.Order("id ASC").Find(&snapshot.Plugins).Error; err != nil {
		return TaskPluginSnapshot{}, err
	}
	if err := tx.Order("channel_id ASC").Find(&snapshot.Bindings).Error; err != nil {
		return TaskPluginSnapshot{}, err
	}
	if err := tx.Order("channel_id ASC").Find(&snapshot.Production).Error; err != nil {
		return TaskPluginSnapshot{}, err
	}
	return snapshot, nil
}

func (s SQLTaskPluginStore) Read(ctx context.Context) (TaskPluginSnapshot, error) {
	db, err := s.database(ctx)
	if err != nil {
		return TaskPluginSnapshot{}, err
	}
	var snapshot TaskPluginSnapshot
	err = db.Transaction(func(tx *gorm.DB) error {
		snapshot, err = readTaskPluginSnapshot(tx, false)
		return err
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	return snapshot, err
}

// Update validates a complete candidate under the cross-node writer lock.
// Callback errors roll back both version changes and the revision. Returning
// the committed snapshot lets the caller publish exactly what was saved.
func (s SQLTaskPluginStore) Update(ctx context.Context, change func(*TaskPluginSnapshot) error) (TaskPluginSnapshot, error) {
	db, err := s.database(ctx)
	if err != nil {
		return TaskPluginSnapshot{}, err
	}
	var snapshot TaskPluginSnapshot
	err = db.Transaction(func(tx *gorm.DB) error {
		var readErr error
		snapshot, readErr = readTaskPluginSnapshot(tx, true)
		if readErr != nil {
			return readErr
		}
		previous := snapshot.Clone()
		if err := change(&snapshot); err != nil {
			return err
		}
		active := make(map[string]bool)
		for _, row := range snapshot.Plugins {
			if row.Active {
				if active[row.Key] {
					return errors.New("multiple active versions for plugin " + row.Key)
				}
				active[row.Key] = true
			}
		}
		if reflect.DeepEqual(previous, snapshot) {
			return nil
		}
		if err := writeTaskPluginBindings(tx, previous, snapshot); err != nil {
			return err
		}
		if err := writeTaskPluginProduction(tx, previous, snapshot); err != nil {
			return err
		}
		if err := validateCurrentTaskPricing(tx, &snapshot); err != nil {
			return err
		}
		if previous.Revision == math.MaxUint64 {
			return errors.New("task plugin revision exhausted")
		}
		old := make(map[int64]TaskPlugin, len(previous.Plugins))
		for _, row := range previous.Plugins {
			old[row.Id] = row
		}
		for i := range snapshot.Plugins {
			row := &snapshot.Plugins[i]
			if row.Id == 0 {
				row.CreatedAt = time.Now().Unix()
				if err := tx.Create(row).Error; err != nil {
					return err
				}
				continue
			}
			before, exists := old[row.Id]
			if !exists || before.Key != row.Key || before.Version != row.Version || before.Source != row.Source || before.SourceHash != row.SourceHash || before.APIVersion != row.APIVersion || before.CreatedAt != row.CreatedAt {
				return errors.New("plugin version source is immutable")
			}
			delete(old, row.Id)
			if before != *row {
				if err := tx.Model(row).Updates(map[string]any{
					"active": row.Active, "enabled": row.Enabled, "remark": row.Remark, "icon": row.Icon,
				}).Error; err != nil {
					return err
				}
			}
		}
		for id := range old {
			row := old[id]
			var references int64
			if err := tx.Model(&TaskPluginTaskReference{}).Where("`key` = ? AND version = ? AND factory = ?", row.Key, row.Version, false).Count(&references).Error; err != nil {
				return err
			}
			if references > 0 {
				return ErrTaskPluginReferenced
			}
			if err := tx.Delete(&TaskPlugin{}, id).Error; err != nil {
				return err
			}
		}
		disabled, err := common.Marshal(snapshot.DisabledFactory)
		if err != nil {
			return err
		}
		snapshot.Revision = previous.Revision + 1
		return tx.Model(&TaskPluginControl{}).Where("id = ?", 1).Updates(map[string]any{
			"revision": snapshot.Revision, "enabled": snapshot.Enabled,
			"factory_enabled": snapshot.FactoryEnabled, "disabled_factory": string(disabled),
		}).Error
	})
	return snapshot, err
}

func writeTaskPluginBindings(tx *gorm.DB, previous, next TaskPluginSnapshot) error {
	old := map[int]TaskPluginShadowBinding{}
	for _, b := range previous.Bindings {
		old[b.ChannelID] = b
	}
	seen := map[int]bool{}
	for _, b := range next.Bindings {
		if seen[b.ChannelID] || b.ChannelID <= 0 || b.Key != "alibaba" || b.SampleEvery < 1 || b.SampleEvery > 10000 {
			return errors.New("invalid shadow binding")
		}
		seen[b.ChannelID] = true
		if !b.Factory {
			found := false
			for _, p := range next.Plugins {
				if p.Key == b.Key && p.Version == b.Version && p.SourceHash == b.SourceHash {
					found = true
					break
				}
			}
			if !found {
				return errors.New("shadow binding references missing plugin version")
			}
		}
		before, exists := old[b.ChannelID]
		delete(old, b.ChannelID)
		if exists && before == b {
			continue
		}
		if b.Enabled {
			var ch Channel
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id", "type").First(&ch, b.ChannelID).Error; err != nil {
				return err
			}
			if ch.Type != constant.ChannelTypeAli {
				return errors.New("shadow binding requires an Alibaba channel")
			}
		}
		if err := tx.Save(&b).Error; err != nil {
			return err
		}
	}
	for id := range old {
		if err := tx.Delete(&TaskPluginShadowBinding{}, id).Error; err != nil {
			return err
		}
	}
	return nil
}

func writeTaskPluginProduction(tx *gorm.DB, previous, next TaskPluginSnapshot) error {
	old := map[int]TaskPluginProductionBinding{}
	for _, b := range previous.Production {
		old[b.ChannelID] = b
	}
	seen := map[int]bool{}
	for _, b := range next.Production {
		if seen[b.ChannelID] || b.ChannelID <= 0 || TaskPluginChannelType(b.Key) == 0 || b.Version == "" || len(b.SourceHash) != 64 {
			return errors.New("invalid production binding")
		}
		seen[b.ChannelID] = true
		var ch Channel
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id", "type").First(&ch, b.ChannelID).Error; err != nil {
			return err
		}
		if TaskPluginKeyForChannel(ch.Type) != b.Key {
			return errors.New("production binding does not match channel type")
		}
		found := false
		if b.Factory {
			for _, p := range next.Plugins {
				if p.Key == b.Key && p.Version == b.Version && p.SourceHash == b.SourceHash {
					found = true
					break
				}
			}
			// The factory source is checked by Manager before the store update.
			if !found && b.SourceHash == "" {
				return errors.New("production factory source is missing")
			}
		} else {
			for _, p := range next.Plugins {
				if p.Key == b.Key && p.Version == b.Version && p.SourceHash == b.SourceHash {
					found = true
					break
				}
			}
			if !found {
				return errors.New("production binding references missing plugin version")
			}
		}
		before, exists := old[b.ChannelID]
		delete(old, b.ChannelID)
		if exists && before == b {
			continue
		}
		if err := tx.Save(&b).Error; err != nil {
			return err
		}
	}
	for id := range old {
		var references int64
		if err := tx.Model(&TaskPluginTaskReference{}).Joins("JOIN tasks ON tasks.id = task_plugin_task_references.task_id").Where("tasks.channel_id = ?", id).Count(&references).Error; err != nil {
			return err
		}
		if references > 0 {
			return ErrTaskPluginReferenced
		}
		if err := tx.Delete(&TaskPluginProductionBinding{}, id).Error; err != nil {
			return err
		}
	}
	return nil
}
