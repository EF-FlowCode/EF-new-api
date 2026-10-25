package controller

import (
	"fmt"
	"net/http"
	"testing"

	"eflow.local/new-api/common"
	"eflow.local/new-api/model"

	"github.com/stretchr/testify/require"
)

// eflow: 按上游 524455fac —— 批量删除兑换码只软删选中项，并留下带数量与请求 ID 的管理审计。

func seedBatchRedemptions(t *testing.T, n int) []model.Redemption {
	t.Helper()
	codes := make([]model.Redemption, 0, n)
	for i := 0; i < n; i++ {
		code := model.Redemption{
			UserId:      1,
			Key:         fmt.Sprintf("batch-key-%02d-%s", i, common.GetRandomString(8)),
			Status:      common.RedemptionCodeStatusEnabled,
			Name:        "batch",
			Quota:       100,
			CreatedTime: 1,
		}
		require.NoError(t, model.DB.Create(&code).Error)
		codes = append(codes, code)
	}
	return codes
}

func setupRedemptionBatchTest(t *testing.T) {
	t.Helper()
	db := openTokenControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Redemption{}, &model.User{}, &model.AuditLog{}))
	require.NoError(t, db.Create(&model.User{Id: 1, Username: "redemption-admin", Password: "test-password", Role: common.RoleAdminUser}).Error)
}

func TestDeleteRedemptionBatchSoftDeletesSelectedAndAudits(t *testing.T) {
	setupRedemptionBatchTest(t)
	codes := seedBatchRedemptions(t, 3)

	missing := codes[2].Id + 1000
	c, w := newAuthenticatedContext(t, http.MethodPost, "/api/redemption/batch", map[string]any{
		"ids": []int{codes[0].Id, codes[1].Id, missing},
	}, 1)
	c.Set("role", common.RoleAdminUser)
	c.Set("username", "redemption-admin")
	DeleteRedemptionBatch(c)

	var response struct {
		Success bool  `json:"success"`
		Data    int64 `json:"data"`
	}
	require.NoError(t, common.Unmarshal(w.Body.Bytes(), &response))
	require.True(t, response.Success, w.Body.String())
	require.EqualValues(t, 2, response.Data)

	var remaining []model.Redemption
	require.NoError(t, model.DB.Find(&remaining).Error)
	require.Len(t, remaining, 1)
	require.Equal(t, codes[2].Id, remaining[0].Id)

	// 软删除：行仍在，只是带了 deleted_at。
	var all int64
	require.NoError(t, model.DB.Unscoped().Model(&model.Redemption{}).Count(&all).Error)
	require.EqualValues(t, 3, all)

	var audits []model.AuditLog
	require.NoError(t, model.LOG_DB.Table("audit_logs").Where("action = ?", "redemption.delete_batch").Find(&audits).Error)
	require.Len(t, audits, 1)
	require.Equal(t, "Batch deleted 2 redemption codes", audits[0].Content)
	require.NotNil(t, audits[0].Other.Op)
	params := audits[0].Other.Op.Params
	require.JSONEq(t, `2`, rawAuditParam(t, params["count"]))
	require.JSONEq(t, `3`, rawAuditParam(t, params["total"]))
	require.JSONEq(t, fmt.Sprintf(`[%d,%d,%d]`, codes[0].Id, codes[1].Id, missing), rawAuditParam(t, params["requested_redemption_ids"]))
}

func TestDeleteRedemptionBatchRejectsInvalidSelections(t *testing.T) {
	setupRedemptionBatchTest(t)
	codes := seedBatchRedemptions(t, 1)

	tooMany := make([]int, 1001)
	for i := range tooMany {
		tooMany[i] = i + 1
	}
	for name, body := range map[string]any{
		"missing ids":  map[string]any{},
		"empty ids":    map[string]any{"ids": []int{}},
		"zero id":      map[string]any{"ids": []int{codes[0].Id, 0}},
		"negative id":  map[string]any{"ids": []int{-1}},
		"over limit":   map[string]any{"ids": tooMany},
		"wrong shape":  map[string]any{"ids": "1,2"},
		"not an array": []int{codes[0].Id},
	} {
		t.Run(name, func(t *testing.T) {
			c, w := newAuthenticatedContext(t, http.MethodPost, "/api/redemption/batch", body, 1)
			c.Set("role", common.RoleAdminUser)
			DeleteRedemptionBatch(c)
			var response struct {
				Success bool `json:"success"`
			}
			require.NoError(t, common.Unmarshal(w.Body.Bytes(), &response))
			require.False(t, response.Success, w.Body.String())
		})
	}

	var count int64
	require.NoError(t, model.DB.Model(&model.Redemption{}).Count(&count).Error)
	require.EqualValues(t, 1, count)
	var audits int64
	require.NoError(t, model.LOG_DB.Table("audit_logs").Where("action = ?", "redemption.delete_batch").Count(&audits).Error)
	require.Zero(t, audits)
}

func TestBatchDeleteRedemptionsValidatesBounds(t *testing.T) {
	_, err := model.BatchDeleteRedemptions(nil)
	require.Error(t, err)
	_, err = model.BatchDeleteRedemptions(make([]int, 1001))
	require.Error(t, err)
	_, err = model.BatchDeleteRedemptions([]int{1, 0})
	require.Error(t, err)
}

// rawAuditParam 把审计参数（读回时是 json.RawMessage）转成 JSON 文本。
func rawAuditParam(t *testing.T, value any) string {
	t.Helper()
	data, err := common.Marshal(value)
	require.NoError(t, err)
	return string(data)
}
