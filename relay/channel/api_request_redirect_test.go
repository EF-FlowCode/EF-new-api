package channel

import (
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"eflow.local/new-api/dto"
	relaycommon "eflow.local/new-api/relay/common"
	"eflow.local/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestDoRequestKeepsUpstreamRedirectResponse(t *testing.T) {
	service.InitHttpClient()
	t.Cleanup(service.ResetProxyClientCache)

	var targetHits atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		targetHits.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer target.Close()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/steal", http.StatusFound)
	}))
	defer upstream.Close()

	sharedClient, err := service.GetHttpClientWithProxySettings("", dto.ChannelSettings{})
	require.NoError(t, err)
	require.NotNil(t, sharedClient.CheckRedirect)
	sharedCheckRedirect := reflect.ValueOf(sharedClient.CheckRedirect).Pointer()

	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{}`))
	req, err := http.NewRequest(http.MethodPost, upstream.URL+"/v1/messages", strings.NewReader(`{}`))
	require.NoError(t, err)
	req.Header.Set("x-api-key", "sk-channel-secret")

	resp, err := doRequest(ctx, req, &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{}})
	require.NoError(t, err)
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)

	require.Equal(t, http.StatusFound, resp.StatusCode)
	require.Equal(t, target.URL+"/steal", resp.Header.Get("Location"))
	require.Zero(t, targetHits.Load(), "redirect target must not receive the channel request")

	cachedAgain, err := service.GetHttpClientWithProxySettings("", dto.ChannelSettings{})
	require.NoError(t, err)
	require.Same(t, sharedClient, cachedAgain)
	require.Equal(t, sharedCheckRedirect, reflect.ValueOf(sharedClient.CheckRedirect).Pointer(),
		"shared cached client CheckRedirect must not be mutated")
	require.NotEqual(t, reflect.ValueOf(keepUpstreamRedirectResponse).Pointer(), sharedCheckRedirect)
}
