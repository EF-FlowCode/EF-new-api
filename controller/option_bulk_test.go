package controller

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"eflow.local/new-api/constant"
	"github.com/gin-gonic/gin"
)

func TestIsBulkOptionAllowedUsesExplicitDomainWhitelist(t *testing.T) {
	allowed := []string{
		"payment_setting.amount_discount",
		"StripePriceId",
		"fetch_setting.enable_ssrf_protection",
		"monitor_setting.channel_test_concurrency",
		"performance_setting.disk_cache_enabled",
		"group_monitoring_setting.group_categories",
		"group_monitoring_setting.category_order",
	}
	for _, key := range allowed {
		if !isBulkOptionAllowed(key) {
			t.Errorf("bulk form save must allow %q", key)
		}
	}

	denied := []string{
		"unrelated.option",
		"payment_setting.compliance_confirmed",
		"payment_setting.compliance_confirmed_by",
		"WaffoPancakeEnabled",
		"WaffoPancakeSandbox",
		"WaffoPancakeCurrency",
		"WaffoPancakeWebhookPublicKey",
		"WaffoPancakeWebhookTestKey",
		"EmailTemplate.unknown.subject",
		"EmailTemplate.ticket_created_admin.other",
	}
	for _, key := range denied {
		if isBulkOptionAllowed(key) {
			t.Errorf("bulk form save must reject %q", key)
		}
	}

	for _, spec := range constant.EmailTemplateSpecs() {
		for _, key := range []string{
			constant.EmailTemplateSubjectKey(spec.Key),
			constant.EmailTemplateBodyKey(spec.Key),
		} {
			if !isBulkOptionAllowed(key) {
				t.Errorf("registered email template option must be allowed: %q", key)
			}
		}
	}
}

func TestValidateOptionDependenciesAcceptsCredentialsFromSameBatch(t *testing.T) {
	for _, requirement := range enabledOptionRequirements {
		updates := map[string]string{requirement.enabledKey: "true"}
		for _, key := range requirement.requiredKeys {
			updates[key] = "configured"
		}
		if err := validateOptionDependencies(nil, updates, true); err != nil {
			t.Errorf("%s complete same-batch configuration rejected: %v", requirement.enabledKey, err)
		}

		delete(updates, requirement.requiredKeys[len(requirement.requiredKeys)-1])
		if err := validateOptionDependencies(nil, updates, true); err == nil {
			t.Errorf("%s missing dependency unexpectedly accepted", requirement.enabledKey)
		}
	}
}

func TestValidateOptionDependenciesMergesCurrentSnapshot(t *testing.T) {
	current := map[string]string{
		"GitHubClientId":     "existing-id",
		"GitHubClientSecret": "existing-secret",
	}
	updates := map[string]string{"GitHubOAuthEnabled": "true"}
	if err := validateOptionDependencies(current, updates, true); err != nil {
		t.Fatalf("current credentials should satisfy enabled dependency: %v", err)
	}
}

func TestValidateOptionDependenciesRejectsUnsafeRelatedUpdates(t *testing.T) {
	tests := []struct {
		name       string
		current    map[string]string
		updates    map[string]string
		compliance bool
	}{
		{
			name:       "positive inviter reward without compliance",
			updates:    map[string]string{"QuotaForInviter": "1"},
			compliance: false,
		},
		{
			name: "clearing credential while provider remains enabled",
			current: map[string]string{
				"GitHubOAuthEnabled": "true",
				"GitHubClientId":     "existing-id",
				"GitHubClientSecret": "existing-secret",
			},
			updates:    map[string]string{"GitHubClientSecret": ""},
			compliance: true,
		},
		{
			name: "invalid email domain whitelist",
			updates: map[string]string{
				"EmailDomainRestrictionEnabled": "true",
				"EmailDomainWhitelist":          "not a domain",
			},
			compliance: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := validateOptionDependencies(tt.current, tt.updates, tt.compliance); err == nil {
				t.Fatal("unsafe related update unexpectedly accepted")
			}
		})
	}
}

func TestValidateOptionDependenciesAcceptsValidEmailDomain(t *testing.T) {
	updates := map[string]string{
		"EmailDomainRestrictionEnabled": "true",
		"EmailDomainWhitelist":          "example.com, invalid value",
	}
	if err := validateOptionDependencies(nil, updates, true); err != nil {
		t.Fatalf("valid domain in whitelist should be accepted: %v", err)
	}
}

func TestUpdateOptionsBulkRejectsInvalidKeysBeforePersistence(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{
			name: "duplicate key",
			body: `{"options":[{"key":"Price","value":1},{"key":"Price","value":2}]}`,
		},
		{
			name: "compliance key",
			body: `{"options":[{"key":"payment_setting.compliance_confirmed","value":true}]}`,
		},
		{
			name: "blank key",
			body: `{"options":[{"key":"   ","value":true}]}`,
		},
	}

	gin.SetMode(gin.TestMode)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			ctx.Request = httptest.NewRequest(http.MethodPut, "/api/option/bulk", strings.NewReader(tt.body))
			ctx.Request.Header.Set("Content-Type", "application/json")

			UpdateOptionsBulk(ctx)

			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusBadRequest, recorder.Body.String())
			}
			var response struct {
				Success bool `json:"success"`
			}
			if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			if response.Success {
				t.Fatal("invalid bulk request unexpectedly succeeded")
			}
		})
	}
}
