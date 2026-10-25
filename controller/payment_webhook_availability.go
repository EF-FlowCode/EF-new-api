package controller

import (
	"strings"

	"eflow.local/new-api/setting"
	"eflow.local/new-api/setting/operation_setting"
)

func isPaymentComplianceConfirmed() bool {
	return operation_setting.IsPaymentComplianceConfirmed()
}

func isStripeTopUpEnabled() bool {
	return isStripeTopUpEnabledWithConfig(setting.GetStripeConfig())
}

func isStripeTopUpEnabledWithConfig(config setting.StripeConfig) bool {
	return isPaymentComplianceConfirmed() && hasStripeTopUpConfig(config)
}

func hasStripeTopUpConfig(config setting.StripeConfig) bool {
	return strings.TrimSpace(config.APISecret) != "" &&
		strings.TrimSpace(config.WebhookSecret) != "" &&
		strings.TrimSpace(config.PriceID) != ""
}

func isStripeWebhookConfigured() bool {
	return isStripeWebhookConfiguredWithConfig(setting.GetStripeConfig())
}

func isStripeWebhookConfiguredWithConfig(config setting.StripeConfig) bool {
	return strings.TrimSpace(config.WebhookSecret) != ""
}

func isStripeWebhookEnabled() bool {
	return isStripeWebhookEnabledWithConfig(setting.GetStripeConfig())
}

func isStripeWebhookEnabledWithConfig(config setting.StripeConfig) bool {
	return isStripeTopUpEnabledWithConfig(config)
}

func isCreemTopUpEnabled() bool {
	return isCreemTopUpEnabledWithConfig(setting.GetCreemConfig())
}

func isCreemTopUpEnabledWithConfig(config setting.CreemConfig) bool {
	return isPaymentComplianceConfirmed() && hasCreemTopUpConfig(config)
}

func hasCreemTopUpConfig(config setting.CreemConfig) bool {
	return strings.TrimSpace(config.APIKey) != "" && len(config.Products) > 0
}

func isCreemWebhookConfigured() bool {
	return isCreemWebhookConfiguredWithConfig(setting.GetCreemConfig())
}

func isCreemWebhookConfiguredWithConfig(config setting.CreemConfig) bool {
	return strings.TrimSpace(config.WebhookSecret) != ""
}

func isCreemWebhookEnabled() bool {
	return isCreemWebhookEnabledWithConfig(setting.GetCreemConfig())
}

func isCreemWebhookEnabledWithConfig(config setting.CreemConfig) bool {
	return isCreemTopUpEnabledWithConfig(config) && isCreemWebhookConfiguredWithConfig(config)
}

func isWaffoTopUpEnabled() bool {
	return isWaffoTopUpEnabledWithConfig(setting.GetWaffoConfig())
}

func isWaffoTopUpEnabledWithConfig(config setting.WaffoConfig) bool {
	return isPaymentComplianceConfirmed() && hasWaffoTopUpConfig(config)
}

func hasWaffoTopUpConfig(config setting.WaffoConfig) bool {
	if !config.Enabled {
		return false
	}

	return isWaffoWebhookConfiguredWithConfig(config)
}

func isWaffoWebhookConfigured() bool {
	return isWaffoWebhookConfiguredWithConfig(setting.GetWaffoConfig())
}

func isWaffoWebhookConfiguredWithConfig(config setting.WaffoConfig) bool {
	if config.Sandbox {
		return strings.TrimSpace(config.SandboxAPIKey) != "" &&
			strings.TrimSpace(config.SandboxPrivateKey) != "" &&
			strings.TrimSpace(config.SandboxPublicCert) != ""
	}

	return strings.TrimSpace(config.APIKey) != "" &&
		strings.TrimSpace(config.PrivateKey) != "" &&
		strings.TrimSpace(config.PublicCert) != ""
}

func isWaffoWebhookEnabled() bool {
	return isWaffoWebhookEnabledWithConfig(setting.GetWaffoConfig())
}

func isWaffoWebhookEnabledWithConfig(config setting.WaffoConfig) bool {
	return isWaffoTopUpEnabledWithConfig(config)
}

func isWaffoPancakeTopUpEnabled() bool {
	return isWaffoPancakeTopUpEnabledWithConfig(setting.GetWaffoPancakeConfig())
}

func isWaffoPancakeTopUpEnabledWithConfig(config setting.WaffoPancakeConfig) bool {
	if !isPaymentComplianceConfirmed() {
		return false
	}
	return isWaffoPancakeConfigured(config)
}

func isWaffoPancakeConfigured(config setting.WaffoPancakeConfig) bool {
	// Presence-of-credentials = enabled. Webhook public keys ship inside
	// the SDK; mode (test/prod) is read from each event.
	return strings.TrimSpace(config.MerchantID) != "" &&
		strings.TrimSpace(config.PrivateKey) != "" &&
		strings.TrimSpace(config.ProductID) != ""
}

func isWaffoPancakeWebhookConfigured() bool {
	return isWaffoPancakeTopUpEnabled()
}

func isWaffoPancakeWebhookEnabled() bool {
	return isWaffoPancakeTopUpEnabled()
}

func isEpayTopUpEnabled() bool {
	return isEpayTopUpEnabledWithConfig(operation_setting.GetEpayConfig())
}

func isEpayTopUpEnabledWithConfig(config operation_setting.EpayConfig) bool {
	return isPaymentComplianceConfirmed() && hasEpayTopUpConfig(config)
}

func hasEpayTopUpConfig(config operation_setting.EpayConfig) bool {
	return isEpayWebhookConfiguredWithConfig(config) && len(config.PayMethods) > 0
}

func isEpayWebhookConfigured() bool {
	return isEpayWebhookConfiguredWithConfig(operation_setting.GetEpayConfig())
}

func isEpayWebhookConfiguredWithConfig(config operation_setting.EpayConfig) bool {
	return strings.TrimSpace(config.PayAddress) != "" &&
		strings.TrimSpace(config.EpayID) != "" &&
		strings.TrimSpace(config.EpayKey) != ""
}

func isEpayWebhookEnabled() bool {
	return isEpayWebhookEnabledWithConfig(operation_setting.GetEpayConfig())
}

func isEpayWebhookEnabledWithConfig(config operation_setting.EpayConfig) bool {
	return isEpayTopUpEnabledWithConfig(config)
}
