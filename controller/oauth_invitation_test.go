package controller

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
)

func TestOAuthInvitationCodeFollowsMasterSwitch(t *testing.T) {
	originalEnabled := common.InvitationCodeEnabled
	originalRequired := common.InvitationCodeOAuthRequired
	t.Cleanup(func() {
		common.InvitationCodeEnabled = originalEnabled
		common.InvitationCodeOAuthRequired = originalRequired
	})

	common.InvitationCodeEnabled = false
	common.InvitationCodeOAuthRequired = true
	if shouldValidateOAuthInvitationCode("") {
		t.Fatal("disabled invitation registration must ignore OAuth-required setting")
	}
	if shouldValidateOAuthInvitationCode("stale-session-code") {
		t.Fatal("disabled invitation registration must ignore stale OAuth invitation code")
	}

	common.InvitationCodeEnabled = true
	if !shouldValidateOAuthInvitationCode("") {
		t.Fatal("enabled invitation registration must enforce OAuth-required setting")
	}
	common.InvitationCodeOAuthRequired = false
	if !shouldValidateOAuthInvitationCode("provided-code") {
		t.Fatal("enabled invitation registration must validate a provided code")
	}
}
