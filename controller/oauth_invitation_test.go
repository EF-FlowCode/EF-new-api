package controller

import (
	"testing"

	"eflow.local/new-api/common"
)

func TestOAuthInvitationCodeFollowsMasterSwitch(t *testing.T) {
	settings := common.AuthSettings{
		InvitationCodeEnabled:       false,
		InvitationCodeOAuthRequired: true,
	}
	if shouldValidateOAuthInvitationCode("", settings) {
		t.Fatal("disabled invitation registration must ignore OAuth-required setting")
	}
	if shouldValidateOAuthInvitationCode("stale-session-code", settings) {
		t.Fatal("disabled invitation registration must ignore stale OAuth invitation code")
	}

	settings.InvitationCodeEnabled = true
	if !shouldValidateOAuthInvitationCode("", settings) {
		t.Fatal("enabled invitation registration must enforce OAuth-required setting")
	}
	settings.InvitationCodeOAuthRequired = false
	if !shouldValidateOAuthInvitationCode("provided-code", settings) {
		t.Fatal("enabled invitation registration must validate a provided code")
	}
}
