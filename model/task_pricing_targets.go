package model

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"

	"eflow.local/new-api/common"
	"eflow.local/new-api/setting/billing_setting"
	"eflow.local/new-api/setting/ratio_setting"
	"gorm.io/gorm"
)

var ValidateTaskPricingTargets func(context.Context, ModelPricingConfig, []Channel, TaskPluginSnapshot, []TaskPluginSourceSnapshot, bool) error
var ResolveOfflineTaskPricingUsage func(context.Context, []Channel, TaskPluginSnapshot, []TaskPluginSourceSnapshot) (map[string]BillingUsageInfo, error)

type TaskPricingEditorModel struct {
	ModelName string                                       `json:"model_name"`
	Usage     BillingUsageInfo                             `json:"billing_usage"`
	Pricing   map[string][]billing_setting.TaskBillingRule `json:"task_pricing"`
}

// Root-only editor metadata includes paused channels. It does not expose keys,
// endpoints or script source and never changes routing/operational enablement.
func GetTaskPricingEditorModels(ctx context.Context) ([]TaskPricingEditorModel, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	result := []TaskPricingEditorModel{}
	err := DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		infos, err := getOfflineTaskPricingUsage(tx)
		if err != nil {
			return err
		}
		var option Option
		if err := tx.Where("`key` = ?", ModelPricingOption).First(&option).Error; err != nil {
			return err
		}
		var document ModelPricingConfig
		if err := json.Unmarshal([]byte(option.Value), &document); err != nil {
			return err
		}
		var selfUse Option
		if err := tx.Where("`key` = ?", "SelfUseModeEnabled").First(&selfUse).Error; err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		for name, info := range infos {
			entry := TaskPricingEditorModel{ModelName: name, Usage: info, Pricing: map[string][]billing_setting.TaskBillingRule{}}
			for plugin, profile := range info.Plugins {
				for _, source := range profile.Sources {
					rule, err := SelectConfiguredTaskRule(document, plugin, name, source.Model, selfUse.Value == "true")
					if err != nil {
						return err
					}
					entry.Pricing[plugin] = append(entry.Pricing[plugin], rule)
				}
			}
			result = append(result, entry)
		}
		return nil
	})
	sort.Slice(result, func(i, j int) bool { return result[i].ModelName < result[j].ModelName })
	return result, err
}

func getOfflineTaskPricingUsage(tx *gorm.DB) (map[string]BillingUsageInfo, error) {
	if ResolveOfflineTaskPricingUsage == nil {
		return GetBillingUsageInfo(tx.Statement.Context, tx)
	}
	state, err := readTaskPluginSnapshot(tx, false)
	if err != nil {
		return nil, err
	}
	var channels []Channel
	if err := tx.Select("id", "type", "status", "models", "model_mapping").Find(&channels).Error; err != nil {
		return nil, err
	}
	var sources []TaskPluginSourceSnapshot
	hashes := []string{}
	for _, b := range state.Production {
		hashes = append(hashes, b.SourceHash)
	}
	if len(hashes) > 0 {
		if err := tx.Where("source_hash IN ?", hashes).Find(&sources).Error; err != nil {
			return nil, err
		}
	}
	return ResolveOfflineTaskPricingUsage(tx.Statement.Context, channels, state, sources)
}

// Project the persistent document for the same selector used by live requests.
func SelectConfiguredTaskRule(document ModelPricingConfig, plugin, name, tail string, selfUse bool) (billing_setting.TaskBillingRule, error) {
	s := billing_setting.BillingSetting{TaskPriorityVersion: document.TaskPriorityVersion,
		BillingMode: map[string]string{}, BillingExpr: map[string]string{}, TaskExpressions: map[string]map[string]billing_setting.TaskPriceRule{}}
	for _, key := range []string{name, tail} {
		record := document.Models[key]
		for field, dst := range map[string]any{"mode": new(string), "expression": new(string), "task_expressions": new(map[string]billing_setting.TaskPriceRule)} {
			if raw := record[field]; len(raw) > 0 {
				if err := json.Unmarshal(raw, dst); err != nil {
					return billing_setting.TaskBillingRule{}, err
				}
			}
			switch field {
			case "mode":
				s.BillingMode[key] = *dst.(*string)
			case "expression":
				s.BillingExpr[key] = *dst.(*string)
			case "task_expressions":
				s.TaskExpressions[key] = *dst.(*map[string]billing_setting.TaskPriceRule)
			}
		}
	}
	matched := ratio_setting.FormatMatchingModelName(name)
	record := document.Models[matched]
	_, unit := record["unit_price"]
	_, ratio := record["input_ratio"]
	if strings.HasSuffix(matched, ratio_setting.CompactModelSuffix) {
		_, wildcardUnit := document.Models[ratio_setting.CompactWildcardModelKey]["unit_price"]
		_, wildcardRatio := document.Models[ratio_setting.CompactWildcardModelKey]["input_ratio"]
		unit, ratio = unit || wildcardUnit, ratio || wildcardRatio
	}
	_, builtin := ratio_setting.GetDefaultModelPriceMap()[name]
	return billing_setting.SelectTaskBillingRule(s, plugin, name, tail, unit || ratio || builtin || selfUse), nil
}

// Read only transaction-local data: no runtime cache or secondary connection.
func validateTaskPricingDocument(tx *gorm.DB, raw string, candidate *TaskPluginSnapshot) error {
	return validateTaskPricingDocumentFor(tx, raw, candidate, nil)
}

// channelIDs limits the check to those channels; nil checks every channel.
func validateTaskPricingDocumentFor(tx *gorm.DB, raw string, candidate *TaskPluginSnapshot, channelIDs []int) error {
	if ValidateTaskPricingTargets == nil || raw == "" {
		return nil
	}
	var document ModelPricingConfig
	if err := json.Unmarshal([]byte(raw), &document); err != nil {
		return err
	}
	if document.TaskPriorityVersion != 1 {
		return nil
	}
	state := TaskPluginSnapshot{}
	var err error
	if candidate != nil {
		state = *candidate
	} else {
		state, err = readTaskPluginSnapshot(tx, false)
		if err != nil {
			return err
		}
	}
	var channels []Channel
	query := tx.Select("id", "type", "status", "models", "model_mapping")
	if channelIDs != nil {
		query = query.Where("id IN ?", channelIDs)
	}
	if err := query.Find(&channels).Error; err != nil {
		return err
	}
	var sources []TaskPluginSourceSnapshot
	hashes := []string{}
	for _, b := range state.Production {
		hashes = append(hashes, b.SourceHash)
	}
	if len(hashes) > 0 {
		if err := tx.Where("source_hash IN ?", hashes).Find(&sources).Error; err != nil {
			return err
		}
	}
	var selfUse Option
	if err := tx.Where("`key` = ?", "SelfUseModeEnabled").First(&selfUse).Error; err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	return ValidateTaskPricingTargets(tx.Statement.Context, document, channels, state, sources, selfUse.Value == "true")
}

func validateCurrentTaskPricing(tx *gorm.DB, candidate *TaskPluginSnapshot) error {
	return validateCurrentTaskPricingFor(tx, candidate, nil)
}

func validateCurrentTaskPricingFor(tx *gorm.DB, candidate *TaskPluginSnapshot, channelIDs []int) error {
	if ValidateTaskPricingTargets == nil {
		return nil
	}
	// Bootstrap and isolated channel-only fixtures precede pricing initialization.
	if !tx.Migrator().HasTable(&Option{}) {
		return nil
	}
	var option Option
	if err := tx.Where("`key` = ?", ModelPricingOption).First(&option).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		return err
	}
	return validateTaskPricingDocumentFor(tx, option.Value, candidate, channelIDs)
}

// Batch writes whose model carries no primary key (tag operations) name the
// affected channels through this statement setting.
const taskPricingChannelIDsSetting = "eflow:task_pricing_channel_ids"

// channelWriteNeedsTaskPricing reports whether a channel write can change a
// task-pricing target: enabling a channel, or changing its type, models or
// model mapping. Disabling and operational fields (response time, balance,
// used quota, other info...) never need the check, since only enabled bound
// channels are validated anyway.
func channelWriteNeedsTaskPricing(stmt *gorm.Statement) bool {
	fields := map[string]any{}
	all := false
	value := func(name string) (any, bool) {
		if dest, ok := stmt.Dest.(map[string]any); ok {
			v, exists := dest[name]
			return v, exists
		}
		if stmt.Schema == nil {
			return nil, false
		}
		field := stmt.Schema.LookUpField(name)
		if field == nil {
			return nil, false
		}
		rv := reflect.Indirect(reflect.ValueOf(stmt.Dest))
		if rv.Kind() != reflect.Struct || rv.Type() != stmt.Schema.ModelType {
			return nil, false
		}
		v, zero := field.ValueOf(stmt.Context, rv)
		return v, !zero || all
	}
	column := func(name string) string {
		if stmt.Schema != nil {
			if field := stmt.Schema.LookUpField(name); field != nil {
				return field.DBName
			}
		}
		return name
	}
	switch {
	case len(stmt.Selects) > 0:
		for _, name := range stmt.Selects {
			if name == "*" {
				all = true
				continue
			}
			fields[column(name)] = nil
		}
	default:
		if dest, ok := stmt.Dest.(map[string]any); ok {
			for name := range dest {
				fields[column(name)] = nil
			}
		} else if stmt.Schema != nil {
			// Struct Updates write only non-zero fields.
			for _, field := range stmt.Schema.Fields {
				if _, set := value(field.Name); set {
					fields[field.DBName] = nil
				}
			}
		}
	}
	_, statusSet := fields["status"]
	if statusSet || all {
		for _, name := range []string{"status", "Status"} {
			if v, ok := value(name); ok {
				if fmt.Sprint(v) != strconv.Itoa(common.ChannelStatusEnabled) {
					return false
				}
				return true
			}
		}
	}
	if all {
		return true
	}
	for _, name := range []string{"type", "models", "model_mapping"} {
		if _, ok := fields[name]; ok {
			return true
		}
	}
	return false
}

// taskPricingChannelIDs returns the channels written by this statement; nil
// means unknown, in which case every channel is checked.
func taskPricingChannelIDs(tx *gorm.DB, channel *Channel) []int {
	if channel != nil && channel.Id != 0 {
		return []int{channel.Id}
	}
	if ids, ok := tx.Get(taskPricingChannelIDsSetting); ok {
		if list, ok := ids.([]int); ok && list != nil {
			return list
		}
	}
	return nil
}

// GORM hooks cover individual, tag/batch, and automatic re-enable writes. The
// enclosing write transaction rolls back when a newly enabled contract is bad.
// Only the written channels are checked, so an unrelated misconfigured binding
// cannot block auto-disable, response-time or balance writes elsewhere.
func (channel *Channel) AfterUpdate(tx *gorm.DB) error {
	if !channelWriteNeedsTaskPricing(tx.Statement) {
		return nil
	}
	ids := taskPricingChannelIDs(tx, channel)
	if ids != nil && len(ids) == 0 {
		return nil
	}
	return validateCurrentTaskPricingFor(tx.Session(&gorm.Session{NewDB: true}), nil, ids)
}

func (channel *Channel) AfterCreate(tx *gorm.DB) error {
	return validateCurrentTaskPricingFor(tx.Session(&gorm.Session{NewDB: true}), nil, taskPricingChannelIDs(tx, channel))
}
