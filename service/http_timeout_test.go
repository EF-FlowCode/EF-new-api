package service

import (
	"math"
	"net/http"
	"testing"
	"time"

	"eflow.local/new-api/common"
	"github.com/stretchr/testify/require"
)

func TestRelayResponseHeaderTimeoutAndTLSIsolation(t *testing.T) {
	oldSeconds, oldInsecure := common.RelayResponseHeaderTimeout, common.TLSInsecureSkipVerify
	oldTransport := http.DefaultTransport
	t.Cleanup(func() {
		common.RelayResponseHeaderTimeout, common.TLSInsecureSkipVerify = oldSeconds, oldInsecure
		http.DefaultTransport = oldTransport
	})
	http.DefaultTransport = &http.Transport{ResponseHeaderTimeout: time.Second}
	common.TLSInsecureSkipVerify = true
	for _, seconds := range []int{0, -1, 1800, math.MaxInt} {
		common.RelayResponseHeaderTimeout = seconds
		transport := newRelayHTTPTransport()
		want := time.Duration(0)
		if seconds > 0 {
			want = time.Duration(min(seconds, int(math.MaxInt64/int64(time.Second)))) * time.Second
		}
		require.Equal(t, want, transport.ResponseHeaderTimeout)
		require.NotSame(t, common.InsecureTLSConfig, transport.TLSClientConfig)
		transport.TLSClientConfig.ServerName = "isolated.invalid"
		require.NotEqual(t, "isolated.invalid", common.InsecureTLSConfig.ServerName)
	}
}
