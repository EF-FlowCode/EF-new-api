package controller

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"eflow.local/new-api/common"
	"eflow.local/new-api/setting/billing_setting"
	"eflow.local/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestGetRatioConfigNormalizesLegacyFlatPrice(t *testing.T) {
	withTieredBillingConfig(t, map[string]string{
		"legacy-fixed": "tiered_expr",
		"dynamic":      "tiered_expr",
	}, map[string]string{
		"legacy-fixed": `tier("base", p * 3 + c * 15 + cc1h * 6)`,
		"dynamic":      `tier("base", p * 3 + c * 15) * (hour("UTC") >= 8 ? 2 : 1)`,
	})

	originalExpose := ratio_setting.IsExposeRatioEnabled()
	ratio_setting.SetExposeRatioEnabled(true)
	t.Cleanup(func() { ratio_setting.SetExposeRatioEnabled(originalExpose) })

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/ratio_config", nil)
	GetRatioConfig(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	var payload struct {
		Success bool                       `json:"success"`
		Data    map[string]json.RawMessage `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &payload))
	require.True(t, payload.Success)

	var modes map[string]string
	require.NoError(t, common.Unmarshal(payload.Data[billing_setting.BillingModeField], &modes))
	require.Equal(t, billing_setting.BillingModeFlatPrice, modes["legacy-fixed"])
	require.Equal(t, billing_setting.BillingModeTieredExpr, modes["dynamic"])
	require.Contains(t, payload.Data, "model_ratio")
	require.Contains(t, payload.Data, billing_setting.BillingExprField)
}
