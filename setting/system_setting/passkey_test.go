package system_setting

import (
	"testing"

	"eflow.local/new-api/internal/testconfig"
	"eflow.local/new-api/setting/config"
)

func TestGetPasskeySettingsDerivesRequestDefaultsWithoutPublishing(t *testing.T) {
	originalServerAddress := GetServerAddress()
	if err := UpdateServerSettings(map[string]string{"ServerAddress": "https://new-api.example:8443"}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = UpdateServerSettings(map[string]string{"ServerAddress": originalServerAddress})
	})
	testconfig.Set(t, "passkey", map[string]string{
		"rp_id":   "",
		"origins": "",
	})

	settings := GetPasskeySettings()
	if settings.RPID != "new-api.example:8443" {
		t.Fatalf("derived RPID = %q, want %q", settings.RPID, "new-api.example:8443")
	}
	if settings.Origins != GetServerAddress() {
		t.Fatalf("derived origins = %q, want %q", settings.Origins, GetServerAddress())
	}

	raw := config.MustSnapshot[PasskeySettings]("passkey")
	if raw.RPID != "" || raw.Origins != "" {
		t.Fatalf("derived passkey fields leaked into published config: %+v", raw)
	}
}
