package model

import (
	"context"
	"database/sql"
	"errors"

	"gorm.io/gorm"
)

const MaxInvoiceExportTickets = 500

var (
	ErrInvoiceExportSelection  = errors.New("Select between 1 and 500 invoice tickets")
	ErrInvoiceExportIncomplete = errors.New("Selected invoices are missing or incomplete; refresh the list and try again")
)

type InvoiceExportRecord struct {
	Ticket       Ticket
	Invoice      TicketInvoice
	OrderNumbers []string
	Application  string
}

// ReadInvoiceExport takes one consistent, read-only snapshot. It never marks an
// invoice as issued; all selected applications must be present and complete.
func ReadInvoiceExport(ctx context.Context, ids []int) ([]InvoiceExportRecord, error) {
	if len(ids) == 0 || len(ids) > MaxInvoiceExportTickets {
		return nil, ErrInvoiceExportSelection
	}
	unique := make([]int, 0, len(ids))
	seen := make(map[int]bool, len(ids))
	for _, id := range ids {
		if id <= 0 {
			return nil, ErrInvoiceExportSelection
		}
		if !seen[id] {
			unique = append(unique, id)
			seen[id] = true
		}
	}
	var records []InvoiceExportRecord
	err := DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var tickets []Ticket
		if err := tx.Where("id IN ? AND type = ?", unique, TicketTypeInvoice).Find(&tickets).Error; err != nil {
			return err
		}
		if len(tickets) != len(unique) {
			return ErrInvoiceExportIncomplete
		}
		var invoices []TicketInvoice
		if err := tx.Where("ticket_id IN ?", unique).Find(&invoices).Error; err != nil {
			return err
		}
		if len(invoices) != len(unique) {
			return ErrInvoiceExportIncomplete
		}
		var messages []TicketMessage
		firstMessages := tx.Model(&TicketMessage{}).Select("MIN(id)").Where("ticket_id IN ?", unique).Group("ticket_id")
		if err := tx.Where("id IN (?)", firstMessages).Find(&messages).Error; err != nil {
			return err
		}
		ticketMap := make(map[int]Ticket, len(tickets))
		for _, ticket := range tickets {
			ticketMap[ticket.Id] = ticket
		}
		messageMap := make(map[int]TicketMessage, len(messages))
		for _, message := range messages {
			messageMap[message.TicketId] = message
		}
		invoiceMap := make(map[int]TicketInvoice, len(invoices))
		orderIDs := make(map[int][]int, len(invoices))
		allOrders := make([]int, 0)
		seenOrders := make(map[int]bool)
		for _, invoice := range invoices {
			ticket := ticketMap[invoice.TicketId]
			message, ok := messageMap[invoice.TicketId]
			if !ok || invoice.UserId != ticket.UserId || message.UserId != ticket.UserId {
				return ErrInvoiceExportIncomplete
			}
			ids, err := invoice.GetTopUpOrderIDs()
			if err != nil || len(ids) == 0 {
				return ErrInvoiceExportIncomplete
			}
			invoiceMap[invoice.TicketId] = invoice
			orderIDs[invoice.TicketId] = ids
			for _, id := range ids {
				if !seenOrders[id] {
					allOrders = append(allOrders, id)
					seenOrders[id] = true
				}
			}
		}
		var orders []TopUp
		if err := tx.Select("id", "user_id", "trade_no").Where("id IN ?", allOrders).Find(&orders).Error; err != nil {
			return err
		}
		orderMap := make(map[int]TopUp, len(orders))
		for _, order := range orders {
			orderMap[order.Id] = order
		}
		for _, id := range unique {
			record := InvoiceExportRecord{Ticket: ticketMap[id], Invoice: invoiceMap[id], Application: messageMap[id].Content}
			for _, orderID := range orderIDs[id] {
				order, ok := orderMap[orderID]
				if !ok || order.UserId != record.Invoice.UserId {
					return ErrInvoiceExportIncomplete
				}
				record.OrderNumbers = append(record.OrderNumbers, order.TradeNo)
			}
			records = append(records, record)
		}
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return nil, err
	}
	return records, nil
}
