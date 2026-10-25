package service

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"eflow.local/new-api/constant"
	"eflow.local/new-api/dto"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// newRedirectPair starts a target server and an upstream that 302s every
// request to it; the returned counter records how often the target was hit.
func newRedirectPair(t *testing.T) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var hits atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(target.Close)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/steal", http.StatusFound)
	}))
	t.Cleanup(upstream.Close)
	return upstream, &hits
}

func TestNewRedirectlessHTTPClientKeepsSharedClient(t *testing.T) {
	InitHttpClient()
	t.Cleanup(ResetProxyClientCache)
	upstream, hits := newRedirectPair(t)

	shared, err := GetHttpClientWithProxySettings("", dto.ChannelSettings{})
	require.NoError(t, err)
	redirectless := NewRedirectlessHTTPClient(shared)
	require.NotSame(t, shared, redirectless)
	require.Same(t, shared.Transport, redirectless.Transport)

	req, err := http.NewRequest(http.MethodGet, upstream.URL, nil)
	require.NoError(t, err)
	req.Header.Set("x-goog-api-key", "channel-secret")
	resp, err := redirectless.Do(req)
	require.NoError(t, err)
	resp.Body.Close()

	require.Equal(t, http.StatusFound, resp.StatusCode)
	require.Zero(t, hits.Load())
	require.Equal(t, reflect.ValueOf(checkRedirect).Pointer(), reflect.ValueOf(shared.CheckRedirect).Pointer())
	require.NotNil(t, NewRedirectlessHTTPClient(nil).CheckRedirect)
}

func TestDoMidjourneyHttpRequestDoesNotFollowRedirect(t *testing.T) {
	InitHttpClient()
	t.Cleanup(ResetProxyClientCache)
	upstream, hits := newRedirectPair(t)

	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/mj/submit/imagine", strings.NewReader(`{"prompt":"cat"}`))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set(string(constant.ContextKeyChannelKey), "mj-channel-secret")

	result, _, _ := DoMidjourneyHttpRequest(c, 10*time.Second, upstream.URL+"/mj/submit/imagine")
	require.NotNil(t, result)
	require.Equal(t, http.StatusFound, result.StatusCode)
	require.Zero(t, hits.Load(), "mj-api-secret must not be sent to the redirect target")
}

func TestNewCredentialStrippingRedirectClient(t *testing.T) {
	var crossOriginKey atomic.Value
	crossOriginKey.Store("unset")
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		crossOriginKey.Store(r.Header.Get("x-goog-api-key") + "|" + r.Header.Get("Authorization"))
		w.WriteHeader(http.StatusOK)
	}))
	defer target.Close()
	var sameOriginKey atomic.Value
	var upstream *httptest.Server
	upstream = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/start":
			http.Redirect(w, r, upstream.URL+"/same", http.StatusFound)
		case "/same":
			sameOriginKey.Store(r.Header.Get("x-goog-api-key"))
			http.Redirect(w, r, target.URL+"/media", http.StatusFound)
		}
	}))
	defer upstream.Close()

	var baseCalls atomic.Int32
	base := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		baseCalls.Add(1)
		return nil
	}}
	client := NewCredentialStrippingRedirectClient(base, "Authorization", "x-goog-api-key")

	req, err := http.NewRequest(http.MethodGet, upstream.URL+"/start", nil)
	require.NoError(t, err)
	req.Header.Set("x-goog-api-key", "channel-secret")
	req.Header.Set("Authorization", "Bearer channel-secret")
	resp, err := client.Do(req)
	require.NoError(t, err)
	resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, "channel-secret", sameOriginKey.Load(), "same-origin hops keep credentials")
	require.Equal(t, "|", crossOriginKey.Load(), "cross-origin hop must not receive credentials")
	require.EqualValues(t, 2, baseCalls.Load(), "base CheckRedirect (SSRF validation) still runs on every hop")
}
