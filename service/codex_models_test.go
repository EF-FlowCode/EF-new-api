package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFetchLatestCodexClientVersion(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "new-api", r.Header.Get("User-Agent"))
		_, _ = w.Write([]byte(`{"name":"v1.2.3"}`))
	}))
	t.Cleanup(server.Close)

	version, err := fetchLatestCodexClientVersion(context.Background(), server.Client(), server.URL)
	require.NoError(t, err)
	require.Equal(t, "v1.2.3", version)
}

func TestFetchCodexModels(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/backend-api/codex/models", r.URL.Path)
		require.Equal(t, "v1.2.3", r.URL.Query().Get("client_version"))
		require.Equal(t, "Bearer access-token", r.Header.Get("Authorization"))
		require.Equal(t, "account-id", r.Header.Get("ChatGPT-Account-Id"))
		_, _ = w.Write([]byte(`{"models":[{"slug":"gpt-test"},{"slug":"gpt-test"},{"slug":""}]}`))
	}))
	t.Cleanup(server.Close)

	status, models, err := FetchCodexModels(context.Background(), server.Client(), server.URL, &CodexOAuthKey{
		AccessToken: "access-token",
		AccountID:   "account-id",
	}, "v1.2.3")
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, status)
	require.Equal(t, []string{"gpt-test"}, models)
}
