package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"eflow.local/new-api/internal/logmigration"
	"eflow.local/new-api/internal/testdb"
	mysql "github.com/go-sql-driver/mysql"
	"github.com/stretchr/testify/require"
)

func TestCLIPlanResumeAndVerify(t *testing.T) {
	gormDB := testdb.OpenMySQL8(t)
	db, err := gormDB.DB()
	require.NoError(t, err)
	_, err = db.Exec("CREATE TABLE logs (id BIGINT PRIMARY KEY AUTO_INCREMENT, other TEXT) ENGINE=InnoDB")
	require.NoError(t, err)
	_, err = db.Exec("INSERT INTO logs(other) VALUES (?), (?)", `{"channel_id":1}`, `{"reject_reason":"private"}`)
	require.NoError(t, err)
	cfg, err := mysql.ParseDSN(os.Getenv("TEST_MYSQL_DSN"))
	require.NoError(t, err)
	require.NoError(t, db.QueryRow("SELECT DATABASE()").Scan(&cfg.DBName))
	t.Setenv("LOG_MIGRATION_DSN", cfg.FormatDSN())
	ctx := context.Background()
	statePath := filepath.Join(t.TempDir(), "online.json")
	require.NoError(t, run(ctx, []string{"plan", "--state", statePath}))
	require.Error(t, run(ctx, []string{"plan", "--state", statePath}))
	args := []string{"run", "--state", statePath, "--batch", "1", "--delay", "0s"}
	require.ErrorContains(t, run(ctx, args), "requires --compatibility-ref")
	args = append(args, "--compatibility-ref", "local-test-compatible-image", "--backup-ref", "local-test-backup")
	require.NoError(t, run(ctx, append(args, "--max-batches", "1")))
	state, err := logmigration.ReadState(statePath)
	require.NoError(t, err)
	require.Equal(t, int64(1), state.Cursor)
	require.False(t, state.Done)
	require.ErrorContains(t, run(ctx, append(args, "--backup-ref", "different")), "references differ")
	reportPath := filepath.Join(t.TempDir(), "pending.json")
	require.ErrorContains(t, run(ctx, []string{"verify", "--state", statePath, "--report", reportPath}), "pending/invalid")
	require.NoError(t, run(ctx, args))
	require.NoError(t, run(ctx, []string{"verify", "--state", statePath, "--report", filepath.Join(t.TempDir(), "clean.json")}))
	// Verify never overwrites an existing report, including an aliased state path.
	require.Error(t, run(ctx, []string{"verify", "--state", statePath, "--report", reportPath}))
	state, err = logmigration.ReadState(statePath)
	require.NoError(t, err)
	require.True(t, state.Done)
	require.Equal(t, int64(2), state.Changed)
}
