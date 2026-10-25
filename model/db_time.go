package model

import "eflow.local/new-api/common"

// GetDBTimestamp returns a UNIX timestamp from database time.
// Falls back to application time on error.
func GetDBTimestamp() int64 {
	var ts int64
	err := DB.Raw("SELECT UNIX_TIMESTAMP()").Scan(&ts).Error
	if err != nil || ts <= 0 {
		return common.GetTimestamp()
	}
	return ts
}
