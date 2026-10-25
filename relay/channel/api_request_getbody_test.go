package channel

import (
	"bytes"
	"io"
	"net/http"
	"testing"

	rootcommon "eflow.local/new-api/common"
	relaycommon "eflow.local/new-api/relay/common"
	"github.com/stretchr/testify/require"
)

func TestApplyUpstreamBodyMetadataSetsReplayBody(t *testing.T) {
	payload := []byte(`{"model":"test-model","input":"retry"}`)
	body, closer, err := relaycommon.NewOutboundJSONBody(payload)
	require.NoError(t, err)
	defer closer.Close()

	req, err := http.NewRequest(http.MethodPost, "https://example.com/v1/chat/completions", body)
	require.NoError(t, err)
	require.Nil(t, req.GetBody)
	require.Zero(t, req.ContentLength)

	ApplyUpstreamBodyMetadata(req, body)
	require.EqualValues(t, len(payload), req.ContentLength)
	require.NotNil(t, req.GetBody)

	sent, err := io.ReadAll(req.Body)
	require.NoError(t, err)
	require.Equal(t, payload, sent)

	for range 2 {
		replay, err := req.GetBody()
		require.NoError(t, err)
		got, err := io.ReadAll(replay)
		require.NoError(t, err)
		require.NoError(t, replay.Close())
		require.Equal(t, payload, got)
	}
}

func TestApplyUpstreamBodyMetadataPreservesNativeGetBody(t *testing.T) {
	payload := []byte("native body")
	body := bytes.NewReader(payload)
	req, err := http.NewRequest(http.MethodPost, "https://example.com", body)
	require.NoError(t, err)
	originalGetBody := req.GetBody

	ApplyUpstreamBodyMetadata(req, body)
	require.NotNil(t, req.GetBody)
	require.NotNil(t, originalGetBody)
	replay, err := req.GetBody()
	require.NoError(t, err)
	got, err := io.ReadAll(replay)
	require.NoError(t, err)
	require.NoError(t, replay.Close())
	require.Equal(t, payload, got)
}

func TestApplyUpstreamBodyMetadataHidesStorageCloser(t *testing.T) {
	payload := []byte("storage body")
	storage, err := rootcommon.CreateBodyStorage(payload)
	require.NoError(t, err)
	defer storage.Close()

	req, err := http.NewRequest(http.MethodPost, "https://example.com", storage)
	require.NoError(t, err)
	ApplyUpstreamBodyMetadata(req, storage)

	_, exposesStorage := req.Body.(rootcommon.BodyStorage)
	require.False(t, exposesStorage)
	require.NotNil(t, req.GetBody)
	require.NoError(t, req.Body.Close())

	replay, err := req.GetBody()
	require.NoError(t, err)
	got, err := io.ReadAll(replay)
	require.NoError(t, err)
	require.NoError(t, replay.Close())
	require.Equal(t, payload, got)
}
