package model

import (
	"strings"
	"testing"

	"eflow.local/new-api/common"
	"eflow.local/new-api/setting/billing_setting"
	"eflow.local/new-api/setting/config"
	"github.com/stretchr/testify/require"
)

func TestMigrateLegacyFlatPriceBillingModes(t *testing.T) {
	db := useModelIntegrationTestDB(t, &Option{})
	common.OptionMapRWMutex.Lock()
	savedOptionMap := common.OptionMap
	common.OptionMap = make(map[string]string)
	common.OptionMapRWMutex.Unlock()
	t.Cleanup(func() {
		common.OptionMapRWMutex.Lock()
		common.OptionMap = savedOptionMap
		common.OptionMapRWMutex.Unlock()
	})

	saved := map[string]string{}
	require.NoError(t, config.GlobalConfig.SaveToDB(func(key, value string) error {
		if strings.HasPrefix(key, "billing_setting.") {
			saved[key] = value
		}
		return nil
	}))
	t.Cleanup(func() {
		require.NoError(t, config.GlobalConfig.LoadFromDB(saved))
	})

	modes := map[string]string{
		"legacy-fixed":   billing_setting.BillingModeTieredExpr,
		"dynamic-priced": billing_setting.BillingModeTieredExpr,
		"already-flat":   billing_setting.BillingModeFlatPrice,
		"ratio-model":    billing_setting.BillingModeRatio,
	}
	expressions := map[string]string{
		"legacy-fixed":   `tier("base", p * 3 + c * 15 + cr * 0.3 + cc1h * 6)`,
		"dynamic-priced": `tier("base", p * 3 + c * 15) * (hour("UTC") >= 8 ? 2 : 1)`,
		"already-flat":   `tier("default", p * 1 + c * 2)`,
	}
	modeJSON, err := common.Marshal(modes)
	require.NoError(t, err)
	expressionJSON, err := common.Marshal(expressions)
	require.NoError(t, err)
	require.NoError(t, db.Create(&[]Option{
		{Key: "billing_setting.billing_mode", Value: string(modeJSON)},
		{Key: "billing_setting.billing_expr", Value: string(expressionJSON)},
	}).Error)
	require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{
		"billing_setting.billing_mode": string(modeJSON),
		"billing_setting.billing_expr": string(expressionJSON),
	}))

	migrated, err := MigrateLegacyFlatPriceBillingModes()
	require.NoError(t, err)
	require.Equal(t, []string{"legacy-fixed"}, migrated)

	var persisted Option
	require.NoError(t, db.Where(&Option{Key: ModelPricingOption}).First(&persisted).Error)
	projection, err := unpackModelPricing(persisted.Value)
	require.NoError(t, err)
	var persistedModes map[string]string
	require.NoError(t, common.Unmarshal([]byte(projection["billing_setting.billing_mode"]), &persistedModes))
	require.Equal(t, billing_setting.BillingModeFlatPrice, persistedModes["legacy-fixed"])
	require.Equal(t, billing_setting.BillingModeTieredExpr, persistedModes["dynamic-priced"])
	require.Equal(t, billing_setting.BillingModeFlatPrice, persistedModes["already-flat"])
	require.Equal(t, billing_setting.BillingModeRatio, persistedModes["ratio-model"])

	secondRun, err := MigrateLegacyFlatPriceBillingModes()
	require.NoError(t, err)
	require.Empty(t, secondRun)
}
