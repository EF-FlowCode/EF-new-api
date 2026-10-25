package model

import (
	"slices"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type ChannelMonitoringStat struct {
	Id               int64   `json:"id" gorm:"primaryKey;autoIncrement"`
	GroupName        string  `json:"group_name" gorm:"type:varchar(64);uniqueIndex:idx_cms_group_channel"`
	ChannelId        int     `json:"channel_id" gorm:"uniqueIndex:idx_cms_group_channel"`
	ChannelName      string  `json:"channel_name" gorm:"-"`
	ChannelStatus    int     `json:"channel_status" gorm:"-"`
	AvailabilityRate float64 `json:"availability_rate" gorm:"type:decimal(8,4);default:-1"`
	CacheHitRate     float64 `json:"cache_hit_rate" gorm:"type:decimal(8,4);default:-1"`
	AvgResponseTime  int     `json:"avg_response_time" gorm:"default:0"`
	AvgFRT           int     `json:"avg_frt" gorm:"default:0"`
	LastTestTime     int64   `json:"last_test_time" gorm:"bigint;default:0"`
	LastTestModel    string  `json:"last_test_model" gorm:"type:varchar(255);default:''"`
	IsOnline         bool    `json:"is_online" gorm:"default:false"`
	UpdatedAt        int64   `json:"updated_at" gorm:"bigint"`
}

type GroupMonitoringStat struct {
	Id               int64   `json:"id" gorm:"primaryKey;autoIncrement"`
	GroupName        string  `json:"group_name" gorm:"type:varchar(64);uniqueIndex:idx_gms_group"`
	AvailabilityRate float64 `json:"availability_rate" gorm:"type:decimal(8,4);default:-1"`
	CacheHitRate     float64 `json:"cache_hit_rate" gorm:"type:decimal(8,4);default:-1"`
	AvgResponseTime  int     `json:"avg_response_time" gorm:"default:0"`
	AvgFRT           int     `json:"avg_frt" gorm:"default:0"`
	OnlineChannels   int     `json:"online_channels" gorm:"default:0"`
	TotalChannels    int     `json:"total_channels" gorm:"default:0"`
	GroupRatio       float64 `json:"group_ratio" gorm:"type:decimal(10,4);default:1"`
	LastTestModel    string  `json:"last_test_model" gorm:"type:varchar(255);default:''"`
	UpdatedAt        int64   `json:"updated_at" gorm:"bigint"`
}

type MonitoringHistory struct {
	Id               int64   `json:"id" gorm:"primaryKey;autoIncrement"`
	GroupName        string  `json:"group_name" gorm:"type:varchar(64);index:idx_mh_group_time"`
	AvailabilityRate float64 `json:"availability_rate" gorm:"type:decimal(8,4);default:-1"`
	CacheHitRate     float64 `json:"cache_hit_rate" gorm:"type:decimal(8,4);default:-1"`
	RecordedAt       int64   `json:"recorded_at" gorm:"bigint;index:idx_mh_group_time;index:idx_mh_time"`
}

func GetGroupMonitoringStatsByNames(names []string) ([]GroupMonitoringStat, error) {
	var stats []GroupMonitoringStat
	err := DB.Where("group_name IN ?", names).Find(&stats).Error
	return stats, err
}

func GetGroupMonitoringStatByName(name string) (*GroupMonitoringStat, error) {
	var stat GroupMonitoringStat
	err := DB.Where("group_name = ?", name).First(&stat).Error
	if err != nil {
		return nil, err
	}
	return &stat, nil
}

func GetGroupMonitoringStatsForPublic(names []string) ([]GroupMonitoringStat, error) {
	var stats []GroupMonitoringStat
	err := DB.Where("group_name IN ?", names).Find(&stats).Error
	return stats, err
}

func GetChannelMonitoringStatsByGroup(groupName string) ([]ChannelMonitoringStat, error) {
	var stats []ChannelMonitoringStat
	err := DB.Where("group_name = ?", groupName).Find(&stats).Error
	return stats, err
}

func GetMonitoringHistory(groupName string, startTime, endTime int64) ([]MonitoringHistory, error) {
	var history []MonitoringHistory
	err := DB.Where("group_name = ? AND recorded_at >= ? AND recorded_at <= ?", groupName, startTime, endTime).
		Order("recorded_at ASC").Find(&history).Error
	return history, err
}

func GetLastMonitoringHistoryBefore(groupName string, beforeTime int64) (*MonitoringHistory, error) {
	var h MonitoringHistory
	err := DB.Where("group_name = ? AND recorded_at < ?", groupName, beforeTime).
		Order("recorded_at DESC").First(&h).Error
	if err != nil {
		return nil, err
	}
	return &h, nil
}

func UpsertGroupMonitoringStat(stat *GroupMonitoringStat) error {
	stat.UpdatedAt = time.Now().Unix()
	return DB.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "group_name"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"availability_rate", "cache_hit_rate", "avg_response_time", "avg_frt",
			"online_channels", "total_channels", "group_ratio", "last_test_model", "updated_at",
		}),
	}).Create(stat).Error
}

func InsertMonitoringHistory(h *MonitoringHistory) error {
	return DB.Create(h).Error
}

func CleanupOldMonitoringHistory(beforeTime int64) error {
	return DB.Where("recorded_at < ?", beforeTime).Delete(&MonitoringHistory{}).Error
}

func DeleteAllMonitoringDataForGroup(groupName string) (int64, error) {
	var total int64
	tx := DB.Where("group_name = ?", groupName).Delete(&ChannelMonitoringStat{})
	if tx.Error != nil {
		return 0, tx.Error
	}
	total += tx.RowsAffected

	tx = DB.Where("group_name = ?", groupName).Delete(&GroupMonitoringStat{})
	if tx.Error != nil {
		return total, tx.Error
	}
	total += tx.RowsAffected

	tx = DB.Where("group_name = ?", groupName).Delete(&MonitoringHistory{})
	if tx.Error != nil {
		return total, tx.Error
	}
	total += tx.RowsAffected
	return total, nil
}

func GetAllChannelsByGroup(groupName string) ([]*Channel, error) {
	var channels []*Channel
	err := DB.Where("status != 0").Find(&channels).Error
	if err != nil {
		return nil, err
	}
	var result []*Channel
	for _, ch := range channels {
		groups := ch.GetGroups()
		if slices.Contains(groups, groupName) {
			result = append(result, ch)
		}
	}
	return result, nil
}

type MonitoringLogAggRow struct {
	Group     string
	ChannelId int
	LogType   int
	Count     int64
	SumTime   int64
}

func AggregateLogsForMonitoring(sinceTimestamp int64) ([]MonitoringLogAggRow, error) {
	var rows []MonitoringLogAggRow
	query := "SELECT " + logGroupCol + " as grp, channel_id, type as log_type, COUNT(*) as cnt, COALESCE(SUM(use_time),0) as sum_time " +
		"FROM logs WHERE created_at >= ? AND type IN (2, 5) " +
		"GROUP BY " + logGroupCol + ", channel_id, type"
	type rawRow struct {
		Grp       string `gorm:"column:grp"`
		ChannelId int    `gorm:"column:channel_id"`
		LogType   int    `gorm:"column:log_type"`
		Cnt       int64  `gorm:"column:cnt"`
		SumTime   int64  `gorm:"column:sum_time"`
	}
	var raw []rawRow
	err := LOG_DB.Raw(query, sinceTimestamp).Scan(&raw).Error
	if err != nil {
		return nil, err
	}
	for _, r := range raw {
		rows = append(rows, MonitoringLogAggRow{
			Group:     r.Grp,
			ChannelId: r.ChannelId,
			LogType:   r.LogType,
			Count:     r.Cnt,
			SumTime:   r.SumTime,
		})
	}
	return rows, nil
}

type MonitoringErrorRow struct {
	RequestId string
	Group     string
	ChannelId int
	ModelName string
	Content   string
	Other     string
}

// GetMonitoringErrorRows 拉取窗口内的错误日志原始行(type=5),带 content / model_name /
// other(含 status_code),供聚合时按当前排除规则逐条「读时」重新判定。
// eflow: 可用率排除从「写时」改为「读时」——错误数以 logs 为准、每次聚合按现行规则现算,
// 因此改一次排除配置即可回溯生效;成功数仍走 Redis(关消费日志时 type=2 不落库)。
// 仅扫 type=5(错误是少数)+ 命中 idx_created_at_type 索引,带 logQueryTimeout 兜底。
func GetMonitoringErrorRows(sinceTimestamp int64) ([]MonitoringErrorRow, error) {
	ctx, cancel := logQueryCtx()
	defer cancel()
	query := "SELECT request_id, " + logGroupCol + " as grp, channel_id, model_name, content, other " +
		"FROM logs WHERE created_at >= ? AND type = ? ORDER BY created_at, id"
	type rawRow struct {
		RequestId string `gorm:"column:request_id"`
		Grp       string `gorm:"column:grp"`
		ChannelId int    `gorm:"column:channel_id"`
		ModelName string `gorm:"column:model_name"`
		Content   string `gorm:"column:content"`
		Other     string `gorm:"column:other"`
	}
	var raw []rawRow
	if err := LOG_DB.WithContext(ctx).Raw(query, sinceTimestamp, LogTypeError).Scan(&raw).Error; err != nil {
		return nil, err
	}
	rows := make([]MonitoringErrorRow, 0, len(raw))
	for _, r := range raw {
		rows = append(rows, MonitoringErrorRow{
			RequestId: r.RequestId,
			Group:     r.Grp,
			ChannelId: r.ChannelId,
			ModelName: r.ModelName,
			Content:   r.Content,
			Other:     r.Other,
		})
	}
	return rows, nil
}

func GetMonitoringSuccessfulRequestIds(sinceTimestamp int64) (map[string]struct{}, error) {
	ctx, cancel := logQueryCtx()
	defer cancel()
	var ids []string
	if err := LOG_DB.WithContext(ctx).Model(&Log{}).
		Distinct("request_id").
		Where("created_at >= ? AND type = ? AND request_id <> ''", sinceTimestamp, LogTypeConsume).
		Pluck("request_id", &ids).Error; err != nil {
		return nil, err
	}
	result := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		result[id] = struct{}{}
	}
	return result, nil
}

// ResetStaleChannelMonitoringStats 把本轮聚合窗口内没有数据的渠道统计置为"无数据"：
// 可用率/缓存命中 -1、离线，保留 updated_at 作为最后一次有数据的时间，不删除行。
// cycleStart 是本轮开始时间，本轮刚写入的行 updated_at 不小于它。
func ResetStaleChannelMonitoringStats(groups []string, cycleStart int64) error {
	if len(groups) == 0 {
		return nil
	}
	return DB.Model(&ChannelMonitoringStat{}).
		Where("group_name IN ? AND updated_at < ?", groups, cycleStart).
		Where("availability_rate <> -1 OR cache_hit_rate <> -1 OR is_online = ? OR avg_response_time <> 0 OR avg_frt <> 0", true).
		UpdateColumns(map[string]any{
			"availability_rate": -1,
			"cache_hit_rate":    -1,
			"avg_response_time": 0,
			"avg_frt":           0,
			"is_online":         false,
		}).Error
}

func BatchUpsertChannelMonitoringStats(stats []*ChannelMonitoringStat) error {
	return DB.Transaction(func(tx *gorm.DB) error {
		for _, stat := range stats {
			stat.UpdatedAt = time.Now().Unix()
			if err := tx.Clauses(clause.OnConflict{
				Columns: []clause.Column{{Name: "group_name"}, {Name: "channel_id"}},
				DoUpdates: clause.AssignmentColumns([]string{
					"availability_rate", "cache_hit_rate", "avg_response_time", "avg_frt",
					"last_test_time", "last_test_model", "is_online", "updated_at",
				}),
			}).Create(stat).Error; err != nil {
				return err
			}
		}
		return nil
	})
}
