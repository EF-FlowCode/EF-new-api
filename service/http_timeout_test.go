package service

import (
	"context"
	"errors"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"eflow.local/new-api/common"
	"eflow.local/new-api/setting/config"
	"eflow.local/new-api/setting/operation_setting"
	"github.com/stretchr/testify/require"
)

func TestRelayTransportLeavesHeaderTimeoutToRoundTripperAndIsolatesTLS(t *testing.T) {
	oldInsecure := common.TLSInsecureSkipVerify
	oldTransport := http.DefaultTransport
	t.Cleanup(func() {
		common.TLSInsecureSkipVerify = oldInsecure
		http.DefaultTransport = oldTransport
	})
	http.DefaultTransport = &http.Transport{ResponseHeaderTimeout: time.Second}
	common.TLSInsecureSkipVerify = true

	transport := newRelayHTTPTransport()
	require.Zero(t, transport.ResponseHeaderTimeout)
	require.NotSame(t, common.InsecureTLSConfig, transport.TLSClientConfig)
	transport.TLSClientConfig.ServerName = "isolated.invalid"
	require.NotEqual(t, "isolated.invalid", common.InsecureTLSConfig.ServerName)

	_, wrapped := newRelayHTTPClient(transport).Transport.(*headerTimeoutRoundTripper)
	require.True(t, wrapped)
}

func setHeaderTimeouts(t *testing.T, streamSeconds, nonStreamSeconds string) {
	t.Helper()
	update := func(stream, nonStream string) {
		_, err := config.GlobalConfig.Update("runtime_limit_setting", map[string]string{
			"response_header_timeout_seconds":            stream,
			"non_stream_response_header_timeout_seconds": nonStream,
		})
		require.NoError(t, err)
	}
	update(streamSeconds, nonStreamSeconds)
	t.Cleanup(func() { update("null", "null") })
}

func TestRelayResponseHeaderTimeoutSeparatesStreamAndNonStream(t *testing.T) {
	setHeaderTimeouts(t, "120", "600")
	require.Equal(t, 120*time.Second, RelayResponseHeaderTimeout(true))
	require.Equal(t, 600*time.Second, RelayResponseHeaderTimeout(false))
	require.Equal(t, 120*time.Second, responseHeaderTimeoutFor(context.Background()))

	setHeaderTimeouts(t, "0", "-1")
	require.Zero(t, RelayResponseHeaderTimeout(true))
	require.Zero(t, RelayResponseHeaderTimeout(false))

	require.Equal(t, time.Duration(math.MaxInt64/int64(time.Second))*time.Second, headerTimeoutDuration(math.MaxInt))
}

func TestHeaderTimeoutRoundTripperPerRequest(t *testing.T) {
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "image")
	}))
	t.Cleanup(server.Close)
	t.Cleanup(func() { close(release) })

	setHeaderTimeouts(t, "3600", "3600")
	client := &http.Client{Transport: &headerTimeoutRoundTripper{base: &http.Transport{}}}

	short, err := http.NewRequest(http.MethodGet, server.URL, nil)
	require.NoError(t, err)
	short = short.WithContext(WithResponseHeaderTimeout(short.Context(), 50*time.Millisecond))
	started := time.Now()
	_, err = client.Do(short)
	require.Error(t, err)
	require.True(t, errors.Is(err, errResponseHeaderTimeout), err)
	require.Less(t, time.Since(started), 5*time.Second)
}

func TestHeaderTimeoutRoundTripperKeepsBodyReadableUntilClose(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		time.Sleep(100 * time.Millisecond)
		_, _ = io.WriteString(w, "late body")
	}))
	t.Cleanup(server.Close)

	client := &http.Client{Transport: &headerTimeoutRoundTripper{base: &http.Transport{}}}
	req, err := http.NewRequest(http.MethodGet, server.URL, nil)
	require.NoError(t, err)
	// Headers arrive in time; the body arrives after the header limit and must still be read.
	req = req.WithContext(WithResponseHeaderTimeout(req.Context(), 50*time.Millisecond))
	resp, err := client.Do(req)
	require.NoError(t, err)
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	require.True(t, strings.HasSuffix(string(body), "late body"))
}

func TestRuntimeHeaderTimeoutFallsBackToEnv(t *testing.T) {
	setHeaderTimeouts(t, "null", "null")
	old := common.RelayResponseHeaderTimeout
	t.Cleanup(func() { common.RelayResponseHeaderTimeout = old })
	common.RelayResponseHeaderTimeout = 42
	require.Equal(t, 42, operation_setting.ResponseHeaderTimeoutSeconds())
	require.Equal(t, 42, operation_setting.NonStreamResponseHeaderTimeoutSeconds())
}
