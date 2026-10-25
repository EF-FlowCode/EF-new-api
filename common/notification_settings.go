package common

import (
	"fmt"
	"math"
	"strconv"
	"sync"
	"sync/atomic"
)

// PaymentNotificationSettings is one immutable runtime version of the
// payment-success notification configuration.
type PaymentNotificationSettings struct {
	UserEnabled  bool
	AdminEnabled bool
	AdminEmail   string
}

// TicketNotificationSettings is one immutable runtime version of the ticket
// mail configuration. Callers should retain one value for a complete async
// operation so a concurrent option update cannot mix versions.
type TicketNotificationSettings struct {
	Enabled                 bool
	AdminEmail              string
	ReplyAttachmentEnabled  bool
	ReplyAttachmentMaxBytes int64
	InvoiceMinTopupMoney    float64
}

var (
	paymentNotificationSettings   atomic.Pointer[PaymentNotificationSettings]
	paymentNotificationSettingsMu sync.Mutex
	ticketNotificationSettings    atomic.Pointer[TicketNotificationSettings]
	ticketNotificationSettingsMu  sync.Mutex
)

func init() {
	publishPaymentNotificationSettings(PaymentNotificationSettings{})
	publishTicketNotificationSettings(TicketNotificationSettings{
		ReplyAttachmentEnabled:  true,
		ReplyAttachmentMaxBytes: 20 * 1024 * 1024,
	})
}

func GetPaymentNotificationSettings() PaymentNotificationSettings {
	current := paymentNotificationSettings.Load()
	if current == nil {
		return PaymentNotificationSettings{}
	}
	return *current
}

func GetTicketNotificationSettings() TicketNotificationSettings {
	current := ticketNotificationSettings.Load()
	if current == nil {
		return TicketNotificationSettings{
			ReplyAttachmentEnabled:  true,
			ReplyAttachmentMaxBytes: 20 * 1024 * 1024,
		}
	}
	return *current
}

// ValidatePaymentNotificationOptionValues parses a candidate update without
// publishing it.
func ValidatePaymentNotificationOptionValues(values map[string]string) error {
	_, err := applyPaymentNotificationOptionValues(GetPaymentNotificationSettings(), values)
	return err
}

// UpdatePaymentNotificationSettings atomically publishes one complete
// replacement after all supplied fields have been parsed.
func UpdatePaymentNotificationSettings(values map[string]string) error {
	paymentNotificationSettingsMu.Lock()
	defer paymentNotificationSettingsMu.Unlock()

	next, err := applyPaymentNotificationOptionValues(GetPaymentNotificationSettings(), values)
	if err != nil {
		return err
	}
	publishPaymentNotificationSettings(next)
	return nil
}

func applyPaymentNotificationOptionValues(current PaymentNotificationSettings, values map[string]string) (PaymentNotificationSettings, error) {
	next := current
	if value, ok := values["PaymentNotifyUserEnabled"]; ok {
		parsed, err := strconv.ParseBool(value)
		if err != nil {
			return current, fmt.Errorf("PaymentNotifyUserEnabled must be a boolean: %w", err)
		}
		next.UserEnabled = parsed
	}
	if value, ok := values["PaymentNotifyAdminEnabled"]; ok {
		parsed, err := strconv.ParseBool(value)
		if err != nil {
			return current, fmt.Errorf("PaymentNotifyAdminEnabled must be a boolean: %w", err)
		}
		next.AdminEnabled = parsed
	}
	if value, ok := values["PaymentAdminEmail"]; ok {
		next.AdminEmail = value
	}
	return next, nil
}

// ValidateTicketNotificationOptionValues parses a candidate update without
// publishing it.
func ValidateTicketNotificationOptionValues(values map[string]string) error {
	_, err := applyTicketNotificationOptionValues(GetTicketNotificationSettings(), values)
	return err
}

// UpdateTicketNotificationSettings atomically publishes one complete
// replacement after all supplied fields have been parsed.
func UpdateTicketNotificationSettings(values map[string]string) error {
	ticketNotificationSettingsMu.Lock()
	defer ticketNotificationSettingsMu.Unlock()

	next, err := applyTicketNotificationOptionValues(GetTicketNotificationSettings(), values)
	if err != nil {
		return err
	}
	publishTicketNotificationSettings(next)
	return nil
}

func applyTicketNotificationOptionValues(current TicketNotificationSettings, values map[string]string) (TicketNotificationSettings, error) {
	next := current
	if value, ok := values["TicketNotifyEnabled"]; ok {
		parsed, err := strconv.ParseBool(value)
		if err != nil {
			return current, fmt.Errorf("TicketNotifyEnabled must be a boolean: %w", err)
		}
		next.Enabled = parsed
	}
	if value, ok := values["TicketAdminEmail"]; ok {
		next.AdminEmail = value
	}
	if value, ok := values["TicketReplyAttachmentEnabled"]; ok {
		parsed, err := strconv.ParseBool(value)
		if err != nil {
			return current, fmt.Errorf("TicketReplyAttachmentEnabled must be a boolean: %w", err)
		}
		next.ReplyAttachmentEnabled = parsed
	}
	if value, ok := values["TicketReplyAttachmentMaxBytes"]; ok {
		parsed, err := strconv.ParseInt(value, 10, 64)
		if err != nil || parsed <= 0 {
			return current, fmt.Errorf("TicketReplyAttachmentMaxBytes must be a positive integer")
		}
		next.ReplyAttachmentMaxBytes = parsed
	}
	if value, ok := values["TicketInvoiceMinTopupMoney"]; ok {
		parsed, err := strconv.ParseFloat(value, 64)
		if err != nil || math.IsNaN(parsed) || math.IsInf(parsed, 0) || parsed < 0 {
			return current, fmt.Errorf("TicketInvoiceMinTopupMoney must be a non-negative number")
		}
		next.InvoiceMinTopupMoney = parsed
	}
	return next, nil
}

func publishPaymentNotificationSettings(next PaymentNotificationSettings) {
	paymentNotificationSettings.Store(&next)
}

func publishTicketNotificationSettings(next TicketNotificationSettings) {
	ticketNotificationSettings.Store(&next)
}
