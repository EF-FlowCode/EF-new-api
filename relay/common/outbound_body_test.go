package common

import (
	"io"
	"testing"

	rootcommon "eflow.local/new-api/common"
	"github.com/stretchr/testify/require"
)

func TestNewOutboundJSONBodyReplaysIndependentReaders(t *testing.T) {
	payload := []byte(`{"model":"test-model","input":"abcdefghijklmnopqrstuvwxyz"}`)
	body, closer, err := NewOutboundJSONBody(payload)
	require.NoError(t, err)
	defer closer.Close()

	primaryHead := make([]byte, 8)
	_, err = io.ReadFull(body, primaryHead)
	require.NoError(t, err)
	require.Equal(t, payload[:8], primaryHead)

	a, err := body.NewReader()
	require.NoError(t, err)
	b, err := body.NewReader()
	require.NoError(t, err)

	aHead := make([]byte, 5)
	_, err = io.ReadFull(a, aHead)
	require.NoError(t, err)
	require.Equal(t, payload[:5], aHead)

	bAll, err := io.ReadAll(b)
	require.NoError(t, err)
	require.NoError(t, b.Close())
	require.Equal(t, payload, bAll)

	aRest, err := io.ReadAll(a)
	require.NoError(t, err)
	require.NoError(t, a.Close())
	require.Equal(t, payload[5:], aRest)

	primaryRest, err := io.ReadAll(body)
	require.NoError(t, err)
	require.Equal(t, payload[8:], primaryRest)

	require.NoError(t, closer.Close())
	_, err = body.NewReader()
	require.ErrorIs(t, err, rootcommon.ErrStorageClosed)
}

func TestNewOutboundJSONBodyDiskStorageReplays(t *testing.T) {
	previous := rootcommon.GetDiskCacheConfig()
	rootcommon.SetDiskCacheConfig(rootcommon.DiskCacheConfig{
		Enabled:     true,
		ThresholdMB: 0,
		MaxSizeMB:   16,
		Path:        t.TempDir(),
	})
	defer rootcommon.SetDiskCacheConfig(previous)

	payload := []byte(`{"model":"disk-model","input":"replay"}`)
	body, closer, err := NewOutboundJSONBody(payload)
	require.NoError(t, err)
	defer closer.Close()

	storage, ok := closer.(rootcommon.BodyStorage)
	require.True(t, ok)
	require.True(t, storage.IsDisk())
	replay, err := body.NewReader()
	require.NoError(t, err)
	got, err := io.ReadAll(replay)
	require.NoError(t, err)
	require.NoError(t, replay.Close())
	require.Equal(t, payload, got)
}
