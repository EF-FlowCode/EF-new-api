//go:build linux

package deployment

import (
	"fmt"
	"os"
	"syscall"
)

// A kernel lock on the shared data mount cannot expire during a stalled job.
// The descriptor is held until process exit; all workers must share this mount.
func LockWorker(path string) (*os.File, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		file.Close()
		return nil, fmt.Errorf("another worker owns the shared deployment lock")
	}
	return file, nil
}
