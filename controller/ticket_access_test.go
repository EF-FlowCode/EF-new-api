package controller

import (
	"net/http/httptest"
	"testing"

	"eflow.local/new-api/common"
	"eflow.local/new-api/model"
	"eflow.local/new-api/setting"
	"github.com/gin-gonic/gin"
)

func TestStaffAccessToUnassignedTicketsFollowsAssignedTypes(t *testing.T) {
	prev := setting.TicketAssignConfig2JSONString()
	t.Cleanup(func() { _ = setting.UpdateTicketAssignConfigFromJSON(prev) })
	if err := setting.UpdateTicketAssignConfigFromJSON(`{"enabled":true,"rules":{"refund":{"strategy":"round_robin","users":[42]}}}`); err != nil {
		t.Fatal(err)
	}

	newCtx := func(userID, role int) *gin.Context {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Set("id", userID)
		c.Set("role", role)
		return c
	}
	staff := newCtx(42, common.RoleCustomerServiceUser)
	outsider := newCtx(43, common.RoleCustomerServiceUser)

	cases := []struct {
		name   string
		ctx    *gin.Context
		ticket model.Ticket
		want   bool
	}{
		{"own-type-unassigned", staff, model.Ticket{Type: model.TicketTypeRefund}, true},
		{"other-type-unassigned", staff, model.Ticket{Type: model.TicketTypeInvoice}, false},
		{"staff-without-rule", outsider, model.Ticket{Type: model.TicketTypeRefund}, false},
		{"assigned-to-self", outsider, model.Ticket{Type: model.TicketTypeInvoice, AssigneeId: 43}, true},
		{"assigned-to-other", staff, model.Ticket{Type: model.TicketTypeRefund, AssigneeId: 7}, false},
	}
	for _, c := range cases {
		if got := ticketAccessibleByActor(c.ctx, &c.ticket); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}
