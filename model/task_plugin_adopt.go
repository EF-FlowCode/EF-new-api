package model

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"

	"eflow.local/new-api/constant"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// AdoptLegacyPluginTask changes only private identity and its source/reference
// rows. It never submits, bills, changes status or rewrites public/upstream IDs.
func (s SQLTaskPluginStore) AdoptLegacyPluginTask(ctx context.Context, id int64, expectedModel string, binding TaskPluginProductionBinding, source TaskPluginSourceSnapshot) (*Task, error) {
	identity := TaskPluginIdentity{IdentityVersion: 1, APIVersion: source.APIVersion, Key: source.Key, Version: source.Version, SourceHash: source.SourceHash, Factory: binding.Factory}
	if id <= 0 || !source.Matches(identity) || source.Key != binding.Key || source.Version != binding.Version || source.SourceHash != binding.SourceHash {
		return nil, ErrTaskPluginIdentity
	}
	db, err := s.database(ctx)
	if err != nil {
		return nil, err
	}
	var task Task
	err = db.Transaction(func(tx *gorm.DB) error {
		control, err := readTaskPluginSnapshot(tx, true)
		if err != nil {
			return err
		}
		found := false
		for _, b := range control.Production {
			if b == binding && b.Enabled {
				found = true
			}
		}
		if !found {
			return ErrTaskPluginDisabled
		}
		if err := CheckTaskPluginEnabled(control, identity); err != nil {
			return err
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&task, id).Error; err != nil {
			return err
		}
		if task.ChannelId != binding.ChannelID || !TaskPluginPlatformMatches(binding.Key, task.Platform) || task.GetUpstreamTaskID() == "" {
			return ErrTaskPluginIdentity
		}
		name := task.Properties.UpstreamModelName
		if name == "" {
			name = task.Properties.OriginModelName
		}
		if expectedModel == "" || name != expectedModel {
			return ErrTaskPluginIdentity
		}
		if task.PrivateData.Plugin != nil {
			var reference TaskPluginTaskReference
			var stored TaskPluginSourceSnapshot
			if *task.PrivateData.Plugin != identity {
				return ErrTaskPluginIdentity
			}
			if err := tx.First(&reference, "task_id = ?", task.ID).Error; err != nil {
				return err
			}
			if err := tx.First(&stored, "source_hash = ?", identity.SourceHash).Error; err != nil {
				return err
			}
			if reference.TaskPluginIdentity != identity || stored != source {
				return ErrTaskPluginIdentity
			}
			return nil
		}
		var stored TaskPluginSourceSnapshot
		err = tx.First(&stored, "source_hash = ?", source.SourceHash).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			if err := tx.Create(&source).Error; err != nil {
				return err
			}
		} else if err != nil {
			return err
		} else if stored != source {
			return ErrTaskPluginIdentity
		}
		// Old rows may use the public ID as upstream identity. Preserve that
		// interpretation explicitly before adding the plugin marker.
		task.PrivateData.UpstreamTaskID = task.GetUpstreamTaskID()
		if binding.Key == "jimeng" {
			var data struct {
				ReqKey string `json:"req_key"`
			}
			_ = json.Unmarshal(task.Data, &data)
			// Old Go submissions did not always retain the converted product.
			// Guessing it can query a different product; leave those rows intact.
			if strings.TrimSpace(data.ReqKey) == "" {
				return ErrTaskPluginIdentity
			}
			task.PrivateData.PluginContext = &TaskPluginContext{State: map[string]any{"req_key": data.ReqKey}}
		}
		if binding.Key == "sora" {
			var data map[string]any
			_ = json.Unmarshal(task.Data, &data)
			facts := map[string]any{}
			if n, ok := data["seconds"].(float64); ok && n > 0 {
				facts["seconds"] = n
			}
			if s, ok := data["seconds"].(string); ok {
				if n, err := strconv.ParseFloat(s, 64); err == nil && n > 0 {
					facts["seconds"] = n
				}
			}
			if size, ok := data["size"].(string); ok {
				facts["size"] = size
			}
			if bc := task.PrivateData.BillingContext; bc != nil {
				if seconds := bc.OtherRatios["seconds"]; seconds > 0 {
					facts["seconds"] = seconds
				}
				if _, ok := facts["size"]; !ok {
					switch bc.OtherRatios["size"] {
					case 1:
						facts["size"] = "720x1280"
					case 1.666667:
						facts["size"] = "1792x1024"
					}
				}
			}
			task.PrivateData.PluginContext = &TaskPluginContext{Usage: facts}
		}
		task.PrivateData.Plugin = &identity
		if err := tx.Model(&Task{}).Where("id = ?", id).UpdateColumn("private_data", task.PrivateData).Error; err != nil {
			return err
		}
		return tx.Create(&TaskPluginTaskReference{TaskID: id, TaskPluginIdentity: identity}).Error
	})
	if err != nil {
		return nil, err
	}
	return &task, nil
}

func TaskPluginPlatformMatches(key string, platform constant.TaskPlatform) bool {
	return key != "" && TaskPluginKeyForPlatform(platform) == key
}

func TaskPluginKeyForPlatform(platform constant.TaskPlatform) string {
	if platform == constant.TaskPlatformSuno {
		return "sunoapi"
	}
	channelType, err := strconv.Atoi(string(platform))
	if err != nil {
		return ""
	}
	return TaskPluginKeyForChannel(channelType)
}
