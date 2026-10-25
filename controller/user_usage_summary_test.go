package controller

import (
	"net/http"
	"net/http/httptest"
	"strconv"
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

func TestSelfUsageSummaryAuthorization(t *testing.T) {
	// Legacy system access tokens stay usable during the 30-day transition.
	t.Cleanup(model.SetLegacyAccessTokenRetireAtForTest(common.GetTimestamp() + 3600))
	db := testdb.OpenMySQL8(t, &model.User{}, &model.UsageRefundReceipt{}, &model.UsageRefundBatch{}, &model.TicketRefund{}, &model.Log{})
	oldDB, oldLogDB, oldRedis := model.DB, model.LOG_DB, common.RedisEnabled
	model.DB, model.LOG_DB, common.RedisEnabled = db, db, false
	t.Cleanup(func() { model.DB, model.LOG_DB, common.RedisEnabled = oldDB, oldLogDB, oldRedis })
	u := model.User{Username: "summary-user", AffCode: "summary", Role: common.RoleCommonUser, Status: common.UserStatusEnabled, Quota: 100, UsedQuota: 300}
	u.SetAccessToken("summary-test-key")
	require.NoError(t, db.Create(&u).Error)
	require.NoError(t, db.Create(&model.UsageRefundReceipt{SourceLogID: 1, UserID: u.Id, Quota: 20}).Error)
	require.NoError(t, db.Create(&model.UsageRefundReceipt{SourceLogID: 2, UserID: u.Id + 1, Quota: 9999}).Error)
	r := gin.New()
	r.Use(sessions.Sessions("test", cookie.NewStore([]byte("summary-test-session-key"))))
	g := r.Group("/api/user/self", middleware.UserAuth())
	g.GET("/usage-summary", GetSelfUsageSummary)
	g.GET("/refund-records", GetSelfRefundRecords)
	for _, path := range []string{"/usage-summary?user_id=999", "/refund-records?kind=ticket&user_id=999"} {
		request := func(auth bool) *httptest.ResponseRecorder {
			req := httptest.NewRequest(http.MethodGet, "/api/user/self"+path, nil)
			if auth {
				req.Header.Set("Authorization", "Bearer summary-test-key")
				req.Header.Set("New-Api-User", strconv.Itoa(u.Id))
			}
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			return w
		}
		denied := request(false)
		require.NotContains(t, denied.Body.String(), `"success":true`)
		allowed := request(true)
		require.Contains(t, allowed.Body.String(), `"success":true`)
		require.NotContains(t, allowed.Body.String(), "9999")
		if path[1] == 'u' {
			require.Contains(t, allowed.Body.String(), `"net_used_quota":280`)
		}
	}
}
