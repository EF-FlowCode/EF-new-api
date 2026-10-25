package controller

import (
	"fmt"
	"net/http"
	"testing"

	"eflow.local/new-api/common"
	"eflow.local/new-api/model"
	"github.com/stretchr/testify/require"
)

func TestManageUserStepUpByAction(t *testing.T) {
	db := useStepUpTestDB(t)
	hash, err := common.Password2Hash("root-password-1")
	require.NoError(t, err)
	root := model.User{Username: "manage-root", Password: hash, AffCode: "mgrt01", Role: common.RoleRootUser, Status: common.UserStatusEnabled}
	target := model.User{Username: "manage-target", Password: "hash", AffCode: "mgtg01", Role: common.RoleCommonUser, Status: common.UserStatusEnabled}
	require.NoError(t, db.Create(&root).Error)
	require.NoError(t, db.Create(&target).Error)
	h := newStepUpHarness(t, &root)
	h.engine.POST("/manage", func(c *ginContext) { asStepUpUser(t, c); ManageUser(c) })
	h.engine.POST("/manage-by-token", func(c *ginContext) {
		asStepUpUser(t, c)
		c.Set("use_access_token", true)
		ManageUser(c)
	})

	manage := func(path, body, proof string) map[string]any {
		t.Helper()
		_, result := h.do(http.MethodPost, path, body, proof)
		return result
	}
	proofFor := func(context string) string {
		return h.proof(`{"method":"password","scope":"admin.user.manage","context":` + context + `,"password":"root-password-1"}`)
	}
	status := func() int {
		var stored model.User
		require.NoError(t, db.First(&stored, target.Id).Error)
		return stored.Status
	}
	role := func() int {
		var stored model.User
		require.NoError(t, db.First(&stored, target.Id).Error)
		return stored.Role
	}
	id := target.Id

	// Browser sessions need a proof bound to the user and the action.
	disable := fmt.Sprintf(`{"id":%d,"action":"disable"}`, id)
	require.Equal(t, "SECURITY_PROOF_REQUIRED", manage("/manage", disable, "")["code"])
	enableProof := proofFor(fmt.Sprintf(`{"user_id":%d,"action":"enable"}`, id))
	require.Equal(t, "SECURITY_PROOF_CONTEXT_MISMATCH", manage("/manage", disable, enableProof)["code"])
	require.Equal(t, common.UserStatusEnabled, status())
	require.Equal(t, true, manage("/manage", disable, proofFor(fmt.Sprintf(`{"user_id":%d,"action":"disable"}`, id)))["success"])
	require.Equal(t, common.UserStatusDisabled, status())

	// eflow: automation with an access token (admin-api) enables/disables without a proof.
	require.Equal(t, true, manage("/manage-by-token", fmt.Sprintf(`{"id":%d,"action":"enable"}`, id), "")["success"])
	require.Equal(t, common.UserStatusEnabled, status())
	// ...but role changes still need a proof even with a token.
	setFinance := fmt.Sprintf(`{"id":%d,"action":"set_role","value":7}`, id)
	require.Equal(t, "SECURITY_PROOF_INVALID", manage("/manage-by-token", setFinance, "")["code"], "a legacy token cannot complete step-up")

	// set_role binds the exact target role.
	supportProof := proofFor(fmt.Sprintf(`{"user_id":%d,"action":"set_role","role":5}`, id))
	require.Equal(t, "SECURITY_PROOF_CONTEXT_MISMATCH", manage("/manage", setFinance, supportProof)["code"])
	require.Equal(t, common.RoleCommonUser, role())
	require.Equal(t, true, manage("/manage", setFinance, proofFor(fmt.Sprintf(`{"user_id":%d,"action":"set_role","role":7}`, id)))["success"])
	require.Equal(t, common.RoleFinanceUser, role())

	// Quota adjustments are never gated.
	require.Equal(t, true, manage("/manage", fmt.Sprintf(`{"id":%d,"action":"add_quota","mode":"add","value":100}`, id), "")["success"])

	// Deleting uses its own scope.
	del := fmt.Sprintf(`{"id":%d,"action":"delete"}`, id)
	require.Equal(t, "SECURITY_PROOF_REQUIRED", manage("/manage", del, "")["code"])
	deleteProof := h.proof(fmt.Sprintf(`{"method":"password","scope":"admin.user.delete","context":{"user_id":%d},"password":"root-password-1"}`, id))
	deleted := manage("/manage", del, deleteProof)
	require.Equal(t, true, deleted["success"], deleted)
}

func TestAdminUserScopesNeedAdministrators(t *testing.T) {
	db := useStepUpTestDB(t)
	support := model.User{Username: "scope-support", Password: "hash", AffCode: "scsp01", Role: common.RoleCustomerServiceUser, Status: common.UserStatusEnabled}
	require.NoError(t, db.Create(&support).Error)
	h := newStepUpHarness(t, &support)
	_, result := h.do(http.MethodGet, "/verify/methods?scope=admin.user.manage", "", "")
	require.Equal(t, "SECURITY_ACTION_FORBIDDEN", result["code"])
}
