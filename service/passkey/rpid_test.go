package passkey

import (
	"testing"

	"eflow.local/new-api/model"
	"eflow.local/new-api/setting/system_setting"
	"github.com/stretchr/testify/require"
)

func TestSelectRPIDMatchesRequestHost(t *testing.T) {
	allowed := []string{"e-flowcode.cc", "example.org"}
	require.Equal(t, "e-flowcode.cc", selectRPID(allowed, "e-flowcode.cc"))
	require.Equal(t, "example.org", selectRPID(allowed, "console.example.org"))
	require.Equal(t, "e-flowcode.cc", selectRPID(allowed, "unrelated.net"), "unknown hosts use the primary RP ID")
	require.Equal(t, "e-flowcode.cc", selectRPID(allowed, "evil-e-flowcode.cc"), "suffix matching is per label")

	settings := system_setting.PasskeySettings{RPID: " E-FlowCode.cc , example.org,e-flowcode.cc "}
	require.Equal(t, []string{"e-flowcode.cc", "example.org"}, settings.RPIDs())
}

func TestCheckCredentialRPID(t *testing.T) {
	legacy := &model.PasskeyCredential{}
	require.NoError(t, CheckCredentialRPID(legacy, "e-flowcode.cc"))
	require.NotNil(t, legacy.RPID, "credentials without a recorded domain adopt the one they were used on")
	require.Equal(t, "e-flowcode.cc", *legacy.RPID)

	require.NoError(t, CheckCredentialRPID(legacy, "e-flowcode.cc"))
	require.Error(t, CheckCredentialRPID(legacy, "example.org"))
}
