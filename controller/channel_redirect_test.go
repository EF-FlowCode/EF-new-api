package controller

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"eflow.local/new-api/common"
	rootdto "eflow.local/new-api/dto"
	"eflow.local/new-api/model"
	"eflow.local/new-api/relaykit/dto"
	"eflow.local/new-api/service"
	"github.com/stretchr/testify/require"
)

// newCredentialRedirectPair starts a target server and an upstream that 302s
// every request to it; the counter records how often the target was hit.
func newCredentialRedirectPair(t *testing.T) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var hits atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"stolen"}]}`))
	}))
	t.Cleanup(target.Close)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/steal", http.StatusFound)
	}))
	t.Cleanup(upstream.Close)
	return upstream, &hits
}

func TestGetResponseBodyDoesNotFollowRedirect(t *testing.T) {
	service.InitHttpClient()
	t.Cleanup(service.ResetProxyClientCache)
	upstream, hits := newCredentialRedirectPair(t)
	channel := &model.Channel{Key: "sk-channel-secret"}

	_, err := GetResponseBody(http.MethodGet, upstream.URL+"/v1/models", channel, GetClaudeAuthHeader(channel.Key))
	require.ErrorContains(t, err, "status code: 302")
	require.Zero(t, hits.Load(), "x-api-key must not be sent to the redirect target")
}

func TestFetchAdvancedCustomManagementBodyDoesNotFollowRedirect(t *testing.T) {
	upstream, hits := newCredentialRedirectPair(t)
	config := &dto.AdvancedCustomConfig{Routes: []dto.AdvancedCustomRoute{
		{IncomingPath: dto.AdvancedCustomModelListPath, UpstreamPath: "/models", Auth: &dto.AdvancedCustomRouteAuth{
			Type: dto.AdvancedCustomAuthTypeHeader, Name: "X-API-Key", Value: "{api_key}",
		}},
	}}
	settings, err := common.Marshal(rootdto.ChannelOtherSettings{AdvancedCustom: config})
	require.NoError(t, err)
	baseURL := upstream.URL
	channel := &model.Channel{BaseURL: &baseURL, Key: "sk-channel-secret", OtherSettings: string(settings)}

	_, err = fetchAdvancedCustomManagementBody(channel, dto.AdvancedCustomModelListPath)
	require.ErrorContains(t, err, "status code: 302")
	require.Zero(t, hits.Load(), "route auth header must not be sent to the redirect target")
}

func TestFetchMidjourneyTasksDoesNotFollowRedirect(t *testing.T) {
	service.InitHttpClient()
	t.Cleanup(service.ResetProxyClientCache)
	upstream, hits := newCredentialRedirectPair(t)
	baseURL := upstream.URL
	channel := &model.Channel{BaseURL: &baseURL, Key: "mj-channel-secret"}

	_, err := fetchMidjourneyTasks(channel, []string{"task-1"})
	require.ErrorContains(t, err, "status code 302")
	require.Zero(t, hits.Load(), "mj-api-secret must not be sent to the redirect target")
}
