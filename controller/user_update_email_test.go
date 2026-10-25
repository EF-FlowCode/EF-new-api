package controller

import (
	"fmt"
	"net/http"
	"testing"

	"eflow.local/new-api/common"
	"eflow.local/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUpdateUserEmailFieldSemantics(t *testing.T) {
	db := useStepUpTestDB(t)
	hash, err := common.Password2Hash("root-password-1")
	require.NoError(t, err)
	root := model.User{Username: "email-root", Password: hash, AffCode: "emrt01", Role: common.RoleRootUser, Status: common.UserStatusEnabled}
	require.NoError(t, db.Create(&root).Error)
	user := model.User{
		Username: "email-update-user",
		Password: "stored-password",
		Email:    "before@example.com",
		Role:     common.RoleCommonUser,
		Status:   common.UserStatusEnabled,
		Group:    "default",
		AffCode:  "emus01",
	}
	require.NoError(t, db.Create(&user).Error)
	h := newStepUpHarness(t, &root)
	h.engine.PUT("/user", func(c *ginContext) { asStepUpUser(t, c); UpdateUser(c) })

	update := func(body string, proof string) map[string]any {
		t.Helper()
		_, result := h.do(http.MethodPut, "/user", body, proof)
		return result
	}
	proof := func() string {
		return h.proof(fmt.Sprintf(`{"method":"password","scope":"admin.user.update","context":{"user_id":%d},"password":"root-password-1"}`, user.Id))
	}
	readEmail := func() string {
		t.Helper()
		var stored model.User
		require.NoError(t, db.First(&stored, user.Id).Error)
		return stored.Email
	}

	result := update(fmt.Sprintf(`{"id":%d,"username":"email-update-user","role":1,"group":"default"}`, user.Id), "")
	require.Equal(t, true, result["success"], result)
	assert.Equal(t, "before@example.com", readEmail(), "omitted email must be preserved")

	result = update(fmt.Sprintf(`{"id":%d,"username":"email-update-user","role":1,"group":"default","email":"after@example.com"}`, user.Id), "")
	require.Equal(t, "SECURITY_PROOF_REQUIRED", result["code"], "changing the recovery email needs a proof")
	assert.Equal(t, "before@example.com", readEmail())

	result = update(fmt.Sprintf(`{"id":%d,"username":"email-update-user","role":1,"group":"default","email":"after@example.com"}`, user.Id), proof())
	require.Equal(t, true, result["success"], result)
	assert.Equal(t, "after@example.com", readEmail(), "provided email must be stored")

	result = update(fmt.Sprintf(`{"id":%d,"username":"email-update-user","role":1,"group":"default","email":""}`, user.Id), proof())
	require.Equal(t, true, result["success"], result)
	assert.Empty(t, readEmail(), "explicitly empty email must clear the binding")
}

// admin-api changes a user's group by sending back the whole object it read
// from GET /api/user/:id with only "group" replaced. That must not need a proof.
func TestUpdateUserGroupRoundTripNeedsNoProof(t *testing.T) {
	db := useStepUpTestDB(t)
	root := model.User{Username: "group-root", Password: "unused", AffCode: "grrt01", Role: common.RoleRootUser, Status: common.UserStatusEnabled}
	require.NoError(t, db.Create(&root).Error)
	user := model.User{Username: "group-user", Password: "secret-hash", Email: "keep@example.com", Role: common.RoleCommonUser,
		Status: common.UserStatusEnabled, Group: "default", AffCode: "grus01", Remark: "note"}
	require.NoError(t, db.Create(&user).Error)
	original := common.TopupGroupRatio2JSONString()
	require.NoError(t, common.UpdateTopupGroupRatioByJSONString(`{"default":1,"vip":1.2}`))
	t.Cleanup(func() { require.NoError(t, common.UpdateTopupGroupRatioByJSONString(original)) })
	h := newStepUpHarness(t, &root)
	h.engine.GET("/user/:id", func(c *ginContext) { asStepUpUser(t, c); GetUser(c) })
	h.engine.PUT("/user", func(c *ginContext) { asStepUpUser(t, c); UpdateUser(c) })

	_, fetched := h.do(http.MethodGet, fmt.Sprintf("/user/%d", user.Id), "", "")
	require.Equal(t, true, fetched["success"], fetched)
	data := fetched["data"].(map[string]any)
	data["group"] = "vip"
	body, err := common.Marshal(data)
	require.NoError(t, err)

	_, result := h.do(http.MethodPut, "/user", string(body), "")
	require.Equal(t, true, result["success"], result)
	var stored model.User
	require.NoError(t, db.First(&stored, user.Id).Error)
	require.Equal(t, "vip", stored.Group)
	require.Equal(t, "keep@example.com", stored.Email)
	require.Equal(t, "secret-hash", stored.Password)
}
