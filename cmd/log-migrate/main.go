// log-migrate is an operator tool; it is never called by application startup.
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"eflow.local/new-api/internal/logmigration"
	"eflow.local/new-api/internal/mysqlversion"
	mysql "github.com/go-sql-driver/mysql"
)

func run(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return errors.New("usage: log-migrate plan|run|verify --state /private/online.json; DSN: LOG_MIGRATION_DSN (actual logs database)")
	}
	if runtime.GOOS != "linux" {
		return errors.New("run in Linux/Docker with a persistent checkpoint directory")
	}
	command := args[0]
	if command != "plan" && command != "run" && command != "verify" {
		return errors.New("unknown command")
	}
	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	path := flags.String("state", "", "persistent checkpoint file; plan refuses overwrite")
	report := flags.String("report", "", "new private JSON verification report (verify only)")
	batch := flags.Int("batch", 100, "rows per batch, 1..1000")
	delay := flags.Duration("delay", 200*time.Millisecond, "delay between batches")
	maxBatches := flags.Int("max-batches", 0, "run at most N batches; 0 means until boundary")
	compat := flags.String("compatibility-ref", "", "reference to deployed compatible image and acceptance evidence (run)")
	backup := flags.String("backup-ref", "", "reference to verified pre-migration backup (run)")
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	if flags.NArg() != 0 || *path == "" || *batch < 1 || *batch > 1000 || *delay < 0 || *maxBatches < 0 {
		return errors.New("invalid arguments")
	}
	if command == "verify" && (*report == "" || *report == *path) {
		return errors.New("verify requires a new --report file distinct from --state")
	}
	if command == "run" && (*compat == "" || *backup == "") {
		return errors.New("run requires --compatibility-ref and --backup-ref; these references do not replace release approval or evidence verification")
	}
	// Read before connecting so an unreadable checkpoint cannot trigger writes.
	var state logmigration.State
	var err error
	if command != "plan" {
		state, err = logmigration.ReadState(*path)
		if err != nil {
			return err
		}
	}
	dsn := os.Getenv("LOG_MIGRATION_DSN")
	if dsn == "" {
		return errors.New("set LOG_MIGRATION_DSN for the actual logs database")
	}
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil || cfg.DBName == "" {
		return errors.New("invalid LOG_MIGRATION_DSN (database required)")
	}
	cfg.MultiStatements = false
	cfg.Timeout, cfg.ReadTimeout, cfg.WriteTimeout = 10*time.Second, 30*time.Second, 30*time.Second
	db, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		return errors.New("cannot open migration database")
	}
	defer db.Close()
	db.SetMaxOpenConns(2)
	if err := mysqlversion.Require8Server(ctx, db); err != nil {
		return err
	}
	conn, err := db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	source, err := logmigration.Inspect(ctx, conn)
	if err != nil {
		return err
	}
	unlock, err := logmigration.Lock(ctx, conn, source)
	if err != nil {
		return err
	}
	defer unlock()
	// Reload under the database lock: another process could have advanced the
	// checkpoint between our initial read and acquiring the lock.
	if command != "plan" {
		state, err = logmigration.ReadState(*path)
		if err != nil {
			return err
		}
		if err = logmigration.Validate(ctx, conn, state); err != nil {
			return err
		}
	}
	switch command {
	case "plan":
		state, err = logmigration.Plan(ctx, conn)
		if err == nil {
			err = logmigration.SaveJSON(*path, state, true)
		}
	case "run":
		if (state.CompatibilityRef != "" && state.CompatibilityRef != *compat) || (state.BackupRef != "" && state.BackupRef != *backup) {
			return errors.New("evidence references differ from checkpoint")
		}
		state.CompatibilityRef, state.BackupRef = *compat, *backup
		if err = logmigration.SaveJSON(*path, state, false); err != nil {
			return err
		}
		err = logmigration.Run(ctx, conn, &state, *batch, *delay, *maxBatches, func(s logmigration.State) error {
			if err := logmigration.SaveJSON(*path, s, false); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "cursor=%d upper=%d scanned=%d changed=%d bounded_pass_done=%t\n", s.Cursor, s.UpperID, s.Scanned, s.Changed, s.Done)
			return nil
		})
	case "verify":
		r, scanErr := logmigration.Verify(ctx, conn, state, *batch, *delay)
		if err = logmigration.SaveJSON(*report, r, true); err != nil {
			return err
		}
		if err = json.NewEncoder(os.Stdout).Encode(r); err != nil {
			return err
		}
		if scanErr != nil {
			return scanErr
		}
		if !r.CleanObservation {
			return errors.New("verification found pending/invalid records or a changed upper bound; see report")
		}
		return nil
	}
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(state)
}

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := run(ctx, os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
