// Package logmigration performs resumable, bounded MySQL log metadata backfills.
// It does not certify deployment compatibility, backups, or stopped writers.
package logmigration

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"time"

	"eflow.local/new-api/internal/logmetadata"
)

type Identity struct {
	ServerUUID   string `json:"server_uuid"`
	Database     string `json:"database"`
	Version      string `json:"mysql_version"`
	SchemaSHA256 string `json:"logs_schema_sha256"`
}

type State struct {
	Format     int      `json:"format"`
	Source     Identity `json:"source"`
	CapturedAt string   `json:"captured_at_utc"`
	UpperID    int64    `json:"upper_id"`
	Cursor     int64    `json:"cursor"`
	Scanned    int64    `json:"scanned"`
	Changed    int64    `json:"changed"`
	Done       bool     `json:"bounded_pass_done"`
	// These are operator evidence references, never an automatic release gate.
	CompatibilityRef string `json:"compatibility_ref,omitempty"`
	BackupRef        string `json:"backup_ref,omitempty"`
}

type Issue struct {
	ID     int64  `json:"id"`
	Reason string `json:"reason"`
}

func (i *Issue) Error() string { return fmt.Sprintf("log %d: %s", i.ID, i.Reason) }

type Report struct {
	Source       Identity `json:"source"`
	StartedAt    string   `json:"started_at_utc"`
	FinishedAt   string   `json:"finished_at_utc"`
	UpperID      int64    `json:"upper_id"`
	EndUpperID   int64    `json:"end_upper_id"`
	Scanned      int64    `json:"scanned"`
	Pending      int64    `json:"pending"`
	Invalid      int64    `json:"invalid"`
	Samples      []Issue  `json:"issue_samples"`
	ScanComplete bool     `json:"scan_complete"`
	// A clean bounded observation is not proof that all writers have stopped.
	CleanObservation bool `json:"clean_observation"`
}

var autoIncrement = regexp.MustCompile(` AUTO_INCREMENT=[0-9]+`)

func Inspect(ctx context.Context, conn *sql.Conn) (Identity, error) {
	var identity Identity
	if err := conn.QueryRowContext(ctx, "SELECT @@server_uuid, DATABASE(), VERSION()").Scan(&identity.ServerUUID, &identity.Database, &identity.Version); err != nil {
		return identity, err
	}
	var table, ddl, engine string
	if err := conn.QueryRowContext(ctx, "SHOW CREATE TABLE logs").Scan(&table, &ddl); err != nil {
		return identity, err
	}
	if err := conn.QueryRowContext(ctx, "SELECT ENGINE FROM information_schema.tables WHERE table_schema=DATABASE() AND table_name='logs'").Scan(&engine); err != nil {
		return identity, err
	}
	if engine != "InnoDB" {
		return identity, errors.New("logs must use InnoDB")
	}
	var triggers, invalidIDs int
	if err := conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM information_schema.triggers WHERE trigger_schema=DATABASE() AND event_object_table='logs'").Scan(&triggers); err != nil {
		return identity, err
	}
	if triggers != 0 {
		return identity, errors.New("logs has triggers; review side effects before migration")
	}
	if err := conn.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM logs WHERE id<=0 LIMIT 1)").Scan(&invalidIDs); err != nil {
		return identity, err
	}
	if invalidIDs != 0 {
		return identity, errors.New("logs contains nonpositive IDs")
	}
	sum := sha256.Sum256([]byte(autoIncrement.ReplaceAllString(ddl, "")))
	identity.SchemaSHA256 = hex.EncodeToString(sum[:])
	return identity, nil
}

// Lock serializes migration tools for this database on a dedicated connection.
// Application writers are protected separately by byte-exact compare-and-swap.
func Lock(ctx context.Context, conn *sql.Conn, source Identity) (func(), error) {
	sum := sha256.Sum256([]byte(source.Database))
	name := "log-other:" + hex.EncodeToString(sum[:24])
	var acquired sql.NullInt64
	if err := conn.QueryRowContext(ctx, "SELECT GET_LOCK(?, 0)", name).Scan(&acquired); err != nil {
		return nil, err
	}
	if !acquired.Valid || acquired.Int64 != 1 {
		return nil, errors.New("another log migration is running")
	}
	if _, err := conn.ExecContext(ctx, "SET SESSION innodb_lock_wait_timeout=3"); err != nil {
		conn.Close()
		return nil, err
	}
	if _, err := conn.ExecContext(ctx, "SET SESSION autocommit=1, sql_mode='STRICT_ALL_TABLES,NO_ENGINE_SUBSTITUTION'"); err != nil {
		conn.Close()
		return nil, err
	}
	return func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = conn.ExecContext(cleanup, "DO RELEASE_LOCK(?)", name)
	}, nil
}

func upperID(ctx context.Context, conn *sql.Conn) (int64, error) {
	var max int64
	err := conn.QueryRowContext(ctx, "SELECT COALESCE(MAX(id),0) FROM logs").Scan(&max)
	return max, err
}

func Plan(ctx context.Context, conn *sql.Conn) (State, error) {
	source, err := Inspect(ctx, conn)
	if err != nil {
		return State{}, err
	}
	upper, err := upperID(ctx, conn)
	return State{Format: 1, Source: source, UpperID: upper, CapturedAt: time.Now().UTC().Format(time.RFC3339Nano)}, err
}

func Validate(ctx context.Context, conn *sql.Conn, state State) error {
	if state.Format != 1 || state.Cursor < 0 || state.UpperID < state.Cursor || state.Scanned < 0 || state.Changed < 0 || state.Changed > state.Scanned || (state.Done && state.Cursor != state.UpperID) {
		return errors.New("invalid checkpoint")
	}
	if _, err := time.Parse(time.RFC3339Nano, state.CapturedAt); err != nil {
		return errors.New("invalid checkpoint timestamp")
	}
	source, err := Inspect(ctx, conn)
	if err != nil {
		return err
	}
	if source != state.Source {
		return errors.New("server, database, version or logs schema differs from checkpoint; create a reviewed new plan")
	}
	return nil
}

type row struct {
	id    int64
	other sql.NullString
}

func readBatch(ctx context.Context, conn *sql.Conn, cursor, upper int64, limit int) ([]row, error) {
	rows, err := conn.QueryContext(ctx, "SELECT id, other FROM logs WHERE id > ? AND id <= ? ORDER BY id LIMIT ?", cursor, upper, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []row{}
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.id, &r.other); err != nil {
			return nil, err
		}
		result = append(result, r)
	}
	return result, rows.Err()
}

func update(ctx context.Context, conn *sql.Conn, r row, value string) error {
	// BINARY avoids the column collation accepting case or trailing-space changes.
	result, err := conn.ExecContext(ctx, "UPDATE logs SET other=? WHERE id=? AND CAST(other AS BINARY)=CAST(? AS BINARY)", value, r.id, r.other.String)
	if err != nil {
		return &Issue{r.id, "database update failed; retry after checking connectivity, locks and column capacity"}
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return &Issue{r.id, "concurrent change or deletion; reread on resume"}
	}
	return nil
}

// Run saves only after an entire batch succeeds. If a crash happens between a
// committed UPDATE and the checkpoint, replay is safe because Normalize is
// idempotent. Counters are operational, not exact accounting after such a crash.
func Run(ctx context.Context, conn *sql.Conn, state *State, batch int, delay time.Duration, maxBatches int, save func(State) error) error {
	if batch < 1 || batch > 1000 || delay < 0 || maxBatches < 0 {
		return errors.New("invalid batch, delay or max-batches")
	}
	if err := Validate(ctx, conn, *state); err != nil {
		return err
	}
	for completed := 0; !state.Done && (maxBatches == 0 || completed < maxBatches); completed++ {
		rows, err := readBatch(ctx, conn, state.Cursor, state.UpperID, batch)
		if err != nil {
			return err
		}
		next := *state
		for _, r := range rows {
			value, err := logmetadata.Normalize(r.other.String)
			if err != nil {
				return &Issue{r.id, err.Error()}
			}
			if value != r.other.String {
				if err = update(ctx, conn, r, value); err != nil {
					return err
				}
				next.Changed++
			}
			next.Scanned++
			next.Cursor = r.id
		}
		if len(rows) < batch || next.Cursor == next.UpperID {
			next.Cursor, next.Done = next.UpperID, true
		}
		if err = save(next); err != nil {
			return err
		}
		*state = next
		if !state.Done && delay > 0 {
			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-timer.C:
			}
		}
	}
	return nil
}

// Verify always scans from zero through a fresh upper bound, including IDs
// committed late below an earlier cursor. It never writes the database.
func Verify(ctx context.Context, conn *sql.Conn, state State, batch int, delay time.Duration) (Report, error) {
	r := Report{Source: state.Source, StartedAt: time.Now().UTC().Format(time.RFC3339Nano), Samples: []Issue{}}
	if batch < 1 || batch > 1000 || delay < 0 {
		return r, errors.New("invalid batch or delay")
	}
	if err := Validate(ctx, conn, state); err != nil {
		return r, err
	}
	var err error
	r.UpperID, err = upperID(ctx, conn)
	if err != nil {
		return r, err
	}
	var cursor int64
	for {
		rows, err := readBatch(ctx, conn, cursor, r.UpperID, batch)
		if err != nil {
			return r, err
		}
		for _, row := range rows {
			r.Scanned++
			value, err := logmetadata.Normalize(row.other.String)
			reason := ""
			if err != nil {
				r.Invalid++
				reason = err.Error()
			} else if value != row.other.String {
				r.Pending++
				reason = "legacy private fields"
			}
			if reason != "" && len(r.Samples) < 100 {
				r.Samples = append(r.Samples, Issue{row.id, reason})
			}
			cursor = row.id
		}
		if len(rows) < batch || cursor == r.UpperID {
			break
		}
		if delay > 0 {
			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return r, ctx.Err()
			case <-timer.C:
			}
		}
	}
	r.EndUpperID, err = upperID(ctx, conn)
	if err != nil {
		return r, err
	}
	if err := Validate(ctx, conn, state); err != nil {
		return r, err
	}
	r.ScanComplete = true
	r.CleanObservation = r.Invalid == 0 && r.Pending == 0 && r.UpperID == r.EndUpperID
	r.FinishedAt = time.Now().UTC().Format(time.RFC3339Nano)
	return r, nil
}
