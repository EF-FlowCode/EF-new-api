package controller

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"eflow.local/new-api/common"
	"eflow.local/new-api/i18n"
	"eflow.local/new-api/internal/testdb"
	"eflow.local/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func useAccountSecurityTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	gin.SetMode(gin.TestMode)
	require.NoError(t, i18n.Init())
	db := testdb.OpenMySQL8(t, &model.User{}, &model.TwoFA{}, &model.PasskeyCredential{})
	previous, previousRedis := model.DB, common.RedisEnabled
	model.DB, common.RedisEnabled = db, false
	t.Cleanup(func() { model.DB, common.RedisEnabled = previous, previousRedis })
	return db
}

func callAccountHandler(t *testing.T, handler gin.HandlerFunc, method, body string, setup func(*gin.Context)) (int, map[string]any) {
	t.Helper()
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(method, "/", strings.NewReader(body))
	ctx.Request.Header.Set("Content-Type", "application/json")
	if setup != nil {
		setup(ctx)
	}
	handler(ctx)
	var result map[string]any
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &result), recorder.Body.String())
	return recorder.Code, result
}

func TestAdminUpdateUserKeepsCustomerTiersWhenOmitted(t *testing.T) {
	db := useAccountSecurityTestDB(t)
	// Seeded directly: the preserved tiers need not be configured any more,
	// since only submitted tiers are validated.
	user := model.User{Username: "tier-keep", Password: "password-hash", AffCode: "tierk1", Group: "vip",
		AdditionalCustomerTiers: model.NewGroupList([]string{"svip"})}
	require.NoError(t, db.Create(&user).Error)
	asRoot := func(c *gin.Context) { c.Set("role", common.RoleRootUser) }

	body, err := json.Marshal(map[string]any{"id": user.Id, "username": "tier-keep", "remark": "note"})
	require.NoError(t, err)
	_, result := callAccountHandler(t, UpdateUser, http.MethodPut, string(body), asRoot)
	require.Equal(t, true, result["success"], result)

	var stored model.User
	require.NoError(t, db.First(&stored, user.Id).Error)
	require.Equal(t, "vip", stored.Group)
	require.Equal(t, []string{"svip"}, stored.AdditionalCustomerTiers.Values())
	require.Equal(t, "note", stored.Remark)

	// An explicit empty list still clears the additional tiers.
	body, err = json.Marshal(map[string]any{"id": user.Id, "username": "tier-keep", "additional_customer_tiers": []string{}})
	require.NoError(t, err)
	_, result = callAccountHandler(t, UpdateUser, http.MethodPut, string(body), asRoot)
	require.Equal(t, true, result["success"], result)
	require.NoError(t, db.First(&stored, user.Id).Error)
	require.Empty(t, stored.AdditionalCustomerTiers.Values())
	require.Equal(t, "vip", stored.Group)
}

func TestAdditionalTiersDropPrimaryDuplicate(t *testing.T) {
	require.Equal(t, []string{"svip"}, withoutPrimaryTier(model.NewGroupList([]string{"vip", "svip"}), "vip").Values())
	require.Empty(t, withoutPrimaryTier(model.NewGroupList([]string{"vip"}), "vip").Values())
}
