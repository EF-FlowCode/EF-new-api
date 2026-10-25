package model

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"eflow.local/new-api/common"
	"eflow.local/new-api/pkg/billingexpr"
	"eflow.local/new-api/pkg/jsplugin"
	"eflow.local/new-api/setting/billing_setting"
	"gorm.io/gorm"
)

type BillingUsageSource struct {
	Plugin     string `json:"plugin"`
	Version    string `json:"version"`
	SourceHash string `json:"source_hash"`
	Model      string `json:"model"`
}

type BillingUsageInfo struct {
	UnresolvedTargets bool                                 `json:"-"` // save-time guard; at least one channel has no provable canonical target
	Plugins           map[string]BillingUsageInfo          `json:"plugins,omitempty"`
	Status            string                               `json:"status"` // ready, unavailable or conflict
	Schema            map[string]jsplugin.UsageFieldSchema `json:"schema,omitempty"`
	Examples          []jsplugin.UsageExample              `json:"examples,omitempty"`
	Sources           []BillingUsageSource                 `json:"sources,omitempty"`
}

// Registered once during service initialization to keep model independent of
// the task runtime. Reads use live bindings; no channel credentials are selected.
var ResolveBillingUsageBindings func(context.Context, []Channel) (map[string]BillingUsageInfo, error)

func GetBillingUsageInfo(ctx context.Context, db *gorm.DB) (map[string]BillingUsageInfo, error) {
	if ResolveBillingUsageBindings == nil {
		return map[string]BillingUsageInfo{}, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var channels []Channel
	if err := db.WithContext(ctx).Select("id", "type", "models", "model_mapping").Where("status = ?", common.ChannelStatusEnabled).Find(&channels).Error; err != nil {
		return nil, err
	}
	return ResolveBillingUsageBindings(ctx, channels)
}

// Live checks apply only to new/changed usage expressions or newly activated
// expression modes. A paused plugin must not break startup or unrelated writes.
func validateChangedUsageExpressions(tx *gorm.DB, next map[string]string) error {
	if strings.TrimSpace(next["billing_setting.billing_expr"]) == "" {
		return nil
	}
	var expressions map[string]string
	if err := json.Unmarshal([]byte(next["billing_setting.billing_expr"]), &expressions); err != nil {
		return err
	}
	candidates := map[string]string{}
	for name, expression := range expressions {
		keys, err := billingexpr.UsedUsageKeys(expression)
		if err != nil {
			return err
		}
		if len(keys) > 0 {
			candidates[name] = expression
		}
	}
	if len(candidates) == 0 {
		return nil
	}
	current, err := optionValuesForUpdate(tx)
	if err != nil {
		return err
	}
	current, err = expandModelPricing(current)
	if err != nil {
		return err
	}
	var oldExpressions, oldModes, newModes map[string]string
	_ = json.Unmarshal([]byte(current["billing_setting.billing_expr"]), &oldExpressions)
	_ = json.Unmarshal([]byte(current["billing_setting.billing_mode"]), &oldModes)
	_ = json.Unmarshal([]byte(next["billing_setting.billing_mode"]), &newModes)
	for name, expression := range candidates {
		if newModes[name] == billing_setting.BillingModeFlatPrice {
			return fmt.Errorf("model %s cannot use u() with flat token pricing", name)
		}
		if strings.TrimSpace(oldExpressions[name]) == strings.TrimSpace(expression) &&
			(!billing_setting.IsExpressionBillingMode(newModes[name]) || billing_setting.IsExpressionBillingMode(oldModes[name])) {
			delete(candidates, name)
		}
	}
	if len(candidates) == 0 {
		return nil
	}
	infos, err := GetBillingUsageInfo(tx.Statement.Context, tx)
	if err != nil {
		return fmt.Errorf("cannot resolve billing usage bindings: %w", err)
	}
	for name, expression := range candidates {
		info, ok := infos[name]
		if !ok || info.Status != "ready" {
			return fmt.Errorf("model %s has no unambiguous enabled plugin usage schema", name)
		}
		if err := billingexpr.ValidateUsageExpression(expression, info.Schema); err != nil {
			return fmt.Errorf("invalid usage expression for %s: %w", name, err)
		}
	}
	return nil
}
