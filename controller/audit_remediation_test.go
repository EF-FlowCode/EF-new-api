package controller

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"eflow.local/new-api/common"
	"eflow.local/new-api/model"
	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/go-playground/validator/v10"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/require"
)

func TestAuditVerificationDependencyErrorsStopBeforeDatabase(t *testing.T) {
	previousRedis, enabled, validation, database := common.RDB, common.RedisEnabled, common.Validate, model.DB
	common.RDB, common.RedisEnabled, common.Validate, model.DB = nil, true, validator.New(), nil
	auth := common.GetAuthSettings()
	require.NoError(t, common.UpdateAuthSettings(map[string]string{"RegisterEnabled": "true", "PasswordRegisterEnabled": "true", "EmailVerificationEnabled": "true"}))
	t.Cleanup(func() {
		common.RDB, common.RedisEnabled, common.Validate, model.DB = previousRedis, enabled, validation, database
		require.NoError(t, common.UpdateAuthSettings(map[string]string{
			"RegisterEnabled":          strconv.FormatBool(auth.RegistrationEnabled),
			"PasswordRegisterEnabled":  strconv.FormatBool(auth.PasswordRegisterEnabled),
			"EmailVerificationEnabled": strconv.FormatBool(auth.EmailVerificationEnabled),
		}))
	})
	for _, tc := range []struct {
		name, method, path, body string
		handler                  gin.HandlerFunc
	}{
		{"register", "POST", "/api/user/register", `{"username":"audit-user","password":"password123","email":"audit@example.com","verification_code":"code"}`, Register},
		{"email-bind", "POST", "/api/oauth/email/bind", `{"email":"audit@example.com","code":"code"}`, EmailBind},
		{"reset", "POST", "/api/user/reset", `{"email":"audit@example.com","token":"token"}`, ResetPassword},
		{"send-reset", "GET", "/api/reset_password?email=audit@example.com", "", SendPasswordResetEmail},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			tc.handler(c)
			require.Equal(t, http.StatusServiceUnavailable, w.Code)
			var response struct {
				Success bool
				Message string
			}
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
			require.False(t, response.Success)
			require.Equal(t, common.ErrVerificationStoreUnavailable.Error(), response.Message)
		})
	}
}

func TestAuditMonitoringRefreshAPIReportsAcceptedOrUnavailable(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr(), MaxRetries: -1})
	previous, enabled, master := common.RDB, common.RedisEnabled, common.IsMasterNode
	common.RDB, common.RedisEnabled, common.IsMasterNode = client, true, false
	t.Cleanup(func() {
		_ = client.Close()
		common.RDB, common.RedisEnabled, common.IsMasterNode = previous, enabled, master
	})
	for _, unavailable := range []bool{false, true} {
		if unavailable {
			server.SetError("ERR private redis diagnostics")
		}
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("POST", "/api/monitoring/admin/refresh", nil)
		RefreshMonitoringData(c)
		if unavailable {
			require.Equal(t, http.StatusServiceUnavailable, w.Code)
			require.NotContains(t, w.Body.String(), "private redis diagnostics")
		} else {
			require.Equal(t, http.StatusAccepted, w.Code)
			require.NotEmpty(t, server.Keys())
		}
	}
}
