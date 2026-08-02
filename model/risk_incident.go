package model

import (
	"strings"

	"github.com/QuantumNous/new-api/common"
)

type RiskIncident struct {
	Id             int    `json:"id"`
	CreatedAt      int64  `json:"created_at" gorm:"bigint;index"`
	SubjectType    string `json:"subject_type" gorm:"type:varchar(16);index"`
	SubjectID      int    `json:"subject_id" gorm:"index"`
	UserID         int    `json:"user_id" gorm:"index"`
	TokenID        int    `json:"token_id" gorm:"index"`
	Username       string `json:"username" gorm:"type:varchar(64);index;default:''"`
	TokenName      string `json:"token_name" gorm:"type:varchar(128);index;default:''"`
	TokenMaskedKey string `json:"token_masked_key" gorm:"type:varchar(64);default:''"`
	// Group records the group dimension of the incident at the time it
	// occurred. For legacy rows from before the v4 migration this is empty.
	Group              string `json:"group" gorm:"column:group;type:varchar(64);index;default:''"`
	RuleID             int    `json:"rule_id" gorm:"index"`
	RuleName           string `json:"rule_name" gorm:"type:varchar(128);index;default:''"`
	Detector           string `json:"detector" gorm:"type:varchar(32);index;default:''"`
	Action             string `json:"action" gorm:"type:varchar(16);index;default:'observe'"`
	Decision           string `json:"decision" gorm:"type:varchar(16);index;default:'allow'"`
	Status             string `json:"status" gorm:"type:varchar(16);index;default:'active'"`
	ResponseStatusCode int    `json:"response_status_code" gorm:"default:0"`
	ResponseMessage    string `json:"response_message" gorm:"type:text"`
	AutoRecover        bool   `json:"auto_recover" gorm:"default:false"`
	RecoverAt          int64  `json:"recover_at" gorm:"bigint;index"`
	ResolvedAt         int64  `json:"resolved_at" gorm:"bigint;index"`
	RequestID          string `json:"request_id" gorm:"type:varchar(64);index;default:''"`
	RequestPath        string `json:"request_path" gorm:"type:varchar(255);default:''"`
	RiskScore          int    `json:"risk_score" gorm:"default:0"`
	Reason             string `json:"reason" gorm:"type:text"`
	Snapshot           string `json:"snapshot" gorm:"type:text"`
	Other              string `json:"other" gorm:"type:text"`
}

type RiskIncidentQuery struct {
	Scope   string
	Action  string
	Keyword string
	Group   string
}

func CreateRiskIncident(incident *RiskIncident) error {
	if incident == nil {
		return nil
	}
	if incident.CreatedAt == 0 {
		incident.CreatedAt = common.GetTimestamp()
	}
	return DB.Create(incident).Error
}

func ListRiskIncidents(query RiskIncidentQuery, startIdx int, pageSize int) ([]*RiskIncident, int64, error) {
	var incidents []*RiskIncident
	var total int64
	tx := DB.Model(&RiskIncident{})
	if query.Scope != "" {
		tx = tx.Where("subject_type = ?", query.Scope)
	}
	if query.Action != "" {
		tx = tx.Where("action = ?", query.Action)
	}
	if query.Group != "" {
		tx = tx.Where(commonGroupCol+" = ?", query.Group)
	}
	if keyword := strings.TrimSpace(query.Keyword); keyword != "" {
		pattern := "%" + keyword + "%"
		tx = tx.Where("username LIKE ? OR token_name LIKE ? OR token_masked_key LIKE ? OR rule_name LIKE ? OR reason LIKE ?",
			pattern, pattern, pattern, pattern, pattern)
	}
	if err := tx.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err := tx.Order("created_at desc, id desc").Limit(pageSize).Offset(startIdx).Find(&incidents).Error
	return incidents, total, err
}

func DeleteExpiredRiskIncidents(cutoff int64) error {
	if cutoff <= 0 {
		return nil
	}
	return DB.Where("created_at > 0 AND created_at < ?", cutoff).Delete(&RiskIncident{}).Error
}
