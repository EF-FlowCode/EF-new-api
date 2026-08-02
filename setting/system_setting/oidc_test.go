package system_setting

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestOIDCSettingsGetEffectiveDisplayName(t *testing.T) {
	tests := []struct {
		name        string
		displayName string
		want        string
	}{
		{"empty", "", "OIDC"},
		{"whitespace", "   ", "OIDC"},
		{"custom", "  Acme SSO  ", "Acme SSO"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			settings := &OIDCSettings{DisplayName: test.displayName}
			assert.Equal(t, test.want, settings.GetEffectiveDisplayName())
		})
	}
}
