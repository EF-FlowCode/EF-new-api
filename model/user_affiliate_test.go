package model

import (
	"testing"

	"eflow.local/new-api/common"
	"eflow.local/new-api/internal/testconfig"
	"eflow.local/new-api/setting/operation_setting"
)

func TestShouldIssueAffiliateReward(t *testing.T) {
	testconfig.Set(t, "payment_setting", map[string]string{
		"compliance_confirmed":     "true",
		"compliance_terms_version": operation_setting.CurrentComplianceTermsVersion,
	})

	enabled := common.UserQuotaSettings{AffiliateEnabled: true}
	disabled := common.UserQuotaSettings{AffiliateEnabled: false}
	if !shouldIssueAffiliateReward(1, enabled) {
		t.Fatal("enabled referral program should issue rewards")
	}

	if shouldIssueAffiliateReward(1, disabled) {
		t.Fatal("disabled referral program must not issue rewards")
	}

	if shouldIssueAffiliateReward(0, enabled) {
		t.Fatal("missing inviter must not issue rewards")
	}

	testconfig.Update(t, "payment_setting", map[string]string{
		"compliance_confirmed": "false",
	})
	if shouldIssueAffiliateReward(1, enabled) {
		t.Fatal("unconfirmed payment compliance must not issue rewards")
	}
}
