package model

import (
	"strings"

	"github.com/QuantumNous/new-api/common"
)

// ModerationIncident records one OpenAI omni-moderation evaluation. We keep
// rows for both flagged and benign requests to support administrators
// debugging detection sensitivity, but the moderation engine retains them
// with two distinct TTLs (see ModerationSetting.{Flagged,Benign}RetentionHours):
// flagged rows survive long enough for downstream client-side handling
// pipelines to consume them.
type ModerationIncident struct {
	Id                int     `json:"id"`
	CreatedAt         int64   `json:"created_at" gorm:"bigint;index"`
	UserID            int     `json:"user_id" gorm:"index"`
	TokenID           int     `json:"token_id" gorm:"index"`
	Username          string  `json:"username" gorm:"type:varchar(64);default:''"`
	TokenName         string  `json:"token_name" gorm:"type:varchar(128);default:''"`
	TokenMaskedKey    string  `json:"token_masked_key" gorm:"type:varchar(64);default:''"`
	Group             string  `json:"group" gorm:"column:group;type:varchar(64);index;default:''"`
	RequestID         string  `json:"request_id" gorm:"type:varchar(64);index;default:''"`
	Model             string  `json:"model" gorm:"type:varchar(64);default:''"`
	Flagged           bool    `json:"flagged" gorm:"index;default:false"`
	MaxScore          float64 `json:"max_score" gorm:"index;default:0"`
	MaxCategory       string  `json:"max_category" gorm:"type:varchar(64);default:''"`
	Categories        string  `json:"categories" gorm:"type:text"`
	AppliedTypes      string  `json:"applied_types" gorm:"type:text"`
	InputSummary      string  `json:"input_summary" gorm:"type:text"`
	UpstreamLatencyMS int `json:"upstream_latency_ms" gorm:"default:0"`
	// Source distinguishes traffic events ("relay") from admin debug runs
	// ("debug") so debug calls don't pollute production sensitivity tuning.
	Source string `json:"source" gorm:"type:varchar(16);index;default:'relay'"`
	// Decision is the synthesized verdict produced by the rule engine. One of
	// allow / observe / flag / block. allow events are not persisted.
	Decision string `json:"decision" gorm:"type:varchar(16);index;default:'allow'"`
	// PrimaryRule is the name of the most severe rule that fired (block >
	// flag > observe). Empty when no rule matched (which never happens in v3
	// because incidents only land here when at least one rule matches).
	PrimaryRule string `json:"primary_rule" gorm:"type:varchar(128);index;default:''"`
	// MatchedRules holds the JSON-encoded []ModerationMatchedRule slice. We
	// keep all matched rules (not just the primary) so admins can audit
	// overlapping coverage without re-running the model.
	MatchedRules string `json:"matched_rules" gorm:"type:text"`
}

type ModerationIncidentQuery struct {
	Group   string
	Source  string
	Flagged *bool
	Keyword string
	UserID  int
}

func GetModerationIncident(id int) (*ModerationIncident, error) {
	var row ModerationIncident
	if err := DB.First(&row, id).Error; err != nil {
		return nil, err
	}
	return &row, nil
}

func CreateModerationIncident(incident *ModerationIncident) error {
	if incident == nil {
		return nil
	}
	if incident.CreatedAt == 0 {
		incident.CreatedAt = common.GetTimestamp()
	}
	return DB.Create(incident).Error
}

func ListModerationIncidents(query ModerationIncidentQuery, startIdx, pageSize int) ([]*ModerationIncident, int64, error) {
	var rows []*ModerationIncident
	var total int64
	tx := DB.Model(&ModerationIncident{})
	if query.Group != "" {
		tx = tx.Where(commonGroupCol+" = ?", query.Group)
	}
	if query.Source != "" {
		tx = tx.Where("source = ?", query.Source)
	}
	if query.Flagged != nil {
		tx = tx.Where("flagged = ?", *query.Flagged)
	}
	if query.UserID > 0 {
		tx = tx.Where("user_id = ?", query.UserID)
	}
	if keyword := strings.TrimSpace(query.Keyword); keyword != "" {
		pattern := "%" + keyword + "%"
		tx = tx.Where("username LIKE ? OR token_name LIKE ? OR token_masked_key LIKE ? OR max_category LIKE ? OR input_summary LIKE ?",
			pattern, pattern, pattern, pattern, pattern)
	}
	if err := tx.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err := tx.Order("created_at desc, id desc").Limit(pageSize).Offset(startIdx).Find(&rows).Error
	if err == nil {
		for _, r := range rows {
			if len(r.InputSummary) > 200 {
				r.InputSummary = r.InputSummary[:200] + "..."
			}
		}
	}
	return rows, total, err
}

// CountFlaggedModerationIncidentsSince returns the count of flagged rows
// since the given unix timestamp. Used by the moderation overview card.
func CountFlaggedModerationIncidentsSince(since int64) (int64, error) {
	var n int64
	err := DB.Model(&ModerationIncident{}).
		Where("flagged = ? AND created_at >= ?", true, since).
		Count(&n).Error
	return n, err
}

// DeleteExpiredModerationIncidents deletes rows according to the two-tier TTL:
// flagged rows are kept for flaggedCutoff seconds; benign for benignCutoff.
// Both must be unix timestamps (now - retentionSeconds).
func DeleteExpiredModerationIncidents(flaggedCutoff, benignCutoff int64) error {
	if flaggedCutoff > 0 {
		if err := DB.Where("flagged = ? AND created_at > 0 AND created_at < ?", true, flaggedCutoff).
			Delete(&ModerationIncident{}).Error; err != nil {
			return err
		}
	}
	if benignCutoff > 0 {
		if err := DB.Where("flagged = ? AND created_at > 0 AND created_at < ?", false, benignCutoff).
			Delete(&ModerationIncident{}).Error; err != nil {
			return err
		}
	}
	return nil
}
