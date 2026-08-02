package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"
)

func TestShouldIssueAffiliateReward(t *testing.T) {
	originalEnabled := common.AffiliateEnabled
	payment := operation_setting.GetPaymentSetting()
	originalConfirmed := payment.ComplianceConfirmed
	originalVersion := payment.ComplianceTermsVersion
	t.Cleanup(func() {
		common.AffiliateEnabled = originalEnabled
		payment.ComplianceConfirmed = originalConfirmed
		payment.ComplianceTermsVersion = originalVersion
	})

	payment.ComplianceConfirmed = true
	payment.ComplianceTermsVersion = operation_setting.CurrentComplianceTermsVersion

	common.AffiliateEnabled = true
	if !shouldIssueAffiliateReward(1) {
		t.Fatal("enabled referral program should issue rewards")
	}

	common.AffiliateEnabled = false
	if shouldIssueAffiliateReward(1) {
		t.Fatal("disabled referral program must not issue rewards")
	}

	common.AffiliateEnabled = true
	if shouldIssueAffiliateReward(0) {
		t.Fatal("missing inviter must not issue rewards")
	}

	payment.ComplianceConfirmed = false
	if shouldIssueAffiliateReward(1) {
		t.Fatal("unconfirmed payment compliance must not issue rewards")
	}
}
