package model

import (
	"strings"

	"github.com/QuantumNous/new-api/common"
)

// EnforcementIncident is the audit row for the unified post-hit handling
// layer. Per decision point 1, each row carries its own EmailDelivered +
// EmailSkipReason fields rather than spawning a separate "email_sent" row;
// keeping email status and the underlying action (hit / auto_ban) in the
// same record makes it trivial to answer "did the user get notified about
// this hit" with a single SELECT.
//
// RuleHint is intentionally a coarse category (e.g. "distribution_block",
// "moderation_flag") — not the rule name. The user-facing email and audit
// surface MUST never quote the specific rule that fired so users can't
// reverse-engineer detection thresholds.
type EnforcementIncident struct {
	Id              int    `json:"id"`
	CreatedAt       int64  `json:"created_at" gorm:"bigint;index"`
	UserID          int    `json:"user_id" gorm:"index"`
	Username        string `json:"username" gorm:"type:varchar(64);index;default:''"`
	Group           string `json:"group" gorm:"column:group;type:varchar(64);index;default:''"`
	Source          string `json:"source" gorm:"type:varchar(32);index"`
	Action          string `json:"action" gorm:"type:varchar(16);index"`
	HitCountAfter   int    `json:"hit_count_after" gorm:"default:0"`
	Threshold       int    `json:"threshold" gorm:"default:0"`
	EmailDelivered  bool   `json:"email_delivered" gorm:"default:false"`
	EmailSkipReason string `json:"email_skip_reason" gorm:"type:varchar(64);default:''"`
	Reason          string `json:"reason" gorm:"type:text"`
	RuleHint        string `json:"rule_hint" gorm:"type:varchar(64);default:''"`
}

const (
	EnforcementActionHit     = "hit"
	EnforcementActionAutoBan = "auto_ban"
	EnforcementActionUnban   = "manual_unban"
	EnforcementActionReset   = "counter_reset"
	EnforcementActionTest    = "test_email"

	EnforcementEmailSkipReasonDisabled  = "disabled"
	EnforcementEmailSkipReasonRateLimit = "rate_limit"
	EnforcementEmailSkipReasonNoEmail   = "no_email"
	EnforcementEmailSkipReasonSendError = "send_error"
)

type EnforcementIncidentQuery struct {
	UserID  int
	Group   string
	Source  string
	Action  string
	Keyword string
}

func CreateEnforcementIncident(incident *EnforcementIncident) error {
	if incident == nil {
		return nil
	}
	if incident.CreatedAt == 0 {
		incident.CreatedAt = common.GetTimestamp()
	}
	return DB.Create(incident).Error
}

func ListEnforcementIncidents(query EnforcementIncidentQuery, startIdx, pageSize int) ([]*EnforcementIncident, int64, error) {
	var rows []*EnforcementIncident
	var total int64
	tx := DB.Model(&EnforcementIncident{})
	if query.UserID > 0 {
		tx = tx.Where("user_id = ?", query.UserID)
	}
	if query.Group != "" {
		tx = tx.Where(commonGroupCol+" = ?", query.Group)
	}
	if query.Source != "" {
		tx = tx.Where("source = ?", query.Source)
	}
	if query.Action != "" {
		tx = tx.Where("action = ?", query.Action)
	}
	if keyword := strings.TrimSpace(query.Keyword); keyword != "" {
		pattern := "%" + keyword + "%"
		tx = tx.Where("username LIKE ? OR rule_hint LIKE ? OR reason LIKE ?", pattern, pattern, pattern)
	}
	if err := tx.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err := tx.Order("created_at desc, id desc").Limit(pageSize).Offset(startIdx).Find(&rows).Error
	return rows, total, err
}

// CountEnforcementIncidentsBy is the building block for the overview card —
// callers pass a since timestamp + action filter to compose "hits today" /
// "auto bans today" / "emails sent today".
func CountEnforcementIncidentsBy(action string, since int64) (int64, error) {
	var n int64
	tx := DB.Model(&EnforcementIncident{}).Where("created_at >= ?", since)
	if action != "" {
		tx = tx.Where("action = ?", action)
	}
	err := tx.Count(&n).Error
	return n, err
}
