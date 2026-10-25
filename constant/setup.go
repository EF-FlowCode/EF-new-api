package constant

import "sync/atomic"

var setupComplete atomic.Bool

func IsSetupComplete() bool {
	return setupComplete.Load()
}

func SetSetupComplete(value bool) {
	setupComplete.Store(value)
}
