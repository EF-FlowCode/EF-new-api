package model

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"strings"

	"eflow.local/new-api/common"
	"eflow.local/new-api/pkg/billingexpr"
	"eflow.local/new-api/setting/billing_setting"
	"gorm.io/gorm"
)

const ModelPricingOption = "ModelPricing"
const TaskRulesFormatMigrationOption = "ModelPricingTaskRulesV1"
const ModelPricingAmountMigrationOption = "ModelPricingAmountMigrationV2"

// ModelPricingConfig is the sole persisted model pricing document. Coefficients
// retain their original units and precision: migration must not change rounding,
// implicit defaults, inactive expressions or per-request/task settlement rules.
// The old option maps are projections for calculation and import/export only.
type ModelPricingConfig struct {
	TaskPriorityVersion int                                   `json:"task_priority_version,omitempty"`
	Version             int                                   `json:"version"`
	Models              map[string]map[string]json.RawMessage `json:"models"`
}

var modelPricingFields = map[string]string{
	"ModelRatio": "input_ratio", "CompletionRatio": "output_ratio",
	"CacheRatio": "cache_read_ratio", "CreateCacheRatio": "cache_write_ratio",
	"ImageRatio": "image_input_ratio", "AudioRatio": "audio_input_ratio",
	"AudioCompletionRatio": "audio_output_ratio", "BatchInputRatio": "batch_input_ratio",
	"ModelPrice": "unit_price", "billing_setting.billing_mode": "mode",
	"billing_setting.billing_expr":        "expression",
	billing_setting.TaskExpressionsOption: "task_expressions",
}

func packModelPricing(values map[string]string) (string, error) {
	config := ModelPricingConfig{Version: 2, Models: map[string]map[string]json.RawMessage{}}
	if raw := values[billing_setting.TaskPriorityOption]; raw != "" {
		if err := json.Unmarshal([]byte(raw), &config.TaskPriorityVersion); err != nil {
			return "", err
		}
	}
	if config.TaskPriorityVersion != 0 && config.TaskPriorityVersion != 1 {
		return "", fmt.Errorf("invalid task priority version")
	}
	for key, field := range modelPricingFields {
		var entries map[string]json.RawMessage
		if raw := strings.TrimSpace(values[key]); raw != "" {
			if err := json.Unmarshal([]byte(raw), &entries); err != nil {
				return "", fmt.Errorf("%s: %w", key, err)
			}
		}
		for name, value := range entries {
			if field == "task_expressions" {
				var rules map[string]billing_setting.TaskPriceRule
				if err := json.Unmarshal(value, &rules); err != nil {
					return "", err
				}
				value, _ = json.Marshal(rules)
			}
			if field == "expression" {
				var expression string
				if err := json.Unmarshal(value, &expression); err != nil {
					return "", fmt.Errorf("invalid expression for %s: %w", name, err)
				}
				amount, err := billingexpr.AmountExpression(expression)
				if err != nil {
					return "", fmt.Errorf("invalid expression for %s: %w", name, err)
				}
				value, _ = json.Marshal(amount)
			}
			if config.Models[name] == nil {
				config.Models[name] = map[string]json.RawMessage{}
			}
			config.Models[name][field] = value
		}
	}
	encoded, err := json.Marshal(config)
	return string(encoded), err
}

func unpackModelPricing(raw string) (map[string]string, error) {
	var config ModelPricingConfig
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&config); err != nil {
		return nil, fmt.Errorf("invalid model pricing: %w", err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return nil, fmt.Errorf("trailing model pricing data")
	}
	if (config.Version != 1 && config.Version != 2) || config.Models == nil {
		return nil, fmt.Errorf("unsupported or empty model pricing document")
	}
	fields := map[string]string{}
	entries := map[string]map[string]json.RawMessage{}
	for key, field := range modelPricingFields {
		fields[field] = key
		entries[key] = map[string]json.RawMessage{}
	}
	for name, record := range config.Models {
		if strings.TrimSpace(name) == "" || record == nil {
			return nil, fmt.Errorf("invalid model pricing record %q", name)
		}
		for field, value := range record {
			key, ok := fields[field]
			if !ok {
				return nil, fmt.Errorf("unknown model pricing field %q", field)
			}
			if string(value) == "null" {
				return nil, fmt.Errorf("null model pricing field %s/%s", name, field)
			}
			if field == "expression" && config.Version == 2 {
				var expression string
				if err := json.Unmarshal(value, &expression); err != nil {
					return nil, err
				}
				if strings.TrimSpace(expression) != "" && !strings.HasPrefix(expression, "v2:") {
					return nil, fmt.Errorf("model pricing v2 requires an explicit v2 amount expression for %s", name)
				}
			}
			entries[key][name] = value
		}
	}
	if config.TaskPriorityVersion != 0 && config.TaskPriorityVersion != 1 {
		return nil, fmt.Errorf("invalid task priority version")
	}
	priority, _ := json.Marshal(config.TaskPriorityVersion)
	values := map[string]string{billing_setting.TaskPriorityOption: string(priority)}
	for key, entry := range entries {
		encoded, err := json.Marshal(entry)
		if err != nil {
			return nil, err
		}
		values[key] = string(encoded)
		if err := validateConfigOption(key, values[key]); err != nil {
			return nil, err
		}
	}
	return values, nil
}

// validateExpressionModesHaveExpressions rejects an expression billing mode
// without an expression, which would make every request for the model fail.
func validateExpressionModesHaveExpressions(values map[string]string) error {
	var modes, expressions map[string]string
	if raw := strings.TrimSpace(values["billing_setting.billing_mode"]); raw != "" {
		if err := common.Unmarshal([]byte(raw), &modes); err != nil {
			return err
		}
	}
	if raw := strings.TrimSpace(values["billing_setting.billing_expr"]); raw != "" {
		if err := common.Unmarshal([]byte(raw), &expressions); err != nil {
			return err
		}
	}
	for modelName, mode := range modes {
		if billing_setting.IsExpressionBillingMode(mode) && strings.TrimSpace(expressions[modelName]) == "" {
			return fmt.Errorf("model %s uses %s billing but has no expression", modelName, mode)
		}
	}
	return nil
}

func expandModelPricing(values map[string]string) (map[string]string, error) {
	raw, exists := values[ModelPricingOption]
	if !exists {
		return values, nil
	}
	projected, err := unpackModelPricing(raw)
	if err != nil {
		return nil, err
	}
	result := make(map[string]string, len(values)+len(projected))
	maps.Copy(result, values)
	maps.Copy(result, projected)
	return result, nil
}

// Only the migration role (or standalone master) may remove old DB options.
// Every pricing key, including auxiliary-only and disabled model records, is
// included. Group multipliers are outside this document and never rewritten.
func migrateModelPricingFromDatabase() error {
	return DB.Transaction(func(tx *gorm.DB) error {
		var options []Option
		if err := lockForUpdate(tx).Find(&options).Error; err != nil {
			return err
		}
		values := map[string]string{}
		for _, option := range options {
			values[option.Key] = option.Value
		}
		if raw, exists := values[ModelPricingOption]; exists {
			projected, err := unpackModelPricing(raw)
			if err != nil {
				return err
			}
			for key := range modelPricingFields {
				if _, old := values[key]; old {
					return fmt.Errorf("mixed model pricing storage: %s", key)
				}
			}
			upgraded, err := packModelPricing(projected)
			if err != nil {
				return err
			}
			var priorityDocument ModelPricingConfig
			if err := json.Unmarshal([]byte(raw), &priorityDocument); err != nil {
				return err
			}
			if priorityDocument.TaskPriorityVersion == 1 {
				var provenance struct {
					Version  int    `json:"version"`
					Source   string `json:"source_hash"`
					Target   string `json:"target_hash"`
					Original string `json:"original"`
				}
				if json.Unmarshal([]byte(values[TaskPricingMigrationOption]), &provenance) != nil || provenance.Version != 1 || len(provenance.Source) != 64 || len(provenance.Target) != 64 || provenance.Original == "" {
					return fmt.Errorf("task pricing migration provenance missing or invalid")
				}
			}
			if raw == upgraded {
				return nil
			}
			var document ModelPricingConfig
			if err := json.Unmarshal([]byte(raw), &document); err != nil {
				return err
			}
			for _, record := range document.Models {
				var rules map[string]json.RawMessage
				if field := record["task_expressions"]; len(field) > 0 {
					if err := json.Unmarshal(field, &rules); err != nil {
						return err
					}
					for _, rule := range rules {
						var expression string
						if json.Unmarshal(rule, &expression) == nil {
							backup, _ := json.Marshal(map[string]any{"version": 1, "source_hash": billingexpr.ExprHashString(raw), "target_hash": billingexpr.ExprHashString(upgraded), "original": raw})
							if err := recordMigrationProvenance(tx, TaskRulesFormatMigrationOption, "source_hash", backup); err != nil {
								return err
							}
							break
						}
					}
				}
			}
			if document.Version == 1 {
				if err := recordPricingAmountMigration(tx, map[string]string{ModelPricingOption: raw}, upgraded); err != nil {
					return err
				}
			}
			return saveOptionValuesWithTx(tx, map[string]string{ModelPricingOption: upgraded})
		}
		// Missing old options mean built-in defaults; freeze precisely that view.
		common.OptionMapRWMutex.RLock()
		for key := range modelPricingFields {
			if _, exists := values[key]; !exists {
				values[key] = common.OptionMap[key]
			}
		}
		common.OptionMapRWMutex.RUnlock()
		raw, err := packModelPricing(values)
		if err != nil {
			return err
		}
		if _, err := unpackModelPricing(raw); err != nil {
			return err
		}
		original := map[string]string{}
		for key := range modelPricingFields {
			original[key] = values[key]
		}
		if err := recordPricingAmountMigration(tx, original, raw); err != nil {
			return err
		}
		if err := saveOptionValuesWithTx(tx, map[string]string{ModelPricingOption: raw}); err != nil {
			return err
		}
		keys := make([]string, 0, len(modelPricingFields))
		for key := range modelPricingFields {
			keys = append(keys, key)
		}
		return tx.Where("`key` IN ?", keys).Delete(&Option{}).Error
	})
}

// API imports may still contain ratio maps. Merge them under the same locked
// option transaction and persist only the model-centric document, never both.
func consolidateModelPricingUpdate(tx *gorm.DB, updates map[string]string) (map[string]string, error) {
	if _, exists := updates[billing_setting.TaskPriorityOption]; exists {
		return nil, fmt.Errorf("task priority can only be changed by the task price migration")
	}
	_, direct := updates[ModelPricingOption]
	touched := false
	for key := range modelPricingFields {
		if _, ok := updates[key]; ok {
			touched = true
		}
	}
	if direct && touched {
		return nil, fmt.Errorf("cannot mix ModelPricing with projected pricing maps")
	}
	if !direct && !touched {
		return updates, nil
	}
	result := map[string]string{}
	for key, value := range updates {
		if _, projected := modelPricingFields[key]; !projected {
			result[key] = value
		}
	}
	if direct {
		current, err := optionValuesForUpdate(tx)
		if err != nil {
			return nil, err
		}
		current, err = expandModelPricing(current)
		if err != nil {
			return nil, err
		}
		values, err := unpackModelPricing(updates[ModelPricingOption])
		if err != nil {
			return nil, err
		}
		oldPriority := current[billing_setting.TaskPriorityOption]
		if oldPriority == "" {
			oldPriority = "0"
		}
		if values[billing_setting.TaskPriorityOption] != oldPriority {
			return nil, fmt.Errorf("task priority can only be changed by migration")
		}
		if err := validateExpressionModesHaveExpressions(values); err != nil {
			return nil, err
		}
		if err := validateChangedUsageExpressions(tx, values); err != nil {
			return nil, err
		}
		if err := validateChangedTaskExpressions(tx, values); err != nil {
			return nil, err
		}
		raw, err := packModelPricing(values)
		if err != nil {
			return nil, err
		}
		result[ModelPricingOption] = raw
		if err := validateTaskPricingDocument(tx, raw, nil); err != nil {
			return nil, err
		}
		return result, nil
	}
	current, err := optionValuesForUpdate(tx)
	if err != nil {
		return nil, err
	}
	current, err = expandModelPricing(current)
	if err != nil {
		return nil, err
	}
	maps.Copy(current, updates)
	if err := validateExpressionModesHaveExpressions(current); err != nil {
		return nil, err
	}
	if err := validateChangedUsageExpressions(tx, current); err != nil {
		return nil, err
	}
	if err := validateChangedTaskExpressions(tx, current); err != nil {
		return nil, err
	}
	raw, err := packModelPricing(current)
	if err != nil {
		return nil, err
	}
	result[ModelPricingOption] = raw
	if err := validateTaskPricingDocument(tx, raw, nil); err != nil {
		return nil, err
	}
	return result, nil
}

// Keep only pricing data, never the full options table (which contains secrets).
// The source and candidate are recorded in the same transaction as the upgrade.
func recordPricingAmountMigration(tx *gorm.DB, original map[string]string, target string) error {
	source, err := json.Marshal(original)
	if err != nil {
		return err
	}
	record, err := json.Marshal(map[string]any{
		"version": 2, "source_sha256": billingexpr.ExprHashString(string(source)),
		"target_sha256": billingexpr.ExprHashString(target), "original": original,
	})
	if err != nil {
		return err
	}
	return recordMigrationProvenance(tx, ModelPricingAmountMigrationOption, "source_sha256", record)
}

// recordMigrationProvenance keeps the first record of a one-time conversion:
// its original is the true pre-upgrade pricing and must never be replaced by
// a later, possibly already-edited state. Restarts never reach here (nothing
// to convert). If the conversion genuinely runs again (binary rolled back,
// old-format pricing written, upgraded again), the new record is stored as
// "latest" so the chain to the current ModelPricing stays auditable while the
// earliest original remains the rollback baseline.
func recordMigrationProvenance(tx *gorm.DB, key, sourceField string, record []byte) error {
	var existing Option
	err := tx.Where("`key` = ?", key).First(&existing).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return tx.Create(&Option{Key: key, Value: string(record)}).Error
	}
	if err != nil {
		return err
	}
	var stored, next map[string]json.RawMessage
	if err := json.Unmarshal([]byte(existing.Value), &stored); err != nil || stored == nil {
		return fmt.Errorf("invalid %s provenance", key)
	}
	if err := json.Unmarshal(record, &next); err != nil {
		return err
	}
	if string(stored[sourceField]) == string(next[sourceField]) {
		return nil
	}
	stored["latest"] = record
	merged, err := json.Marshal(stored)
	if err != nil {
		return err
	}
	return tx.Model(&Option{}).Where("`key` = ?", key).Update("value", string(merged)).Error
}
