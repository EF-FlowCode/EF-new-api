package router

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"eflow.local/new-api/common"
	"eflow.local/new-api/i18n"
	"eflow.local/new-api/internal/testdb"
	"eflow.local/new-api/model"
	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestInvoiceExportRequiresAdmin(t *testing.T) {
	require.NoError(t, i18n.Init())
	db := testdb.OpenMySQL8(t, &model.User{}, &model.Ticket{}, &model.TicketInvoice{}, &model.TicketMessage{}, &model.TopUp{})
	previous, redis := model.DB, common.RedisEnabled
	model.DB, common.RedisEnabled = db, false
	t.Cleanup(func() { model.DB, common.RedisEnabled = previous, redis })
	require.NoError(t, db.Create(&model.Ticket{Id: 1, UserId: 7, Type: model.TicketTypeInvoice, Subject: "invoice"}).Error)
	require.NoError(t, db.Create(&model.TicketInvoice{TicketId: 1, UserId: 7, CompanyName: "公司", Email: "a@example.test", TopUpOrderIds: "[1]", TotalMoney: 50}).Error)
	require.NoError(t, db.Create(&model.TicketMessage{TicketId: 1, UserId: 7, Content: "申请备注"}).Error)
	require.NoError(t, db.Create(&model.TopUp{Id: 1, UserId: 7, TradeNo: "invoice-order"}).Error)
	r := gin.New()
	r.Use(sessions.Sessions("session", cookie.NewStore([]byte("invoice-export-test-cookie"))))
	r.GET("/test-login/:id", func(c *gin.Context) {
		id, _ := strconv.Atoi(c.Param("id"))
		session := sessions.Default(c)
		session.Set("id", id)
		require.NoError(t, session.Save())
	})
	SetApiRouter(r)
	for _, role := range []int{common.RoleCommonUser, common.RoleCustomerServiceUser, common.RoleAdminUser, common.RoleRootUser} {
		user := model.User{Username: "export-" + strconv.Itoa(role), Role: role, Status: common.UserStatusEnabled, AffCode: "invoice-" + strconv.Itoa(role)}
		require.NoError(t, db.Create(&user).Error)
		login := httptest.NewRecorder()
		r.ServeHTTP(login, httptest.NewRequest(http.MethodGet, "/test-login/"+strconv.Itoa(user.Id), nil))
		request := httptest.NewRequest(http.MethodPost, "/api/ticket/admin/invoice-export", bytes.NewBufferString(`{"ids":[1]}`))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("New-Api-User", strconv.Itoa(user.Id))
		request.AddCookie(login.Result().Cookies()[0])
		response := httptest.NewRecorder()
		r.ServeHTTP(response, request)
		if role >= common.RoleAdminUser {
			require.Equal(t, http.StatusOK, response.Code, response.Body.String())
			require.Equal(t, "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", response.Header().Get("Content-Type"))
			require.Equal(t, "no-store", response.Header().Get("Cache-Control"))
			require.True(t, bytes.HasPrefix(response.Body.Bytes(), []byte("PK")))
		} else {
			var result struct {
				Success bool `json:"success"`
			}
			require.NoError(t, json.Unmarshal(response.Body.Bytes(), &result))
			require.False(t, result.Success)
			require.NotContains(t, response.Body.String(), "invoice-order")
		}
	}
}
