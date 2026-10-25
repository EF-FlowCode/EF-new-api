package model

import "testing"

func TestValidateConfigOptionRejectsValuesThatCouldFailDuringPublish(t *testing.T) {
	tests := []struct {
		key   string
		value string
	}{
		{key: "InvitationCodePolicy", value: `{invalid`},
		{key: "ModelRequestRateLimitGroup", value: `{"vip":[0,0]}`},
		{key: "ModelRPMConfig", value: `{"enabled":true,"models":{"gpt":{"global_rpm":0}}}`},
		{key: "console_setting.api_info", value: `[{"url":"not-a-url"}]`},
		{key: "passkey.user_verification", value: "sometimes"},
		{key: "passkey.attachment_preference", value: "mobile"},
		{key: "PayMethods", value: `[{"name":1}]`},
		{key: "QuotaForInviter", value: "not-a-number"},
		{key: "ModelRequestRateLimitSuccessCount", value: "0"},
		{key: "SMTPPort", value: "65536"},
		{key: "RetryTimes", value: "11"},
		{key: "GitHubOAuthEnabled", value: "not-a-bool"},
		{key: "DataExportDefaultTime", value: "month"},
		{key: "Chats", value: `{invalid`},
		{key: "TicketAssignConfig", value: `{invalid`},
		{key: "monitor_setting.auto_test_channel_minutes", value: "+Inf"},
		{key: "general_setting.custom_currency_exchange_rate", value: "NaN"},
		{key: "checkin_setting.min_quota", value: "1.5"},
		{key: "QuotaPerUnit", value: "not-a-number"},
		{key: "QuotaPerUnit", value: "NaN"},
		{key: "QuotaPerUnit", value: "+Inf"},
		{key: "QuotaPerUnit", value: "0"},
		{key: "QuotaPerUnit", value: "-1"},
	}

	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			if err := validateConfigOption(tt.key, tt.value); err == nil {
				t.Fatalf("validateConfigOption(%q, %q) unexpectedly succeeded", tt.key, tt.value)
			}
		})
	}
}

func TestValidateConfigOptionAcceptsPublishableValues(t *testing.T) {
	tests := []struct {
		key   string
		value string
	}{
		{key: "InvitationCodePolicy", value: `{}`},
		{key: "ModelRequestRateLimitGroup", value: `{"vip":[0,1]}`},
		{key: "ModelRPMConfig", value: `{"enabled":true,"models":{"gpt":{"global_rpm":1}}}`},
		{key: "console_setting.api_info", value: `[]`},
		{key: "passkey.user_verification", value: "preferred"},
		{key: "passkey.attachment_preference", value: ""},
		{key: "PayMethods", value: `[{"name":"card"}]`},
		{key: "QuotaForInviter", value: "0"},
		{key: "ModelRequestRateLimitSuccessCount", value: "1"},
		{key: "SMTPPort", value: "465"},
		{key: "RetryTimes", value: "10"},
		{key: "GitHubOAuthEnabled", value: "true"},
		{key: "DataExportDefaultTime", value: "week"},
		{key: "QuotaPerUnit", value: "500000"},
	}

	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			if err := validateConfigOption(tt.key, tt.value); err != nil {
				t.Fatalf("validateConfigOption(%q, %q) = %v", tt.key, tt.value, err)
			}
		})
	}
}

func TestValidateConfigOptionRejectsNonFiniteNumbers(t *testing.T) {
	for _, value := range []string{"NaN", "+Inf", "-Inf"} {
		t.Run(value, func(t *testing.T) {
			if err := validateConfigOption("Price", value); err == nil {
				t.Fatalf("validateConfigOption(%q, %q) unexpectedly succeeded", "Price", value)
			}
		})
	}
}
