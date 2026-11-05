package service

import (
	"context"
	"fmt"
	"sync"
	"time"

	"eflow.local/new-api/common"
)

// eflow: quota warnings fire once per crossing. A balance that stays below the
// threshold sends nothing more; rising back above it re-arms the warning.

const quotaWarnedTTL = 30 * 24 * time.Hour

var quotaWarnedStore sync.Map

func walletQuotaWarnedKey(userID int) string {
	return fmt.Sprintf("quota_warned:user:%d", userID)
}

func tokenQuotaWarnedKey(tokenID int) string {
	return fmt.Sprintf("quota_warned:token:%d", tokenID)
}

// claimQuotaWarning reports whether a warning should be sent now. Below the
// threshold only the first caller wins; at or above it the mark is cleared.
func claimQuotaWarning(key string, below bool) bool {
	if !below {
		releaseQuotaWarning(key)
		return false
	}
	if common.RedisEnabled {
		claimed, err := common.RDB.SetNX(context.Background(), key, 1, quotaWarnedTTL).Result()
		if err != nil {
			common.SysLog(fmt.Sprintf("quota warning mark %s unavailable: %v", key, err))
			return false
		}
		return claimed
	}
	_, loaded := quotaWarnedStore.LoadOrStore(key, struct{}{})
	return !loaded
}

// releaseQuotaWarning clears the mark, re-arming the warning.
func releaseQuotaWarning(key string) {
	if common.RedisEnabled {
		if err := common.RDB.Del(context.Background(), key).Err(); err != nil {
			common.SysLog(fmt.Sprintf("failed to clear quota warning mark %s: %v", key, err))
		}
		return
	}
	quotaWarnedStore.Delete(key)
}
