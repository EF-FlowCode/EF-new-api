package service

import (
	"testing"

	"eflow.local/new-api/model"
	"eflow.local/new-api/setting/system_setting"
)

func TestNotificationTemplateUsesCapturedServerAddress(t *testing.T) {
	before := system_setting.GetServerAddress()
	t.Cleanup(func() {
		_ = system_setting.UpdateServerSettings(map[string]string{"ServerAddress": before})
	})
	if err := system_setting.UpdateServerSettings(map[string]string{"ServerAddress": "https://a.example"}); err != nil {
		t.Fatal(err)
	}
	captured := system_setting.GetServerAddress()
	if err := system_setting.UpdateServerSettings(map[string]string{"ServerAddress": "https://b.example"}); err != nil {
		t.Fatal(err)
	}

	topUpVars := buildTopUpVars(&model.TopUp{}, &model.User{}, false, captured)
	if got := topUpVars["server_address"]; got != "https://a.example" {
		t.Fatalf("top-up template address = %q", got)
	}
	ticketVars := buildTicketVars(&model.Ticket{}, nil, false, "status", captured)
	if got := ticketVars["server_address"]; got != "https://a.example" {
		t.Fatalf("ticket template address = %q", got)
	}
}
