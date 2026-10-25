package logmigration

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"

	"eflow.local/new-api/internal/logmetadata"
	"eflow.local/new-api/internal/testdb"
	"github.com/stretchr/testify/require"
)

func database(t *testing.T) (*sql.DB, *sql.Conn) {
	t.Helper()
	gormDB := testdb.OpenMySQL8(t)
	db, err := gormDB.DB()
	require.NoError(t, err)
	_, err = db.Exec("CREATE TABLE logs (id BIGINT PRIMARY KEY AUTO_INCREMENT, other LONGTEXT NULL, quota BIGINT NOT NULL DEFAULT 99, prompt_tokens BIGINT NOT NULL DEFAULT 9007199254740993, created_at BIGINT NOT NULL DEFAULT 1700000000) ENGINE=InnoDB")
	require.NoError(t, err)
	conn, err := db.Conn(context.Background())
	require.NoError(t, err)
	t.Cleanup(func() { conn.Close() })
	return db, conn
}

func insert(t *testing.T, db *sql.DB, id int64, value any) {
	t.Helper()
	_, err := db.Exec("INSERT INTO logs(id,other) VALUES (?,?)", id, value)
	require.NoError(t, err)
}

func checkpoint(t *testing.T, state State) (string, func(State) error) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "checkpoint.json")
	require.NoError(t, SaveJSON(path, state, true))
	return path, func(s State) error { return SaveJSON(path, s, false) }
}

func TestBackfillResumeLateCommitAndFinalSweep(t *testing.T) {
	ctx := context.Background()
	db, conn := database(t)
	insert(t, db, 1, `{"channel_id":1,"reasoning_effort":"high","n":9007199254740993}`)
	// Reserve an ID in an uncommitted writer transaction, then commit a higher
	// ID on another connection so MAX(id) alone cannot detect the late commit.
	tx, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer tx.Rollback()
	_, err = tx.Exec("INSERT INTO logs(id,other) VALUES (2,?)", `{"reject_reason":"late"}`)
	require.NoError(t, err)
	insert(t, db, 3, `{"channel_name":"three"}`)
	state, err := Plan(ctx, conn)
	require.NoError(t, err)
	require.Equal(t, int64(3), state.UpperID)
	unlock, err := Lock(ctx, conn, state.Source)
	require.NoError(t, err)
	defer unlock()
	path, save := checkpoint(t, state)
	require.NoError(t, Run(ctx, conn, &state, 1, 0, 1, save))
	require.Equal(t, int64(1), state.Cursor)
	state, err = ReadState(path)
	require.NoError(t, err)
	require.NoError(t, Run(ctx, conn, &state, 1, 0, 0, save))
	require.True(t, state.Done)
	require.NoError(t, tx.Commit())
	insert(t, db, 4, `{"channel_type":4}`)
	insert(t, db, 5, nil)
	report, err := Verify(ctx, conn, state, 2, 0)
	require.NoError(t, err)
	require.False(t, report.CleanObservation)
	require.Equal(t, int64(2), report.Pending)
	require.Equal(t, []Issue{{2, "legacy private fields"}, {4, "legacy private fields"}}, report.Samples)
	// Stopped-writer sweep: a NEW plan starts from zero with a fresh boundary.
	final, err := Plan(ctx, conn)
	require.NoError(t, err)
	_, finalSave := checkpoint(t, final)
	require.NoError(t, Run(ctx, conn, &final, 2, 0, 0, finalSave))
	require.Equal(t, int64(2), final.Changed)
	report, err = Verify(ctx, conn, final, 2, 0)
	require.NoError(t, err)
	require.True(t, report.CleanObservation)
	var untouched int
	require.NoError(t, db.QueryRow("SELECT COUNT(*) FROM logs WHERE quota=99 AND prompt_tokens=9007199254740993 AND created_at=1700000000").Scan(&untouched))
	require.Equal(t, 5, untouched)
	var first string
	require.NoError(t, db.QueryRow("SELECT other FROM logs WHERE id=1").Scan(&first))
	require.Contains(t, first, `"n":9007199254740993`)
	require.Contains(t, first, `"reasoning_effort":"high"`)
	var nullOther sql.NullString
	require.NoError(t, db.QueryRow("SELECT other FROM logs WHERE id=5").Scan(&nullOther))
	require.False(t, nullOther.Valid)
	// Repeating a completed checkpoint and a fresh full sweep are both no-ops.
	require.NoError(t, Run(ctx, conn, &final, 2, 0, 0, finalSave))
	again, err := Plan(ctx, conn)
	require.NoError(t, err)
	require.NoError(t, Run(ctx, conn, &again, 2, 0, 0, func(State) error { return nil }))
	require.Zero(t, again.Changed)
}

func TestCheckpointFailureReplaysCommittedRows(t *testing.T) {
	ctx := context.Background()
	db, conn := database(t)
	insert(t, db, 1, `{"reject_reason":"one"}`)
	state, err := Plan(ctx, conn)
	require.NoError(t, err)
	path, save := checkpoint(t, state)
	err = Run(ctx, conn, &state, 1, 0, 0, func(State) error { return errors.New("simulated disk failure") })
	require.Error(t, err)
	require.Zero(t, state.Cursor)
	state, err = ReadState(path)
	require.NoError(t, err)
	require.Zero(t, state.Cursor)
	var actual string
	require.NoError(t, db.QueryRow("SELECT other FROM logs WHERE id=1").Scan(&actual))
	require.JSONEq(t, `{"admin_info":{"reject_reason":"one"}}`, actual)
	require.NoError(t, Run(ctx, conn, &state, 1, 0, 0, save))
	require.True(t, state.Done)
	require.Zero(t, state.Changed) // committed before failure, not an accounting counter
	require.Error(t, SaveJSON(path, state, true))
	loaded, err := ReadState(path)
	require.NoError(t, err)
	require.Equal(t, state, loaded)
}

func TestAmbiguousRowsStopWithoutOverwritingOrAdvancing(t *testing.T) {
	ctx := context.Background()
	db, conn := database(t)
	inputs := []string{`{"channel_id":1,"admin_info":{"channel_id":2}}`, `{"x":`, `{"admin_info":null}`, `{"x":1,"x":2}`}
	for i, input := range inputs {
		insert(t, db, int64(i+1), input)
	}
	state, err := Plan(ctx, conn)
	require.NoError(t, err)
	_, save := checkpoint(t, state)
	require.ErrorContains(t, Run(ctx, conn, &state, 4, 0, 0, save), "log 1")
	require.Zero(t, state.Cursor)
	for i, input := range inputs {
		var got string
		require.NoError(t, db.QueryRow("SELECT other FROM logs WHERE id=?", i+1).Scan(&got))
		require.Equal(t, input, got)
	}
	report, err := Verify(ctx, conn, state, 2, 0)
	require.NoError(t, err)
	require.Equal(t, int64(4), report.Invalid)
	require.False(t, report.CleanObservation)
}

func TestConcurrentModificationUsesByteExactCAS(t *testing.T) {
	ctx := context.Background()
	db, conn := database(t)
	old := `{"reject_reason":"A"}`
	insert(t, db, 1, old)
	batch, err := readBatch(ctx, conn, 0, 1, 1)
	require.NoError(t, err)
	normalized, err := logmetadata.Normalize(old)
	require.NoError(t, err)
	for _, concurrent := range []string{`{"reject_reason":"a"}`, old + " "} {
		_, err = db.Exec("UPDATE logs SET other=? WHERE id=1", concurrent)
		require.NoError(t, err)
		require.ErrorContains(t, update(ctx, conn, batch[0], normalized), "concurrent")
		var actual string
		require.NoError(t, db.QueryRow("SELECT other FROM logs WHERE id=1").Scan(&actual))
		require.Equal(t, concurrent, actual)
	}
}

func TestIdentitySchemaLockAndCancellation(t *testing.T) {
	ctx := context.Background()
	db, conn := database(t)
	insert(t, db, 1, `{"channel_id":1}`)
	state, err := Plan(ctx, conn)
	require.NoError(t, err)
	_, otherConn := database(t)
	require.ErrorContains(t, Validate(ctx, otherConn, state), "differs")
	wrongSource := state
	wrongSource.Source.ServerUUID = "different-server"
	require.ErrorContains(t, Validate(ctx, conn, wrongSource), "differs")
	unlock, err := Lock(ctx, conn, state.Source)
	require.NoError(t, err)
	defer unlock()
	second, err := db.Conn(ctx)
	require.NoError(t, err)
	defer second.Close()
	_, err = Lock(ctx, second, state.Source)
	require.ErrorContains(t, err, "another log migration")
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	require.Error(t, Run(canceled, conn, &state, 1, 0, 0, func(State) error { return nil }))
	require.Zero(t, state.Cursor)
	_, err = db.Exec("ALTER TABLE logs ADD COLUMN new_field INT NULL")
	require.NoError(t, err)
	require.ErrorContains(t, Validate(ctx, conn, state), "schema differs")
}

func TestStrictStorageAndTriggerGuard(t *testing.T) {
	ctx := context.Background()
	db, conn := database(t)
	_, err := db.Exec("ALTER TABLE logs MODIFY other VARCHAR(20)")
	require.NoError(t, err)
	insert(t, db, 1, `{"channel_id":1}`)
	state, err := Plan(ctx, conn)
	require.NoError(t, err)
	_, err = conn.ExecContext(ctx, "SET SESSION sql_mode=''")
	require.NoError(t, err)
	unlock, err := Lock(ctx, conn, state.Source)
	require.NoError(t, err)
	defer unlock()
	err = Run(ctx, conn, &state, 1, 0, 0, func(State) error { return nil })
	require.ErrorContains(t, err, "database update failed")
	var original string
	require.NoError(t, db.QueryRow("SELECT other FROM logs WHERE id=1").Scan(&original))
	require.Equal(t, `{"channel_id":1}`, original)
	require.Zero(t, state.Cursor)
	_, err = db.Exec("CREATE TRIGGER logs_side_effect BEFORE UPDATE ON logs FOR EACH ROW SET NEW.quota=0")
	require.NoError(t, err)
	_, err = Plan(ctx, conn)
	require.ErrorContains(t, err, "triggers")
}
