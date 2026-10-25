package model

import (
	"sort"

	"eflow.local/new-api/common"
	"eflow.local/new-api/setting/billing_setting"
)

// MigrateLegacyFlatPriceBillingModes converts legacy single-tier fixed-price
// expressions to flat_price. It preserves expression text and is idempotent.
func MigrateLegacyFlatPriceBillingModes() ([]string, error) {
	modes := billing_setting.GetBillingModeCopy()
	expressions := billing_setting.GetBillingExprCopy()
	migrated := make([]string, 0)

	for modelName, mode := range modes {
		if mode != billing_setting.BillingModeTieredExpr {
			continue
		}
		expression, ok := expressions[modelName]
		if !ok || billing_setting.NormalizeExpressionBillingMode(mode, expression) != billing_setting.BillingModeFlatPrice {
			continue
		}
		modes[modelName] = billing_setting.BillingModeFlatPrice
		migrated = append(migrated, modelName)
	}

	if len(migrated) == 0 {
		return nil, nil
	}
	sort.Strings(migrated)
	encodedModes, err := common.Marshal(modes)
	if err != nil {
		return nil, err
	}
	if err := UpdateOptionsBulk(map[string]string{
		"billing_setting.billing_mode": string(encodedModes),
	}); err != nil {
		return nil, err
	}
	return migrated, nil
}
