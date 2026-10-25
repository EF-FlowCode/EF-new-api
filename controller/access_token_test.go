package controller

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"eflow.local/new-api/common"
	"eflow.local/new-api/model"
	"github.com/stretchr/testify/require"
)

func TestAccessTokenLifecycleRequiresBoundProofs(t *testing.T) {
	db := useStepUpTestDB(t)
	t.Cleanup(model.SetLegacyAccessTokenRetireAtForTest(common.GetTimestamp() + 3600))
	hash, err := common.Password2Hash("owner-password-1")
	require.NoError(t, err)
	owner := model.User{Username: "pat-life", Password: hash, AffCode: "patlf1", Role: common.RoleCustomerServiceUser, Status: common.UserStatusEnabled}
	require.NoError(t, db.Create(&owner).Error)
	h := newStepUpHarness(t, &owner)
	h.engine.GET("/tokens", func(c *ginContext) { asStepUpUser(t, c); ListAccessTokens(c) })
	h.engine.GET("/tokens/catalog", func(c *ginContext) { asStepUpUser(t, c); GetAccessTokenCatalog(c) })
	h.engine.POST("/tokens", func(c *ginContext) { asStepUpUser(t, c); CreateAccessToken(c) })
	h.engine.PATCH("/tokens/:id", func(c *ginContext) { asStepUpUser(t, c); UpdateAccessToken(c) })
	h.engine.DELETE("/tokens/legacy", func(c *ginContext) { asStepUpUser(t, c); RevokeLegacyAccessToken(c) })
	h.engine.DELETE("/tokens/:id", func(c *ginContext) { asStepUpUser(t, c); DeleteAccessToken(c) })
	proof := func(scope, context string) string {
		return h.proof(`{"method":"password","scope":"` + scope + `","context":` + context + `,"password":"owner-password-1"}`)
	}

	_, result := h.do(http.MethodGet, "/tokens/catalog", "", "")
	require.Contains(t, fmt.Sprint(result["data"]), "ticket_admin", "support staff can grant ticket management")

	create := `{"name":"automation","scopes":["user:read","profile:read"],"expires_at":0}`
	_, result = h.do(http.MethodPost, "/tokens", create, "")
	require.Equal(t, "SECURITY_PROOF_REQUIRED", result["code"])
	_, result = h.do(http.MethodPost, "/tokens", `{"name":"x","scopes":["user:write"],"expires_at":0}`, "")
	require.Equal(t, "ACCESS_TOKEN_SCOPE_FORBIDDEN", result["code"], "support staff cannot grant user:write, checked before any proof")
	_, result = h.do(http.MethodPost, "/tokens", `{"name":"x","scopes":["profile:read"],"expires_at":5}`, "")
	require.Equal(t, "ACCESS_TOKEN_EXPIRY_INVALID", result["code"])

	// The proof binds the exact grant: another scope set is rejected.
	wrong := proof("access_token.generate", `{"scopes":["profile:read"],"expires_at":0}`)
	_, result = h.do(http.MethodPost, "/tokens", create, wrong)
	require.Equal(t, "SECURITY_PROOF_CONTEXT_MISMATCH", result["code"])
	_, result = h.do(http.MethodPost, "/tokens", create, proof("access_token.generate", `{"scopes":["profile:read","user:read"],"expires_at":0}`))
	require.Equal(t, true, result["success"], result)
	data := result["data"].(map[string]any)
	raw := data["token"].(string)
	require.True(t, strings.HasPrefix(raw, model.AccessTokenPrefix))
	require.Len(t, raw, len(model.AccessTokenPrefix)+43)
	item := data["item"].(map[string]any)
	id := int(item["id"].(float64))
	require.Equal(t, model.AccessTokenFingerprint(raw), item["token_ref"])
	require.NotContains(t, fmt.Sprint(item), raw, "the plaintext is only returned once, outside the item")

	// Renaming needs no proof; changing the grant does.
	_, result = h.do(http.MethodPatch, fmt.Sprintf("/tokens/%d", id), `{"name":"renamed"}`, "")
	require.Equal(t, true, result["success"], result)
	_, result = h.do(http.MethodPatch, fmt.Sprintf("/tokens/%d", id), `{"name":"renamed","scopes":["profile:read"]}`, "")
	require.Equal(t, "SECURITY_PROOF_REQUIRED", result["code"])
	_, result = h.do(http.MethodPatch, fmt.Sprintf("/tokens/%d", id), `{"name":"renamed","scopes":["profile:read"]}`,
		proof("access_token.update", fmt.Sprintf(`{"token_id":%d,"scopes":["profile:read"]}`, id)))
	require.Equal(t, true, result["success"], result)
	stored, err := model.GetUserAccessToken(owner.Id, id)
	require.NoError(t, err)
	require.Equal(t, []string{"profile:read"}, stored.GetScopes())

	// Legacy token: listed during the transition and revocable, never regenerated.
	require.NoError(t, model.SetLegacyUserAccessToken(owner.Id, "legacylifecycletoken000000000000"))
	_, result = h.do(http.MethodGet, "/tokens", "", "")
	listed := result["data"].(map[string]any)
	require.Len(t, listed["items"], 1)
	require.NotNil(t, listed["legacy"])
	_, result = h.do(http.MethodDelete, "/tokens/legacy", "", proof("access_token.revoke", `{"legacy":true}`))
	require.Equal(t, true, result["success"], result)

	// Revoking checks ownership first, then needs a proof for that token.
	_, result = h.do(http.MethodDelete, "/tokens/999999", "", "")
	require.Equal(t, "ACCESS_TOKEN_NOT_FOUND", result["code"])
	_, result = h.do(http.MethodDelete, fmt.Sprintf("/tokens/%d", id), "", proof("access_token.revoke", `{"token_id":999999}`))
	require.Equal(t, "SECURITY_PROOF_CONTEXT_MISMATCH", result["code"])
	_, result = h.do(http.MethodDelete, fmt.Sprintf("/tokens/%d", id), "", proof("access_token.revoke", fmt.Sprintf(`{"token_id":%d}`, id)))
	require.Equal(t, true, result["success"], result)
	_, err = model.GetUserAccessToken(owner.Id, id)
	require.ErrorIs(t, err, model.ErrAccessTokenNotFound)

	var actions []string
	require.NoError(t, db.Model(&model.AuditLog{}).Where("category = ?", model.AuditCategorySecurity).Pluck("action", &actions).Error)
	for _, action := range []string{"access_token.generate", "access_token.rename", "access_token.update", "access_token.revoke"} {
		require.Contains(t, actions, action)
	}
}
