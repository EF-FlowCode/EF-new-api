package model

import (
	"errors"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestConcurrentRefundTicketsNeverOverdrawQuota(t *testing.T) {
	db := useModelIntegrationTestDB(t, &User{}, &Ticket{}, &TicketRefund{}, &TicketMessage{}, &Log{})
	user := User{Username: "refund-race", Password: "x", Quota: 1000, Group: "default"}
	require.NoError(t, db.Create(&user).Error)

	const attempts = 5
	var wg sync.WaitGroup
	errs := make([]error, attempts)
	for i := range attempts {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, _, _, errs[i] = CreateRefundTicket(CreateRefundTicketParams{
				UserId: user.Id, Username: user.Username, RefundQuota: 1000,
				PayeeType: RefundPayeeTypeAlipay, PayeeName: "n", PayeeAccount: "a", Contact: "c",
			})
		}(i)
	}
	wg.Wait()

	succeeded := 0
	for _, err := range errs {
		if err == nil {
			succeeded++
		} else {
			require.ErrorIs(t, err, ErrTicketRefundQuotaExceed)
		}
	}
	require.Equal(t, 1, succeeded)
	var reloaded User
	require.NoError(t, db.First(&reloaded, user.Id).Error)
	require.Equal(t, 0, reloaded.Quota)
	var refunds int64
	require.NoError(t, db.Model(&TicketRefund{}).Where("user_id = ?", user.Id).Count(&refunds).Error)
	require.EqualValues(t, 1, refunds)
}

func TestInvoiceStatusOnlyLeavesPending(t *testing.T) {
	db := useModelIntegrationTestDB(t, &Ticket{}, &TicketInvoice{}, &TicketAttachment{})
	ticket := Ticket{UserId: 7, Subject: "invoice", Type: TicketTypeInvoice, Status: TicketStatusOpen}
	require.NoError(t, db.Create(&ticket).Error)
	invoice := TicketInvoice{TicketId: ticket.Id, UserId: 7, CompanyName: "c", TaxNumber: "t", Email: "e@x.com", TopUpOrderIds: "[1]", InvoiceStatus: InvoiceStatusPending}
	require.NoError(t, db.Create(&invoice).Error)

	_, _, _, err := UpdateInvoiceStatus(ticket.Id, 1, InvoiceStatusRejected)
	require.NoError(t, err)

	for _, next := range []int{InvoiceStatusIssued, InvoiceStatusPending, InvoiceStatusRejected} {
		_, _, _, err = UpdateInvoiceStatus(ticket.Id, 1, next)
		require.ErrorIs(t, err, ErrTicketInvoiceStatusTransition, "rejected -> %d", next)
		require.ErrorIs(t, err, ErrTicketInvoiceStatusInvalid)
	}
}

func TestBindAttachmentsFailsWhenAttachmentWasRemovedConcurrently(t *testing.T) {
	truncateAttachments(t)
	a := seedAttachment(t, 101)
	deleted, err := DeleteUnboundAttachment(a.Id)
	require.NoError(t, err)
	require.True(t, deleted)

	// 已撤回（软删）的附件与已绑定的附件都不能再被绑定。
	b := seedAttachment(t, 101)
	c := seedAttachment(t, 101)
	require.NoError(t, DB.Model(&TicketAttachment{}).Where("id = ?", c.Id).Update("message_id", 9).Error)
	err = DB.Transaction(func(tx *gorm.DB) error {
		return BindAttachmentsToMessage(tx, []int{b.Id, c.Id}, 1, 2, 101)
	})
	require.True(t, errors.Is(err, ErrAttachmentBound), "got %v", err)
}

func TestDeleteUnboundAttachmentKeepsBoundAttachment(t *testing.T) {
	truncateAttachments(t)
	a := seedAttachment(t, 101)
	require.NoError(t, DB.Model(&TicketAttachment{}).Where("id = ?", a.Id).Update("message_id", 3).Error)

	deleted, err := DeleteUnboundAttachment(a.Id)
	require.NoError(t, err)
	require.False(t, deleted)
	var reloaded TicketAttachment
	require.NoError(t, DB.First(&reloaded, a.Id).Error)
	require.Equal(t, 3, reloaded.MessageId)
}
