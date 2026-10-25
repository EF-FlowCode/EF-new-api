package common

import "eflow.local/new-api/constant"

const defaultAnonymousRequestBodyLimitKB = 512

// GetAnonymousRequestBodyLimitBytes returns the route-level limit for
// unauthenticated API requests. A negative value restores the default, while
// zero explicitly disables the limit.
func GetAnonymousRequestBodyLimitBytes() int64 {
	limitKB := constant.AnonymousRequestBodyLimitKB
	if limitKB < 0 {
		limitKB = defaultAnonymousRequestBodyLimitKB
	}
	return int64(limitKB) << 10
}
