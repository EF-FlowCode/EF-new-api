package model

import (
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// QuotaData 柱状图数据
type QuotaData struct {
	Id        int    `json:"id"`
	UserID    int    `json:"user_id" gorm:"index;uniqueIndex:idx_qdt_identity,priority:1"`
	Username  string `json:"username" gorm:"index:idx_qdt_model_user_name,priority:2;uniqueIndex:idx_qdt_identity,priority:2;size:64;default:''"`
	ModelName string `json:"model_name" gorm:"index:idx_qdt_model_user_name,priority:1;uniqueIndex:idx_qdt_identity,priority:3;size:64;default:''"`
	CreatedAt int64  `json:"created_at" gorm:"bigint;index:idx_qdt_created_at,priority:2;uniqueIndex:idx_qdt_identity,priority:4"`
	TokenUsed int    `json:"token_used" gorm:"default:0"`
	Count     int    `json:"count" gorm:"default:0"`
	Quota     int    `json:"quota" gorm:"default:0"`
}

func LogQuotaData(userId int, username string, modelName string, quota int, createdAt int64, tokenUsed int) error {
	createdAt = createdAt - (createdAt % 3600)
	quotaData := &QuotaData{
		UserID:    userId,
		Username:  username,
		ModelName: modelName,
		CreatedAt: createdAt,
		TokenUsed: tokenUsed,
		Count:     1,
		Quota:     quota,
	}
	return DB.Clauses(clause.OnConflict{
		Columns: []clause.Column{
			{Name: "user_id"},
			{Name: "username"},
			{Name: "model_name"},
			{Name: "created_at"},
		},
		DoUpdates: clause.Assignments(map[string]any{
			"count":      gorm.Expr("quota_data.count + ?", quotaData.Count),
			"quota":      gorm.Expr("quota_data.quota + ?", quotaData.Quota),
			"token_used": gorm.Expr("quota_data.token_used + ?", quotaData.TokenUsed),
		}),
	}).Create(quotaData).Error
}

func GetQuotaDataByUsername(username string, startTime int64, endTime int64) (quotaData []*QuotaData, err error) {
	var quotaDatas []*QuotaData
	// 从quota_data表中查询数据
	err = DB.Table("quota_data").Where("username = ? and created_at >= ? and created_at <= ?", username, startTime, endTime).Find(&quotaDatas).Error
	return quotaDatas, err
}

func GetQuotaDataByUserId(userId int, startTime int64, endTime int64) (quotaData []*QuotaData, err error) {
	var quotaDatas []*QuotaData
	// 从quota_data表中查询数据
	err = DB.Table("quota_data").Where("user_id = ? and created_at >= ? and created_at <= ?", userId, startTime, endTime).Find(&quotaDatas).Error
	return quotaDatas, err
}

func GetQuotaDataGroupByUser(startTime int64, endTime int64) (quotaData []*QuotaData, err error) {
	var quotaDatas []*QuotaData
	err = DB.Table("quota_data").
		Select("username, created_at, sum(count) as count, sum(quota) as quota, sum(token_used) as token_used").
		Where("created_at >= ? and created_at <= ?", startTime, endTime).
		Group("username, created_at").
		Find(&quotaDatas).Error
	return quotaDatas, err
}

// GetUserModelUsageTopN 返回指定用户最近 since 时间戳以来调用次数 TopN 的模型聚合。
// 数据来源是按消费日志实时聚合的 quota_data，避免直接扫 logs 对 LOG_DB 造成压力。
func GetUserModelUsageTopN(userId int, since int64, topN int) ([]*QuotaData, error) {
	var rows []*QuotaData
	err := DB.Table("quota_data").
		Select("model_name, SUM(count) AS count, SUM(quota) AS quota, SUM(token_used) AS token_used").
		Where("user_id = ? AND created_at >= ?", userId, since).
		Group("model_name").
		Order("count DESC").
		Limit(topN).
		Scan(&rows).Error
	return rows, err
}

// GetModelRankingData returns per-model aggregates (count / quota / token_used)
// over [start, end). end<=0 means "no upper bound". Ordered by token_used DESC.
// Only model-level rollups — no user identity is selected, so it is safe to
// expose publicly (powers the public model leaderboard).
func GetModelRankingData(start int64, end int64) ([]*QuotaData, error) {
	var rows []*QuotaData
	q := DB.Table("quota_data").
		Select("model_name, SUM(count) AS count, SUM(quota) AS quota, SUM(token_used) AS token_used").
		Where("created_at >= ?", start)
	if end > 0 {
		q = q.Where("created_at < ?", end)
	}
	err := q.Group("model_name").Order("token_used DESC").Scan(&rows).Error
	return rows, err
}

func GetAllQuotaDates(startTime int64, endTime int64, username string) (quotaData []*QuotaData, err error) {
	if username != "" {
		return GetQuotaDataByUsername(username, startTime, endTime)
	}
	var quotaDatas []*QuotaData
	// 从quota_data表中查询数据
	// only select model_name, sum(count) as count, sum(quota) as quota, model_name, created_at from quota_data group by model_name, created_at;
	//err = DB.Table("quota_data").Where("created_at >= ? and created_at <= ?", startTime, endTime).Find(&quotaDatas).Error
	err = DB.Table("quota_data").Select("model_name, sum(count) as count, sum(quota) as quota, sum(token_used) as token_used, created_at").Where("created_at >= ? and created_at <= ?", startTime, endTime).Group("model_name, created_at").Find(&quotaDatas).Error
	return quotaDatas, err
}
