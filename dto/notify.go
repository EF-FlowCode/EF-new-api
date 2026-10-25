package dto

import relaydto "eflow.local/new-api/relaykit/dto"

type Notify = relaydto.Notify

const ContentValueParam = relaydto.ContentValueParam

const (
	NotifyTypeQuotaExceed    = relaydto.NotifyTypeQuotaExceed
	NotifyTypeChannelUpdate  = relaydto.NotifyTypeChannelUpdate
	NotifyTypeChannelTest    = relaydto.NotifyTypeChannelTest
	NotifyTypeTicketCreated  = "ticket_created"
	NotifyTypeTicketReply    = "ticket_reply"
	NotifyTypeTicketStatus   = "ticket_status"
	NotifyTypePaymentSuccess = "payment_success"
)

func NewNotify(t string, title string, content string, values []any) Notify {
	return relaydto.NewNotify(t, title, content, values)
}
