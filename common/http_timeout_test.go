package common

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRelayResponseHeaderTimeoutEnvironment(t *testing.T) {
	for _, tc := range []struct {
		name, current, legacy string
		want                  int
	}{
		{"default", "", "", 1800}, {"legacy", "", "42", 42},
		{"new wins", "23", "42", 23}, {"disabled", "0", "42", 0},
		{"legacy disabled", "", "0", 0}, {"invalid", "oops", "42", 1800},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("RELAY_RESPONSE_HEADER_TIMEOUT", tc.current)
			if tc.current == "" {
				require.NoError(t, os.Unsetenv("RELAY_RESPONSE_HEADER_TIMEOUT"))
			}
			t.Setenv("RESPONSE_HEADER_TIMEOUT", tc.legacy)
			require.Equal(t, tc.want, relayResponseHeaderTimeoutFromEnv())
		})
	}
}
