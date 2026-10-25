package oauth

import (
	"strconv"
	"testing"

	settingconfig "eflow.local/new-api/setting/config"
	"eflow.local/new-api/setting/system_setting"
)

func TestGetProviderCapturesOIDCSnapshotForRequest(t *testing.T) {
	original := *system_setting.GetOIDCSettings()
	originalServer := system_setting.GetServerSettings()
	t.Cleanup(func() {
		if _, err := settingconfig.GlobalConfig.Update("oidc", oidcOptionValues(original)); err != nil {
			t.Fatalf("restore OIDC settings: %v", err)
		}
		if err := system_setting.UpdateServerSettings(map[string]string{"ServerAddress": originalServer.Address}); err != nil {
			t.Fatalf("restore server settings: %v", err)
		}
	})

	versionA := oidcSnapshotSettings("a", true)
	versionB := oidcSnapshotSettings("b", false)
	if _, err := settingconfig.GlobalConfig.Update("oidc", oidcOptionValues(versionA)); err != nil {
		t.Fatalf("publish OIDC version A: %v", err)
	}
	if err := system_setting.UpdateServerSettings(map[string]string{"ServerAddress": "https://server-a.example.com"}); err != nil {
		t.Fatalf("publish server version A: %v", err)
	}

	providerA, ok := GetProvider("oidc").(*OIDCProvider)
	if !ok {
		t.Fatal("OIDC provider is not registered")
	}
	if _, err := settingconfig.GlobalConfig.Update("oidc", oidcOptionValues(versionB)); err != nil {
		t.Fatalf("publish OIDC version B: %v", err)
	}
	if err := system_setting.UpdateServerSettings(map[string]string{"ServerAddress": "https://server-b.example.com"}); err != nil {
		t.Fatalf("publish server version B: %v", err)
	}

	if got := providerA.oidcSettings(); got != versionA {
		t.Fatalf("existing OIDC request changed versions: got %+v, want %+v", got, versionA)
	}
	if got := providerA.oidcServerAddress(); got != "https://server-a.example.com" {
		t.Fatalf("existing OIDC request server = %q, want version A", got)
	}
	providerB := GetProvider("oidc").(*OIDCProvider)
	if got := providerB.oidcSettings(); got != versionB {
		t.Fatalf("new OIDC request did not capture version B: got %+v, want %+v", got, versionB)
	}
	if got := providerB.oidcServerAddress(); got != "https://server-b.example.com" {
		t.Fatalf("new OIDC request server = %q, want version B", got)
	}

	if err := system_setting.UpdateServerSettings(map[string]string{"ServerAddress": ""}); err != nil {
		t.Fatalf("publish empty server version: %v", err)
	}
	providerWithEmptyServer := GetProvider("oidc").(*OIDCProvider)
	if err := system_setting.UpdateServerSettings(map[string]string{"ServerAddress": "https://server-c.example.com"}); err != nil {
		t.Fatalf("publish server version C: %v", err)
	}
	if got := providerWithEmptyServer.oidcServerAddress(); got != "" {
		t.Fatalf("captured empty server changed to %q", got)
	}
}

func TestLinuxDOTrustLevelValidationIsRegistrationOnlyPolicy(t *testing.T) {
	provider := &LinuxDOProvider{settings: &system_setting.LegacyOAuthSettings{LinuxDOMinimumTrustLevel: 3}}
	user := &OAuthUser{Extra: map[string]any{"trust_level": 2}}

	err := provider.ValidateNewUser(t.Context(), user)
	trustErr, ok := err.(*TrustLevelError)
	if !ok {
		t.Fatalf("ValidateNewUser error = %T, want *TrustLevelError", err)
	}
	if trustErr.Required != 3 || trustErr.Current != 2 {
		t.Fatalf("trust error = %+v, want required=3 current=2", trustErr)
	}

	user.Extra["trust_level"] = 3
	if err := provider.ValidateNewUser(t.Context(), user); err != nil {
		t.Fatalf("minimum accepted trust level rejected: %v", err)
	}
}

func oidcSnapshotSettings(version string, enabled bool) system_setting.OIDCSettings {
	return system_setting.OIDCSettings{
		Enabled:               enabled,
		DisplayName:           "OIDC " + version,
		ClientId:              "client-" + version,
		ClientSecret:          "secret-" + version,
		WellKnown:             "https://" + version + ".example.com/.well-known/openid-configuration",
		AuthorizationEndpoint: "https://" + version + ".example.com/authorize",
		TokenEndpoint:         "https://" + version + ".example.com/token",
		UserInfoEndpoint:      "https://" + version + ".example.com/userinfo",
	}
}

func oidcOptionValues(settings system_setting.OIDCSettings) map[string]string {
	return map[string]string{
		"enabled":                strconv.FormatBool(settings.Enabled),
		"display_name":           settings.DisplayName,
		"client_id":              settings.ClientId,
		"client_secret":          settings.ClientSecret,
		"well_known":             settings.WellKnown,
		"authorization_endpoint": settings.AuthorizationEndpoint,
		"token_endpoint":         settings.TokenEndpoint,
		"user_info_endpoint":     settings.UserInfoEndpoint,
	}
}
