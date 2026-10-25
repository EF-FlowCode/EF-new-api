package controller

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"

	"eflow.local/new-api/common"
	"eflow.local/new-api/i18n"
	"eflow.local/new-api/internal/testdb"
	"eflow.local/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestCreateUserRoleWhitelist(t *testing.T) {
	require.NoError(t, i18n.Init())
	db := testdb.OpenMySQL8(t, &model.User{}, &model.Log{})
	oldDB, oldLog, oldRedis := model.DB, model.LOG_DB, common.RedisEnabled
	model.DB, model.LOG_DB, common.RedisEnabled = db, db, false
	t.Cleanup(func() { model.DB, model.LOG_DB, common.RedisEnabled = oldDB, oldLog, oldRedis })
	for _, role := range []int{-1, 2, 6, 9, 99, 101, 1, 5, 7, 10} {
		t.Run(fmt.Sprint(role), func(t *testing.T) {
			name := fmt.Sprintf("role-test-%d", role)
			r := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(r)
			c.Set("role", common.RoleRootUser)
			c.Request = httptest.NewRequest("POST", "/api/user/", strings.NewReader(fmt.Sprintf(`{"username":%q,"password":"test-password-123","role":%d,"group":"default"}`, name, role)))
			CreateUser(c)
			var response struct {
				Success bool   `json:"success"`
				Code    string `json:"code"`
			}
			require.NoError(t, json.Unmarshal(r.Body.Bytes(), &response))
			valid := common.IsValidateRole(role)
			// eflow: support (5) and finance (7) are created without step-up;
			// administrators need a proof bound to the role.
			needsProof := valid && role >= common.RoleAdminUser
			require.Equal(t, valid && !needsProof, response.Success, r.Body.String())
			if !valid {
				require.False(t, strings.HasPrefix(response.Code, "SECURITY_PROOF_"), "invalid roles are rejected before any proof is consumed")
			}
			if needsProof {
				require.True(t, strings.HasPrefix(response.Code, "SECURITY_PROOF_"), r.Body.String())
			}
			var count int64
			require.NoError(t, db.Model(&model.User{}).Where("username = ?", name).Count(&count).Error)
			if valid && !needsProof {
				require.EqualValues(t, 1, count)
			} else {
				require.Zero(t, count)
			}
		})
	}
}
