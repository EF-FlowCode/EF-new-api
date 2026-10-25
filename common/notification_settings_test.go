package common

import (
	"fmt"
	"strconv"
	"sync"
	"testing"
)

func restoreNotificationSettings(t *testing.T) {
	t.Helper()
	payment := GetPaymentNotificationSettings()
	ticket := GetTicketNotificationSettings()
	t.Cleanup(func() {
		_ = UpdatePaymentNotificationSettings(map[string]string{
			"PaymentNotifyUserEnabled":  strconv.FormatBool(payment.UserEnabled),
			"PaymentNotifyAdminEnabled": strconv.FormatBool(payment.AdminEnabled),
			"PaymentAdminEmail":         payment.AdminEmail,
		})
		_ = UpdateTicketNotificationSettings(map[string]string{
			"TicketNotifyEnabled":           strconv.FormatBool(ticket.Enabled),
			"TicketAdminEmail":              ticket.AdminEmail,
			"TicketReplyAttachmentEnabled":  strconv.FormatBool(ticket.ReplyAttachmentEnabled),
			"TicketReplyAttachmentMaxBytes": strconv.FormatInt(ticket.ReplyAttachmentMaxBytes, 10),
			"TicketInvoiceMinTopupMoney":    strconv.FormatFloat(ticket.InvoiceMinTopupMoney, 'f', -1, 64),
		})
	})
}

func TestNotificationSettingsInvalidBatchDoesNotPublish(t *testing.T) {
	restoreNotificationSettings(t)
	if err := UpdatePaymentNotificationSettings(map[string]string{
		"PaymentNotifyUserEnabled":  "false",
		"PaymentNotifyAdminEnabled": "false",
		"PaymentAdminEmail":         "before@example.com",
	}); err != nil {
		t.Fatal(err)
	}
	before := GetPaymentNotificationSettings()
	if err := UpdatePaymentNotificationSettings(map[string]string{
		"PaymentNotifyUserEnabled":  "true",
		"PaymentNotifyAdminEnabled": "invalid",
		"PaymentAdminEmail":         "after@example.com",
	}); err == nil {
		t.Fatal("expected invalid batch to fail")
	}
	if got := GetPaymentNotificationSettings(); got != before {
		t.Fatalf("invalid batch published: got %+v, want %+v", got, before)
	}
}

func TestPaymentNotificationSettingsConcurrentVersions(t *testing.T) {
	restoreNotificationSettings(t)
	a := map[string]string{
		"PaymentNotifyUserEnabled":  "true",
		"PaymentNotifyAdminEnabled": "false",
		"PaymentAdminEmail":         "a@example.com",
	}
	b := map[string]string{
		"PaymentNotifyUserEnabled":  "false",
		"PaymentNotifyAdminEnabled": "true",
		"PaymentAdminEmail":         "b@example.com",
	}
	if err := UpdatePaymentNotificationSettings(a); err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	errs := make(chan error, 1)
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := range 2_000 {
			values := a
			if i%2 == 1 {
				values = b
			}
			if err := UpdatePaymentNotificationSettings(values); err != nil {
				select {
				case errs <- err:
				default:
				}
				return
			}
		}
	}()
	go func() {
		defer wg.Done()
		for range 4_000 {
			got := GetPaymentNotificationSettings()
			isA := got.UserEnabled && !got.AdminEnabled && got.AdminEmail == "a@example.com"
			isB := !got.UserEnabled && got.AdminEnabled && got.AdminEmail == "b@example.com"
			if !isA && !isB {
				select {
				case errs <- fmt.Errorf("torn payment notification settings: %+v", got):
				default:
				}
				return
			}
		}
	}()
	wg.Wait()
	select {
	case err := <-errs:
		t.Fatal(err)
	default:
	}
}
