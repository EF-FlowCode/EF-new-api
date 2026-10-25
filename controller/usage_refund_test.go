package controller

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"eflow.local/new-api/common"
	"eflow.local/new-api/internal/testdb"
	"eflow.local/new-api/middleware"
	"eflow.local/new-api/model"
	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestUsageRefundFailureMessages(t *testing.T) {
	for _, test := range []struct {
		name, fallback, message string
		err                     error
		status                  int
	}{
		{"preview timeout", "Refund preview failed; no balance was changed", "Refund scan timed out; narrow the time range or import request IDs. No balance was changed", fmt.Errorf("scan: %w", context.DeadlineExceeded), http.StatusGatewayTimeout},
		{"preview database failure", "Refund preview failed; no balance was changed", "Refund preview failed; no balance was changed", errors.New("private database error"), http.StatusInternalServerError},
		{"execute timeout", "refund operation failed; query the batch before retrying", "refund operation failed; query the batch before retrying", context.DeadlineExceeded, http.StatusInternalServerError},
		{"list failure", "Failed to load batches", "Failed to load batches", errors.New("private database error"), http.StatusInternalServerError},
		{"preview limit", "Refund preview failed; no balance was changed", model.ErrUsageRefundLimit.Error(), model.ErrUsageRefundLimit, http.StatusBadRequest},
	} {
		t.Run(test.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			usageRefundResponse(c, nil, test.err, test.fallback)
			require.Equal(t, test.status, w.Code)
			var result struct {
				Success bool   `json:"success"`
				Message string `json:"message"`
			}
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
			require.False(t, result.Success)
			require.Equal(t, test.message, result.Message)
		})
	}
}

func TestUsageRefundRootAPI(t *testing.T) {
	// Legacy system access tokens stay usable during the 30-day transition.
	t.Cleanup(model.SetLegacyAccessTokenRetireAtForTest(common.GetTimestamp() + 3600))
	db := testdb.OpenMySQL8(t, &model.User{}, &model.Token{}, &model.Log{}, &model.UsageRefundBatch{}, &model.UsageRefundReceipt{}, &model.UsageRefundNotice{}, &model.UsageRefundLogDelivery{})
	oldDB, oldLogDB, oldRedis := model.DB, model.LOG_DB, common.RedisEnabled
	model.DB, model.LOG_DB, common.RedisEnabled = db, db, false
	t.Cleanup(func() { model.DB, model.LOG_DB, common.RedisEnabled = oldDB, oldLogDB, oldRedis })
	admin := model.User{Username: "refund-admin", Role: common.RoleRootUser, Status: common.UserStatusEnabled}
	admin.SetAccessToken("refund-admin-access")
	admin.AffCode = "refund-admin"
	user := model.User{Username: "refund-customer", Role: common.RoleCommonUser, Status: common.UserStatusEnabled, Quota: 100}
	user.SetAccessToken("refund-user-access")
	user.AffCode = "refund-user"
	require.NoError(t, db.Create(&admin).Error)
	require.NoError(t, db.Create(&user).Error)
	log := model.Log{UserId: user.Id, Username: user.Username, Type: model.LogTypeConsume, Quota: 30, RequestId: "api-test", CreatedAt: 100}
	require.NoError(t, db.Create(&log).Error)
	r := gin.New()
	r.Use(sessions.Sessions("test", cookie.NewStore([]byte("refund-test-session-key"))))
	g := r.Group("/api/log/refunds", middleware.RootAuth())
	g.POST("/preview", PreviewUsageRefund)
	g.POST("/candidates", ListUsageRefundCandidates)
	g.POST("/:id/execute", ExecuteUsageRefund)
	g.GET("/:id", GetUsageRefund)
	g.GET("/", ListUsageRefunds)
	request := func(method, path, body string, actor model.User, key string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+key)
		req.Header.Set("New-Api-User", strconv.Itoa(actor.Id))
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}
	assertDenied := func(response *httptest.ResponseRecorder, status int, code string) {
		t.Helper()
		require.Equal(t, status, response.Code, response.Body.String())
		var rejection struct {
			Success bool   `json:"success"`
			Code    string `json:"code"`
		}
		require.NoError(t, json.Unmarshal(response.Body.Bytes(), &rejection))
		require.False(t, rejection.Success)
		require.Equal(t, code, rejection.Code)
		require.Equal(t, "no-store", response.Header().Get("Cache-Control"))
	}
	selection := `{"request_ids":["api-test"],"reason":"service interruption"}`
	w := request(http.MethodPost, "/api/log/refunds/preview", selection, user, "refund-user-access")
	assertDenied(w, http.StatusForbidden, "permission_denied")
	var unauthorizedCount int64
	require.NoError(t, db.Model(&model.UsageRefundBatch{}).Count(&unauthorizedCount).Error)
	require.Zero(t, unauthorizedCount)
	w = request(http.MethodPost, "/api/log/refunds/preview", selection+` {}`, admin, "refund-admin-access")
	require.Equal(t, http.StatusBadRequest, w.Code)
	w = request(http.MethodPost, "/api/log/refunds/preview", selection, admin, "refund-admin-access")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var result struct {
		Success bool                  `json:"success"`
		Data    model.UsageRefundView `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	require.True(t, result.Success)
	id := result.Data.ID
	for _, role := range []int{common.RoleCommonUser, common.RoleCustomerServiceUser, common.RoleAdminUser} {
		actor := model.User{Username: "denied-" + strconv.Itoa(role), Role: role, Status: common.UserStatusEnabled, AffCode: "denied-" + strconv.Itoa(role)}
		key := "refund-denied-" + strconv.Itoa(role)
		actor.SetAccessToken(key)
		require.NoError(t, db.Create(&actor).Error)
		for _, endpoint := range []struct{ method, path, body string }{
			{http.MethodPost, "/preview", selection},
			{http.MethodPost, "/candidates", `{"user_id":` + strconv.Itoa(user.Id) + `,"start_timestamp":90,"end_timestamp":110}`},
			{http.MethodGet, "/", ""},
			{http.MethodGet, "/" + id, ""},
			{http.MethodPost, "/" + id + "/execute", `{"expected_quota":30,"expected_count":1}`},
		} {
			denied := request(endpoint.method, "/api/log/refunds"+endpoint.path, endpoint.body, actor, key)
			assertDenied(denied, http.StatusForbidden, "permission_denied")
		}
		spoofed := request(http.MethodPost, "/api/log/refunds/"+id+"/execute", `{"expected_quota":30,"expected_count":1}`, admin, key)
		assertDenied(spoofed, http.StatusUnauthorized, "user_id_mismatch")
	}
	require.NoError(t, db.Model(&model.UsageRefundBatch{}).Count(&unauthorizedCount).Error)
	require.EqualValues(t, 1, unauthorizedCount, "denied requests must not create additional refund batches")
	var deniedReceipts int64
	require.NoError(t, db.Model(&model.UsageRefundReceipt{}).Count(&deniedReceipts).Error)
	require.Zero(t, deniedReceipts)
	var deniedRefundLogs int64
	require.NoError(t, db.Model(&model.Log{}).Where("type = ?", model.LogTypeRefund).Count(&deniedRefundLogs).Error)
	require.Zero(t, deniedRefundLogs)
	require.NoError(t, db.First(&user, user.Id).Error)
	require.Equal(t, 100, user.Quota)
	w = request(http.MethodPost, "/api/log/refunds/"+id+"/execute", `{"expected_quota":31,"expected_count":1}`, admin, "refund-admin-access")
	require.Equal(t, http.StatusBadRequest, w.Code)
	w = request(http.MethodPost, "/api/log/refunds/"+id+"/execute", `{"expected_quota":30,"expected_count":1}`, admin, "refund-admin-access")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	require.Equal(t, "executed", result.Data.Status)
	require.Zero(t, result.Data.LogsPending)
	var notice model.Log
	require.NoError(t, db.Where("type = ? AND user_id = ?", model.LogTypeRefund, user.Id).First(&notice).Error)
	require.Equal(t, 30, notice.Quota)
	require.Contains(t, notice.Content, "service interruption")

	partialLog := model.Log{UserId: user.Id, Username: user.Username, Type: model.LogTypeConsume, Quota: 40, RequestId: "api-partial", CreatedAt: 105}
	require.NoError(t, db.Create(&partialLog).Error)
	w = request(http.MethodPost, "/api/log/refunds/candidates", `{"username":"refund-customer","start_timestamp":90,"end_timestamp":110}`, admin, "refund-admin-access")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var candidates struct {
		Data model.UsageRefundCandidates `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &candidates))
	require.Len(t, candidates.Data.Items, 2)
	partial := `{"log_ids":[` + strconv.Itoa(partialLog.Id) + `],"amounts":{"` + strconv.Itoa(partialLog.Id) + `":15},"reason":"manual"}`
	w = request(http.MethodPost, "/api/log/refunds/preview", partial, admin, "refund-admin-access")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	require.Equal(t, 15, result.Data.Quota)
	require.Equal(t, 40, result.Data.Items[0].OriginalQuota)
	tooMuch := `{"log_ids":[` + strconv.Itoa(partialLog.Id) + `],"amounts":{"` + strconv.Itoa(partialLog.Id) + `":41},"reason":"manual"}`
	w = request(http.MethodPost, "/api/log/refunds/preview", tooMuch, admin, "refund-admin-access")
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
}
