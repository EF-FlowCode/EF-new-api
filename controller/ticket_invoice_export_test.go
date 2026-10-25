package controller

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"eflow.local/new-api/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestInvoiceExportRejectsUnauthorizedOrMalformedRequests(t *testing.T) {
	for _, test := range []struct {
		role   int
		body   string
		status int
	}{
		{common.RoleCommonUser, `{"ids":[1]}`, http.StatusForbidden},
		{common.RoleCustomerServiceUser, `{"ids":[1]}`, http.StatusForbidden},
		{common.RoleAdminUser, `{"ids":[]}`, http.StatusBadRequest},
		{common.RoleAdminUser, `{"ids":[0]}`, http.StatusBadRequest},
		{common.RoleAdminUser, `{"ids":["1"]}`, http.StatusBadRequest},
		{common.RoleAdminUser, `{"ids":`, http.StatusBadRequest},
		{common.RoleAdminUser, `{"ids":[1],"padding":"` + strings.Repeat("x", 33000) + `"}`, http.StatusBadRequest},
	} {
		response := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(response)
		ctx.Set("role", test.role)
		ctx.Request = httptest.NewRequest(http.MethodPost, "/api/ticket/admin/invoice-export", strings.NewReader(test.body))
		ctx.Request.Header.Set("Content-Type", "application/json")
		ExportTicketInvoices(ctx)
		require.Equal(t, test.status, response.Code)
		require.Equal(t, "no-store", response.Header().Get("Cache-Control"))
		require.Contains(t, response.Header().Get("Content-Type"), "application/json")
		require.Empty(t, response.Header().Get("Content-Disposition"))
	}
}
