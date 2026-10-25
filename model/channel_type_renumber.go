package model

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"eflow.local/new-api/constant"
	"gorm.io/gorm"
)

// eflow: 2026-10 渠道类型重编号。私有的阿里媒体渠道原先占用 58–61，与上游
// AdvancedCustom / Sub2API / NewAPI / TaskPlugin 冲突，现挪到 1001–1004。
// 迁移只执行一次，完成后写入只读的来源记录；之后新建的 58–61 号渠道属于上游类型，不再改动。

const ChannelTypeRenumberMigrationOption = "ChannelTypeRenumberV1"

var legacyEflowChannelTypes = map[int]int{
	58: constant.ChannelTypeAliHappyhorse,
	59: constant.ChannelTypeAliTTS,
	60: constant.ChannelTypeAliASR,
	61: constant.ChannelTypeAliASRRealtime,
}

// legacyHappyhorseFactorySourceHash is the factory happyhorse plugin that
// declared channelTypes [58]. Production bindings pinned to it move to the
// renumbered factory source; bindings to any other source are left alone.
const legacyHappyhorseFactorySourceHash = "4fc583d5358d5bcd8f1bc9dbe8048087a4bef8aabc618c1dd1282d075236e93f"

type ChannelTypeRenumberReport struct {
	AlreadyApplied  bool  `json:"already_applied"`
	Channels        int64 `json:"channels"`
	Tasks           int64 `json:"tasks"`
	PluginBindings  int64 `json:"plugin_bindings"`
	AppliedAtUnixMs int64 `json:"applied_at_unix_ms,omitempty"`
}

// MigrateChannelTypeRenumberV1 moves channels and task platforms from the old
// eflow channel types to 1001–1004 and re-pins factory happyhorse bindings to
// happyhorseFactorySourceHash. Everything happens in one transaction.
func MigrateChannelTypeRenumberV1(happyhorseFactorySourceHash string) (ChannelTypeRenumberReport, error) {
	var report ChannelTypeRenumberReport
	if happyhorseFactorySourceHash == "" {
		return report, errors.New("channel type renumber: happyhorse factory source hash is required")
	}
	err := DB.Transaction(func(tx *gorm.DB) error {
		var existing Option
		err := tx.Where("`key` = ?", ChannelTypeRenumberMigrationOption).First(&existing).Error
		if err == nil {
			report.AlreadyApplied = true
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}

		channelCase, platformCase := "CASE type", "CASE platform"
		oldTypes, oldPlatforms := make([]int, 0, len(legacyEflowChannelTypes)), make([]string, 0, len(legacyEflowChannelTypes))
		for _, oldType := range []int{58, 59, 60, 61} {
			newType := legacyEflowChannelTypes[oldType]
			channelCase += fmt.Sprintf(" WHEN %d THEN %d", oldType, newType)
			platformCase += fmt.Sprintf(" WHEN '%d' THEN '%d'", oldType, newType)
			oldTypes = append(oldTypes, oldType)
			oldPlatforms = append(oldPlatforms, strconv.Itoa(oldType))
		}
		channelCase += " END"
		platformCase += " END"

		// UpdateColumn：只改编号，不触碰 updated_at 等时间戳。
		result := tx.Model(&Channel{}).Where("type IN ?", oldTypes).UpdateColumn("type", gorm.Expr(channelCase))
		if result.Error != nil {
			return fmt.Errorf("renumber channels: %w", result.Error)
		}
		report.Channels = result.RowsAffected

		result = tx.Model(&Task{}).Where("platform IN ?", oldPlatforms).UpdateColumn("platform", gorm.Expr(platformCase))
		if result.Error != nil {
			return fmt.Errorf("renumber task platforms: %w", result.Error)
		}
		report.Tasks = result.RowsAffected

		result = tx.Model(&TaskPluginProductionBinding{}).
			Where("`key` = ? AND factory = ? AND source_hash = ?", "happyhorse", true, legacyHappyhorseFactorySourceHash).
			UpdateColumn("source_hash", happyhorseFactorySourceHash)
		if result.Error != nil {
			return fmt.Errorf("re-pin happyhorse bindings: %w", result.Error)
		}
		report.PluginBindings = result.RowsAffected
		if report.PluginBindings > 0 {
			if err := tx.Model(&TaskPluginControl{}).Where("id = ?", 1).
				Update("revision", gorm.Expr("revision + 1")).Error; err != nil {
				return fmt.Errorf("bump task plugin revision: %w", err)
			}
		}

		report.AppliedAtUnixMs = time.Now().UnixMilli()
		record, err := json.Marshal(map[string]any{
			"version":                  1,
			"mapping":                  legacyEflowChannelTypes,
			"channels":                 report.Channels,
			"tasks":                    report.Tasks,
			"plugin_bindings":          report.PluginBindings,
			"happyhorse_source_before": legacyHappyhorseFactorySourceHash,
			"happyhorse_source_after":  happyhorseFactorySourceHash,
			"applied_at_unix_ms":       report.AppliedAtUnixMs,
		})
		if err != nil {
			return err
		}
		return tx.Create(&Option{Key: ChannelTypeRenumberMigrationOption, Value: string(record)}).Error
	})
	return report, err
}
