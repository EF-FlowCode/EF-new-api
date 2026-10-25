package common

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCleanupOldDiskCacheFilesKeepsFilesStillInUse(t *testing.T) {
	previous := GetDiskCacheConfig()
	config := previous
	config.Path = t.TempDir()
	SetDiskCacheConfig(config)
	t.Cleanup(func() { SetDiskCacheConfig(previous) })

	storage, err := newDiskStorage([]byte("in-flight body"), "")
	require.NoError(t, err)
	t.Cleanup(func() { _ = storage.Close() })
	leftover := filepath.Join(GetDiskCacheDir(), "body-leftover.tmp")
	require.NoError(t, os.WriteFile(leftover, []byte("crashed"), 0600))

	old := time.Now().Add(-time.Hour)
	require.NoError(t, os.Chtimes(storage.filePath, old, old))
	require.NoError(t, os.Chtimes(leftover, old, old))
	before := GetDiskCacheStats().CurrentDiskUsageBytes

	require.NoError(t, CleanupOldDiskCacheFiles(10*time.Minute))

	require.FileExists(t, storage.filePath, "an in-flight request body must survive the cleanup")
	require.NoFileExists(t, leftover)
	require.Equal(t, before, GetDiskCacheStats().CurrentDiskUsageBytes)
	reader, err := storage.NewReader()
	require.NoError(t, err, "retries reopen the body file")
	require.NoError(t, reader.Close())

	require.NoError(t, storage.Close())
	require.NoFileExists(t, storage.filePath)
	_, live := liveDiskCacheFiles.Load(storage.filePath)
	require.False(t, live)
}
