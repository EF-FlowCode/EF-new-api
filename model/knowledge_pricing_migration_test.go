package model

import (
	"strings"
	"testing"

	"eflow.local/new-api/common"
	"eflow.local/new-api/pkg/billingexpr"
	"eflow.local/new-api/pkg/knowledge"
	"github.com/stretchr/testify/require"
)

func TestMigrateLegacyKnowledgePricesValues(t *testing.T) {
	db := useModelIntegrationTestDB(t, &Option{})

	input := 0.5
	image := 1.0
	batch := 0.25
	legacyJSON, err := common.Marshal(map[string]knowledge.Prices{
		"qwen3.7-text-embedding": {Input: &input, Image: &image, BatchInput: &batch},
	})
	require.NoError(t, err)
	values := map[string]string{"billing_setting.knowledge_prices": string(legacyJSON)}

	require.NoError(t, migrateLegacyKnowledgePricesValues(values))
	_, legacyStillPresent := values["billing_setting.knowledge_prices"]
	require.False(t, legacyStillPresent)

	var persisted []Option
	require.NoError(t, db.Find(&persisted).Error)
	byKey := make(map[string]string, len(persisted))
	for _, option := range persisted {
		byKey[option.Key] = option.Value
	}

	var modelRatios map[string]float64
	require.NoError(t, common.Unmarshal([]byte(byKey["ModelRatio"]), &modelRatios))
	require.Equal(t, 0.25, modelRatios["qwen3.7-text-embedding"])

	var imageRatios map[string]float64
	require.NoError(t, common.Unmarshal([]byte(byKey["ImageRatio"]), &imageRatios))
	require.Equal(t, 2.0, imageRatios["qwen3.7-text-embedding"])

	var batchRatios map[string]float64
	require.NoError(t, common.Unmarshal([]byte(byKey["BatchInputRatio"]), &batchRatios))
	require.Equal(t, 0.125, batchRatios["qwen3.7-text-embedding"])

	var modes map[string]string
	require.NoError(t, common.Unmarshal([]byte(byKey["billing_setting.billing_mode"]), &modes))
	require.Equal(t, "tiered_expr", modes["qwen3.7-text-embedding"])

	var expressions map[string]string
	require.NoError(t, common.Unmarshal([]byte(byKey["billing_setting.billing_expr"]), &expressions))
	expression := expressions["qwen3.7-text-embedding"]
	require.Equal(t, "v2:(tier(\"knowledge\", p * 0.5 + img * 1 + c * 0)\n) / 1000000", expression)
	amount, _, err := billingexpr.RunExpr(expression, billingexpr.TokenParams{P: 1000, Img: 200, C: 999})
	require.NoError(t, err)
	// $0.5/M input + $1/M image, zero output: $0.0007 = 350 quota.
	require.InDelta(t, 0.0007, amount, 1e-12)
	require.InDelta(t, 350, billingexpr.ExpressionQuota(amount, expression, 500000), 1e-9)
	require.NotContains(t, byKey, "billing_setting.knowledge_prices")
}

func TestLoadOptionsFromDatabaseRejectsLegacyKnowledgePricesOutsideMigrationRole(t *testing.T) {
	db := useModelIntegrationTestDB(t, &Option{})
	legacyJSON, err := common.Marshal(map[string]knowledge.Prices{
		"qwen3.7-text-embedding": {Input: func() *float64 { value := 0.5; return &value }()},
	})
	require.NoError(t, err)
	require.NoError(t, db.Create(&Option{Key: "billing_setting.knowledge_prices", Value: string(legacyJSON)}).Error)

	savedRole, savedMaster := common.DeploymentRole, common.IsMasterNode
	common.DeploymentRole = "web"
	common.IsMasterNode = false
	t.Cleanup(func() {
		common.DeploymentRole, common.IsMasterNode = savedRole, savedMaster
	})

	err = loadOptionsFromDatabase()
	require.Error(t, err)
	require.True(t, strings.Contains(err.Error(), "requires a migration role"))
}

func TestShouldRunLegacyKnowledgePriceMigration(t *testing.T) {
	savedRole, savedMaster := common.DeploymentRole, common.IsMasterNode
	t.Cleanup(func() {
		common.DeploymentRole, common.IsMasterNode = savedRole, savedMaster
	})

	common.DeploymentRole = "migrate"
	common.IsMasterNode = false
	require.True(t, shouldRunLegacyKnowledgePriceMigration())

	common.DeploymentRole = "web"
	common.IsMasterNode = false
	require.False(t, shouldRunLegacyKnowledgePriceMigration())

	common.DeploymentRole = ""
	common.IsMasterNode = true
	require.True(t, shouldRunLegacyKnowledgePriceMigration())

}
