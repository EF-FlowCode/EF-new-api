package constant

import "testing"

func TestAccountEmailTemplateValidation(t *testing.T) {
	for _, key := range []string{EmailTemplateKeyPasswordReset, EmailTemplateKeyQuotaWarning} {
		spec, ok := FindEmailTemplateSpec(key)
		if !ok {
			t.Fatalf("missing spec %s", key)
		}
		for field, value := range map[string]string{"subject": spec.DefaultSubject, "body": spec.DefaultBody} {
			if err := ValidateEmailTemplateField(spec, field, value); err != nil {
				t.Fatal(err)
			}
			if err := ValidateEmailTemplateOption(EmailTemplateOptionPrefix+key+"."+field, ""); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, tc := range []struct {
		name, key, field, value string
		invalid                 bool
	}{
		{"missing URL", EmailTemplateKeyPasswordReset, "body", "<p>{{valid_minutes}}</p>", true},
		{"missing expiry", EmailTemplateKeyPasswordReset, "body", "{{reset_url}}", true},
		{"missing balance", EmailTemplateKeyQuotaWarning, "body", "{{topup_url}}", true},
		{"missing topup", EmailTemplateKeyQuotaWarning, "body", "{{remaining_quota}}", true},
		{"unknown", EmailTemplateKeyPasswordReset, "subject", "{{system_nam}}", true},
		{"unclosed", EmailTemplateKeyPasswordReset, "subject", "{{system_name", true},
		{"unopened", EmailTemplateKeyPasswordReset, "subject", "system_name}}", true},
		{"nested", EmailTemplateKeyPasswordReset, "subject", "{{{system_name}}}", true},
		{"blank", EmailTemplateKeyPasswordReset, "body", " \n\t", true},
		{"header newline", EmailTemplateKeyPasswordReset, "subject", "账户\r\nBcc: x@example.com", true},
		{"custom", EmailTemplateKeyPasswordReset, "body", "<style>p {color:red;}</style><p>{{ valid_minutes }} 分钟：{{ reset_url }}</p>", false},
		{"literal subject", EmailTemplateKeyQuotaWarning, "subject", "账户通知", false},
		{"legacy", EmailTemplateKeyTicketCreatedAdmin, "body", "{{unknown}} {{info_table}}", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateEmailTemplateOption(EmailTemplateOptionPrefix+tc.key+"."+tc.field, tc.value)
			if (err != nil) != tc.invalid {
				t.Fatalf("error = %v, want invalid %v", err, tc.invalid)
			}
		})
	}
}
