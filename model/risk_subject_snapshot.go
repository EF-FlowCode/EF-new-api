package model

import (
	"strings"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm/clause"
)

type RiskSubjectSnapshot struct {
	Id          int    `json:"id"`
	SubjectType string `json:"subject_type" gorm:"type:varchar(16);uniqueIndex:idx_risk_subject_unique_v2;index"`
	SubjectID   int    `json:"subject_id" gorm:"uniqueIndex:idx_risk_subject_unique_v2;index"`
	UserID      int    `json:"user_id" gorm:"index"`
	TokenID     int    `json:"token_id" gorm:"index"`
	Username    string `json:"username" gorm:"type:varchar(64);index;default:''"`
	TokenName   string `json:"token_name" gorm:"type:varchar(128);index;default:''"`
	TokenMaskedKey string `json:"token_masked_key" gorm:"type:varchar(64);default:''"`
	// Group is part of the unique key so that the same (scope, subjectID) can
	// have separate risk states across groups (vip vs free). Empty group rows
	// are legacy data left over from before the v4 migration and the engine
	// never writes new rows with an empty group.
	Group             string `json:"group" gorm:"column:group;type:varchar(64);uniqueIndex:idx_risk_subject_unique_v2;index;default:''"`
	Status            string `json:"status" gorm:"type:varchar(16);index;default:'normal'"`
	RiskScore         int    `json:"risk_score" gorm:"index;default:0"`
	DistinctIP10M     int    `json:"distinct_ip_10m" gorm:"default:0"`
	DistinctIP1H      int    `json:"distinct_ip_1h" gorm:"default:0"`
	DistinctUA10M     int    `json:"distinct_ua_10m" gorm:"default:0"`
	RequestCount1M    int    `json:"request_count_1m" gorm:"default:0"`
	RequestCount10M   int    `json:"request_count_10m" gorm:"default:0"`
	InflightNow       int    `json:"inflight_now" gorm:"default:0"`
	RuleHitCount24H   int    `json:"rule_hit_count_24h" gorm:"default:0"`
	ActiveRuleNames   string `json:"active_rule_names" gorm:"type:text"`
	LastRuleName      string `json:"last_rule_name" gorm:"type:varchar(128);default:''"`
	LastDecision      string `json:"last_decision" gorm:"type:varchar(16);default:'allow'"`
	LastAction        string `json:"last_action" gorm:"type:varchar(16);default:'allow'"`
	LastReason        string `json:"last_reason" gorm:"type:text"`
	LastRequestPath   string `json:"last_request_path" gorm:"type:varchar(255);default:''"`
	LastStatusCode    int    `json:"last_status_code" gorm:"default:0"`
	BlockUntil        int64  `json:"block_until" gorm:"bigint;index"`
	RecoverAt         int64  `json:"recover_at" gorm:"bigint;index"`
	AutoRecover       bool   `json:"auto_recover" gorm:"default:false"`
	LastSeenAt        int64  `json:"last_seen_at" gorm:"bigint;index"`
	LastEvaluatedAt   int64  `json:"last_evaluated_at" gorm:"bigint"`
	LastIncidentAt    int64  `json:"last_incident_at" gorm:"bigint"`
	SnapshotExtraData string `json:"snapshot_extra_data" gorm:"type:text"`
}

type RiskSubjectQuery struct {
	Scope   string
	Status  string
	Keyword string
	Group   string
}

func UpsertRiskSubjectSnapshot(snapshot *RiskSubjectSnapshot) error {
	if snapshot == nil {
		return nil
	}
	now := common.GetTimestamp()
	if snapshot.LastEvaluatedAt == 0 {
		snapshot.LastEvaluatedAt = now
	}
	if snapshot.LastSeenAt == 0 {
		snapshot.LastSeenAt = now
	}
	return DB.Clauses(clause.OnConflict{
		Columns: []clause.Column{
			{Name: "subject_type"},
			{Name: "subject_id"},
			{Name: "group"},
		},
		DoUpdates: clause.AssignmentColumns([]string{
			"user_id",
			"token_id",
			"username",
			"token_name",
			"token_masked_key",
			"status",
			"risk_score",
			"distinct_ip_10m",
			"distinct_ip_1h",
			"distinct_ua_10m",
			"request_count_1m",
			"request_count_10m",
			"inflight_now",
			"rule_hit_count_24h",
			"active_rule_names",
			"last_rule_name",
			"last_decision",
			"last_action",
			"last_reason",
			"last_request_path",
			"last_status_code",
			"block_until",
			"recover_at",
			"auto_recover",
			"last_seen_at",
			"last_evaluated_at",
			"last_incident_at",
			"snapshot_extra_data",
		}),
	}).Create(snapshot).Error
}

func GetRiskSubjectSnapshot(subjectType string, subjectID int, group string) (*RiskSubjectSnapshot, error) {
	var snapshot RiskSubjectSnapshot
	err := DB.First(&snapshot, "subject_type = ? AND subject_id = ? AND "+commonGroupCol+" = ?", subjectType, subjectID, group).Error
	if err != nil {
		return nil, err
	}
	return &snapshot, nil
}

func ListRiskSubjectSnapshots(query RiskSubjectQuery, startIdx int, pageSize int) ([]*RiskSubjectSnapshot, int64, error) {
	var snapshots []*RiskSubjectSnapshot
	var total int64
	tx := DB.Model(&RiskSubjectSnapshot{})
	if query.Scope != "" {
		tx = tx.Where("subject_type = ?", query.Scope)
	}
	if query.Status != "" {
		tx = tx.Where("status = ?", query.Status)
	}
	if query.Group != "" {
		tx = tx.Where(commonGroupCol+" = ?", query.Group)
	}
	if keyword := strings.TrimSpace(query.Keyword); keyword != "" {
		pattern := "%" + keyword + "%"
		tx = tx.Where("username LIKE ? OR token_name LIKE ? OR token_masked_key LIKE ? OR last_rule_name LIKE ?",
			pattern, pattern, pattern, pattern)
	}
	if err := tx.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err := tx.Order("risk_score desc, last_seen_at desc, id desc").
		Limit(pageSize).
		Offset(startIdx).
		Find(&snapshots).Error
	return snapshots, total, err
}

func CountRiskSubjectSnapshotsByStatus(status string) (int64, error) {
	var total int64
	err := DB.Model(&RiskSubjectSnapshot{}).Where("status = ?", status).Count(&total).Error
	return total, err
}

// CountRiskSubjectSnapshotsByStatusAndGroup powers the per-group statistics on
// the GET /api/risk/groups response.
func CountRiskSubjectSnapshotsByStatusAndGroup(status, group string) (int64, error) {
	var total int64
	err := DB.Model(&RiskSubjectSnapshot{}).
		Where("status = ? AND "+commonGroupCol+" = ?", status, group).
		Count(&total).Error
	return total, err
}

func CountHighRiskSubjectSnapshots(minRiskScore int) (int64, error) {
	var total int64
	err := DB.Model(&RiskSubjectSnapshot{}).Where("risk_score >= ?", minRiskScore).Count(&total).Error
	return total, err
}

// CountHighRiskSubjectSnapshotsByGroup powers per-group "high risk subjects"
// counter.
func CountHighRiskSubjectSnapshotsByGroup(minRiskScore int, group string) (int64, error) {
	var total int64
	err := DB.Model(&RiskSubjectSnapshot{}).
		Where("risk_score >= ? AND "+commonGroupCol+" = ?", minRiskScore, group).
		Count(&total).Error
	return total, err
}

func MarkExpiredRiskSubjectSnapshotsAsRecovered(now int64) ([]*RiskSubjectSnapshot, error) {
	var snapshots []*RiskSubjectSnapshot
	if err := DB.Where("status = ? AND block_until > 0 AND block_until <= ?", "blocked", now).Find(&snapshots).Error; err != nil {
		return nil, err
	}
	if len(snapshots) == 0 {
		return nil, nil
	}
	if err := DB.Model(&RiskSubjectSnapshot{}).
		Where("status = ? AND block_until > 0 AND block_until <= ?", "blocked", now).
		Updates(map[string]interface{}{
			"status":            "observe",
			"block_until":       int64(0),
			"recover_at":        now,
			"last_action":       "recover",
			"last_decision":     "allow",
			"last_evaluated_at": now,
		}).Error; err != nil {
		return nil, err
	}
	for _, snapshot := range snapshots {
		snapshot.Status = "observe"
		snapshot.BlockUntil = 0
		snapshot.RecoverAt = now
		snapshot.LastAction = "recover"
		snapshot.LastDecision = "allow"
		snapshot.LastEvaluatedAt = now
	}
	return snapshots, nil
}

func DeleteExpiredRiskSubjectSnapshots(cutoff int64) error {
	if cutoff <= 0 {
		return nil
	}
	return DB.Where("status <> ? AND last_seen_at > 0 AND last_seen_at < ?", "blocked", cutoff).
		Delete(&RiskSubjectSnapshot{}).Error
}
