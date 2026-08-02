package model

import (
	"errors"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/types"
	"gorm.io/gorm"
)

// ModerationRule encodes one admin-authored decision rule against the
// OpenAI omni-moderation response. Conditions are evaluated together with
// the rule's MatchMode (any/all) — the engine never short-circuits on the
// rule's MatchMode default; whatever the admin saved is what runs.
//
// Groups follows the same "未配置 = 不启用" semantics as RiskRule: a rule
// with empty Groups is dropped at reload time, regardless of Enabled.
type ModerationRule struct {
	Id          int            `json:"id"`
	Name        string         `json:"name" gorm:"type:varchar(128);uniqueIndex"`
	Description string         `json:"description" gorm:"type:text"`
	Enabled     bool           `json:"enabled" gorm:"default:false;index"`
	MatchMode   string         `json:"match_mode" gorm:"type:varchar(8);default:'all'"`
	Action      string         `json:"action" gorm:"type:varchar(16);default:'observe'"`
	Priority    int            `json:"priority" gorm:"default:0;index"`
	ScoreWeight int            `json:"score_weight" gorm:"default:10"`
	Conditions  string         `json:"conditions" gorm:"type:text"`
	Groups      string         `json:"groups" gorm:"type:text"`
	Metadata    string         `json:"metadata" gorm:"type:text"`
	CreatedAt   int64          `json:"created_at" gorm:"bigint;index"`
	UpdatedAt   int64          `json:"updated_at" gorm:"bigint"`
	CreatedBy   int            `json:"created_by" gorm:"default:0"`
	UpdatedBy   int            `json:"updated_by" gorm:"default:0"`
	DeletedAt   gorm.DeletedAt `json:"-" gorm:"index"`
}

func (rule *ModerationRule) BeforeCreate(tx *gorm.DB) error {
	now := common.GetTimestamp()
	if rule.CreatedAt == 0 {
		rule.CreatedAt = now
	}
	rule.UpdatedAt = now
	return nil
}

func (rule *ModerationRule) BeforeUpdate(tx *gorm.DB) error {
	rule.UpdatedAt = common.GetTimestamp()
	return nil
}

// ParsedGroups returns the deduped, trimmed list of groups this rule applies
// to. Returns nil for empty / invalid JSON so reload code can use a single
// "len() == 0" check to decide skip-at-load semantics.
func (rule *ModerationRule) ParsedGroups() []string {
	if rule == nil {
		return nil
	}
	raw := strings.TrimSpace(rule.Groups)
	if raw == "" {
		return nil
	}
	var arr []string
	if err := common.UnmarshalJsonStr(raw, &arr); err != nil {
		return nil
	}
	seen := make(map[string]struct{}, len(arr))
	out := make([]string, 0, len(arr))
	for _, g := range arr {
		g = strings.TrimSpace(g)
		if g == "" {
			continue
		}
		if _, ok := seen[g]; ok {
			continue
		}
		seen[g] = struct{}{}
		out = append(out, g)
	}
	return out
}

// ParsedConditions returns []ModerationCondition; on JSON parse error returns
// nil so the engine drops the rule rather than firing on garbage.
func (rule *ModerationRule) ParsedConditions() []types.ModerationCondition {
	if rule == nil {
		return nil
	}
	raw := strings.TrimSpace(rule.Conditions)
	if raw == "" {
		return nil
	}
	var arr []types.ModerationCondition
	if err := common.UnmarshalJsonStr(raw, &arr); err != nil {
		return nil
	}
	return arr
}

func ListModerationRules() ([]*ModerationRule, error) {
	var rules []*ModerationRule
	err := DB.Order("priority desc, id asc").Find(&rules).Error
	return rules, err
}

func ListEnabledModerationRules() ([]*ModerationRule, error) {
	var rules []*ModerationRule
	err := DB.Where("enabled = ?", true).Order("priority desc, id asc").Find(&rules).Error
	return rules, err
}

func ListEnabledModerationRulesAll() ([]*ModerationRule, error) {
	var rules []*ModerationRule
	err := DB.Where("enabled = ?", true).Find(&rules).Error
	return rules, err
}

func GetModerationRuleByID(id int) (*ModerationRule, error) {
	if id <= 0 {
		return nil, errors.New("invalid rule id")
	}
	var rule ModerationRule
	if err := DB.First(&rule, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &rule, nil
}

func CreateModerationRule(rule *ModerationRule) error {
	if rule == nil {
		return errors.New("moderation rule is nil")
	}
	rule.Name = strings.TrimSpace(rule.Name)
	return DB.Create(rule).Error
}

func UpdateModerationRule(rule *ModerationRule) error {
	if rule == nil || rule.Id <= 0 {
		return errors.New("invalid moderation rule")
	}
	rule.Name = strings.TrimSpace(rule.Name)
	return DB.Model(&ModerationRule{}).Where("id = ?", rule.Id).Updates(map[string]interface{}{
		"name":         rule.Name,
		"description":  rule.Description,
		"enabled":      rule.Enabled,
		"match_mode":   rule.MatchMode,
		"action":       rule.Action,
		"priority":     rule.Priority,
		"score_weight": rule.ScoreWeight,
		"conditions":   rule.Conditions,
		"groups":       rule.Groups,
		"metadata":     rule.Metadata,
		"updated_at":   common.GetTimestamp(),
		"updated_by":   rule.UpdatedBy,
	}).Error
}

func DeleteModerationRule(id int) error {
	if id <= 0 {
		return errors.New("invalid rule id")
	}
	return DB.Delete(&ModerationRule{}, "id = ?", id).Error
}

func CountModerationRules() (int64, error) {
	var n int64
	err := DB.Model(&ModerationRule{}).Count(&n).Error
	return n, err
}

// CountEnabledModerationRulesWithoutGroups powers the overview "未配置分组的
// 规则数" indicator — same role as the matching counter on the risk-control
// side, mirrored here so each engine surfaces its own misconfiguration.
func CountEnabledModerationRulesWithoutGroups() (int64, error) {
	var n int64
	err := DB.Model(&ModerationRule{}).
		Where("enabled = ? AND (groups IS NULL OR groups = '' OR groups = '[]')", true).
		Count(&n).Error
	return n, err
}
