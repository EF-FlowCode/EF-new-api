package model

import (
	"errors"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

type RiskRule struct {
	Id                  int    `json:"id"`
	Name                string `json:"name" gorm:"type:varchar(128);uniqueIndex"`
	Description         string `json:"description" gorm:"type:text"`
	Enabled             bool   `json:"enabled" gorm:"default:true;index"`
	Scope               string `json:"scope" gorm:"type:varchar(16);index"`
	Detector            string `json:"detector" gorm:"type:varchar(32);index"`
	MatchMode           string `json:"match_mode" gorm:"type:varchar(8);default:'all'"`
	Priority            int    `json:"priority" gorm:"default:0;index"`
	Action              string `json:"action" gorm:"type:varchar(16);default:'observe'"`
	AutoBlock           bool   `json:"auto_block" gorm:"default:false"`
	AutoRecover         bool   `json:"auto_recover" gorm:"default:true"`
	RecoverMode         string `json:"recover_mode" gorm:"type:varchar(16);default:'ttl'"`
	RecoverAfterSeconds int    `json:"recover_after_seconds" gorm:"default:900"`
	ResponseStatusCode  int    `json:"response_status_code" gorm:"default:429"`
	ResponseMessage     string `json:"response_message" gorm:"type:text"`
	ScoreWeight         int    `json:"score_weight" gorm:"default:10"`
	Conditions          string `json:"conditions" gorm:"type:text"`
	// Groups holds a JSON-encoded []string of group names this rule applies to.
	// Empty/"" / "[]" means "not configured" and the engine skips loading the
	// rule. The engine uses this together with the request's UsingGroup to
	// decide whether the rule participates in evaluation.
	Groups    string         `json:"groups" gorm:"column:groups;type:text"`
	Metadata  string         `json:"metadata" gorm:"type:text"`
	CreatedAt int64          `json:"created_at" gorm:"bigint;index"`
	UpdatedAt int64          `json:"updated_at" gorm:"bigint"`
	CreatedBy int            `json:"created_by" gorm:"default:0"`
	UpdatedBy int            `json:"updated_by" gorm:"default:0"`
	DeletedAt gorm.DeletedAt `json:"-" gorm:"index"`
}

func (rule *RiskRule) BeforeCreate(tx *gorm.DB) error {
	now := common.GetTimestamp()
	if rule.CreatedAt == 0 {
		rule.CreatedAt = now
	}
	rule.UpdatedAt = now
	return nil
}

func (rule *RiskRule) BeforeUpdate(tx *gorm.DB) error {
	rule.UpdatedAt = common.GetTimestamp()
	return nil
}

// ParsedGroups returns the deduped, trimmed list of groups stored in Groups.
// Returns an empty slice when the field is empty or contains an empty array.
// Parse errors are swallowed so callers can treat invalid JSON as "not
// configured" (the engine then skips the rule).
func (rule *RiskRule) ParsedGroups() []string {
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

func ListRiskRules() ([]*RiskRule, error) {
	var rules []*RiskRule
	err := DB.Order("priority desc, id asc").Find(&rules).Error
	return rules, err
}

func ListEnabledRiskRules() ([]*RiskRule, error) {
	var rules []*RiskRule
	err := DB.Where("enabled = ?", true).Order("priority desc, id asc").Find(&rules).Error
	return rules, err
}

func GetRiskRuleByID(id int) (*RiskRule, error) {
	if id <= 0 {
		return nil, errors.New("invalid rule id")
	}
	var rule RiskRule
	if err := DB.First(&rule, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &rule, nil
}

func CreateRiskRule(rule *RiskRule) error {
	if rule == nil {
		return errors.New("risk rule is nil")
	}
	rule.Name = strings.TrimSpace(rule.Name)
	return DB.Create(rule).Error
}

func UpdateRiskRule(rule *RiskRule) error {
	if rule == nil || rule.Id <= 0 {
		return errors.New("invalid risk rule")
	}
	rule.Name = strings.TrimSpace(rule.Name)
	return DB.Model(&RiskRule{}).Where("id = ?", rule.Id).Updates(map[string]interface{}{
		"name":                  rule.Name,
		"description":           rule.Description,
		"enabled":               rule.Enabled,
		"scope":                 rule.Scope,
		"detector":              rule.Detector,
		"match_mode":            rule.MatchMode,
		"priority":              rule.Priority,
		"action":                rule.Action,
		"auto_block":            rule.AutoBlock,
		"auto_recover":          rule.AutoRecover,
		"recover_mode":          rule.RecoverMode,
		"recover_after_seconds": rule.RecoverAfterSeconds,
		"response_status_code":  rule.ResponseStatusCode,
		"response_message":      rule.ResponseMessage,
		"score_weight":          rule.ScoreWeight,
		"conditions":            rule.Conditions,
		"groups":                rule.Groups,
		"metadata":              rule.Metadata,
		"updated_at":            common.GetTimestamp(),
		"updated_by":            rule.UpdatedBy,
	}).Error
}

func DeleteRiskRule(id int) error {
	if id <= 0 {
		return errors.New("invalid rule id")
	}
	return DB.Delete(&RiskRule{}, "id = ?", id).Error
}

func CountRiskRules() (int64, error) {
	var count int64
	err := DB.Model(&RiskRule{}).Count(&count).Error
	return count, err
}

// CountEnabledRiskRulesWithoutGroups counts rules that admins enabled but
// forgot to scope to any group — they will silently never fire because
// reloadRules skips them. Surfaced on the overview card so operators can spot
// the misconfiguration.
func CountEnabledRiskRulesWithoutGroups() (int64, error) {
	var count int64
	emptyGroups := DB.Where(map[string]interface{}{"groups": nil}).
		Or(map[string]interface{}{"groups": ""}).
		Or(map[string]interface{}{"groups": "[]"})
	err := DB.Model(&RiskRule{}).
		Where(map[string]interface{}{"enabled": true}).
		Where(emptyGroups).
		Count(&count).Error
	return count, err
}

// ListEnabledRiskRulesAll returns every rule with Enabled=true regardless of
// groups; used by admin diagnostics ("rules enabled but unlisted").
func ListEnabledRiskRulesAll() ([]*RiskRule, error) {
	var rules []*RiskRule
	err := DB.Where("enabled = ?", true).Find(&rules).Error
	return rules, err
}
