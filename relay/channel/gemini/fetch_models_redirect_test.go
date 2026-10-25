package gemini

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"eflow.local/new-api/service"
	"github.com/stretchr/testify/require"
)

func TestFetchGeminiModelsDoesNotFollowRedirect(t *testing.T) {
	service.InitHttpClient()
	t.Cleanup(service.ResetProxyClientCache)

	var hits atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"models":[{"name":"models/stolen"}]}`))
	}))
	defer target.Close()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/steal", http.StatusFound)
	}))
	defer upstream.Close()

	_, err := FetchGeminiModels(upstream.URL, "gemini-channel-secret", "")
	require.Error(t, err)
	require.Contains(t, err.Error(), "302")
	require.Zero(t, hits.Load(), "x-goog-api-key must not be sent to the redirect target")
}
