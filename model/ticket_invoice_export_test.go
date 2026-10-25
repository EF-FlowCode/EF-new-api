package model

import (
	"context"
	"strconv"
	"testing"

	"eflow.local/new-api/common"
	"github.com/stretchr/testify/require"
)

func TestInvoiceExportSnapshotAndTitleSearch(t *testing.T) {
	db := useModelIntegrationTestDB(t, &Ticket{}, &TicketInvoice{}, &TicketMessage{}, &TopUp{})
	for i, title := range []string{"示例大学", "示例大学附属医院", "100%_研究所"} {
		id := i + 1
		require.NoError(t, db.Create(&Ticket{Id: id, UserId: id, Username: "applicant", Subject: "发票申请", Type: TicketTypeInvoice, Status: TicketStatusClosed}).Error)
		require.NoError(t, db.Create(&TicketInvoice{Id: id + 100, TicketId: id, UserId: id, CompanyName: title, TaxNumber: "001234567890123456", BankAccount: "001234567890123456789", Email: "invoice@example.test", TopUpOrderIds: "[" + strconv.Itoa(id) + "]", TotalMoney: 123.45, InvoiceStatus: InvoiceStatusIssued}).Error)
		require.NoError(t, db.Create(&TicketMessage{TicketId: id, UserId: id, Content: "原始申请\n备注：请拆为两张"}).Error)
		require.NoError(t, db.Create(&TicketMessage{TicketId: id, UserId: 10, Content: "客服后续回复"}).Error)
		require.NoError(t, db.Create(&TopUp{Id: id, UserId: id, TradeNo: "order-" + strconv.Itoa(id)}).Error)
	}
	page := &common.PageInfo{Page: 1, PageSize: 1}
	tickets, total, err := ListTickets(TicketQueryOptions{CompanyName: " 示例大学 ", OrderBy: "id"}, page)
	require.NoError(t, err)
	require.EqualValues(t, 2, total)
	require.Len(t, tickets, 1)
	require.Equal(t, 1, tickets[0].Id, "invoice row ID must not replace ticket ID")
	page.Page = 2
	tickets, _, err = ListTickets(TicketQueryOptions{CompanyName: "示例大学", OrderBy: "id"}, page)
	require.NoError(t, err)
	require.Equal(t, 2, tickets[0].Id)
	_, total, err = ListTickets(TicketQueryOptions{CompanyName: "%_"}, page)
	require.NoError(t, err)
	require.EqualValues(t, 1, total, "wildcards in a company name are literal text")
	_, total, err = ListTickets(TicketQueryOptions{CompanyName: "大学", TypeIn: []string{TicketTypeRefund}, AssigneeId: -1}, page)
	require.NoError(t, err)
	require.Zero(t, total, "title search cannot bypass the staff type scope")
	_, total, err = ListTickets(TicketQueryOptions{CompanyName: "大学", UserId: 2}, page)
	require.NoError(t, err)
	require.EqualValues(t, 1, total)

	var before []TicketInvoice
	require.NoError(t, db.Order("id").Find(&before).Error)
	records, err := ReadInvoiceExport(context.Background(), []int{2, 1, 2})
	require.NoError(t, err)
	require.Len(t, records, 2)
	require.Equal(t, 2, records[0].Ticket.Id)
	require.Equal(t, []string{"order-2"}, records[0].OrderNumbers)
	require.Equal(t, "原始申请\n备注：请拆为两张", records[0].Application)
	require.Equal(t, "001234567890123456789", records[1].Invoice.BankAccount)
	var after []TicketInvoice
	require.NoError(t, db.Order("id").Find(&after).Error)
	require.Equal(t, before, after, "export must not mark invoices as issued or alter data")
	_, err = ReadInvoiceExport(context.Background(), []int{1, 999})
	require.ErrorIs(t, err, ErrInvoiceExportIncomplete)
	require.NoError(t, db.Model(&TopUp{}).Where("id = 1").Update("user_id", 99).Error)
	_, err = ReadInvoiceExport(context.Background(), []int{1})
	require.ErrorIs(t, err, ErrInvoiceExportIncomplete, "never export another user's order")
	require.NoError(t, db.Delete(&Ticket{}, 2).Error)
	_, err = ReadInvoiceExport(context.Background(), []int{2})
	require.ErrorIs(t, err, ErrInvoiceExportIncomplete, "deleted tickets must not be exported")
}

func TestInvoiceExportRejectsInvalidSelectionBeforeReading(t *testing.T) {
	for _, ids := range [][]int{nil, {}, {0}, {-1}, make([]int, MaxInvoiceExportTickets+1)} {
		_, err := ReadInvoiceExport(context.Background(), ids)
		require.ErrorIs(t, err, ErrInvoiceExportSelection)
	}
}
