package logmigration

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
)

func ReadState(path string) (State, error) {
	var state State
	f, err := os.Open(path)
	if err != nil {
		return state, err
	}
	defer f.Close()
	d := json.NewDecoder(io.LimitReader(f, 1<<20))
	d.DisallowUnknownFields()
	if err := d.Decode(&state); err != nil {
		return state, err
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return state, errors.New("trailing checkpoint data")
	}
	return state, nil
}

// SaveJSON uses a sibling temporary file, fsync, atomic replacement and directory
// fsync. Run the CLI on Linux with a persistent local filesystem. Checkpoints are
// private operational state, not a substitute for a database backup.
func SaveJSON(path string, value any, create bool) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	f, err := os.CreateTemp(dir, ".log-migration-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if _, err = f.Write(append(data, '\n')); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if create {
		// Link is atomic and refuses to overwrite an existing plan/report.
		err = os.Link(tmp, path)
	} else {
		err = os.Rename(tmp, path)
	}
	if err != nil {
		return err
	}
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}
