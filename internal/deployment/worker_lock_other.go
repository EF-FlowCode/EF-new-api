//go:build !linux

package deployment

import (
	"fmt"
	"os"
)

func LockWorker(string) (*os.File, error) {
	return nil, fmt.Errorf("deployment workers require the Linux shared-volume lock")
}
