package common

import (
	"fmt"
	"strconv"
	"sync"
	"sync/atomic"
)

// UserQuotaSettings is one immutable runtime version of user creation and
// referral rewards. A creation flow should retain one value through commit and
// post-commit rewards.
type UserQuotaSettings struct {
	QuotaForNewUser      int
	QuotaForInviter      int
	QuotaForInvitee      int
	QuotaRemindThreshold int
	PreConsumedQuota     int
	AffiliateEnabled     bool
}

var (
	userQuotaSettings   atomic.Pointer[UserQuotaSettings]
	userQuotaSettingsMu sync.Mutex
)

func init() {
	publishUserQuotaSettings(UserQuotaSettings{
		QuotaRemindThreshold: 1000,
		PreConsumedQuota:     500,
		AffiliateEnabled:     true,
	})
}

// GetUserQuotaSettings returns one immutable runtime configuration version.
func GetUserQuotaSettings() UserQuotaSettings {
	current := userQuotaSettings.Load()
	if current == nil {
		return UserQuotaSettings{}
	}
	return *current
}

// UpdateUserQuotaSettings merges supplied quota options and atomically
// publishes one complete replacement. Unknown keys are ignored.
func UpdateUserQuotaSettings(values map[string]string) error {
	userQuotaSettingsMu.Lock()
	defer userQuotaSettingsMu.Unlock()

	next := GetUserQuotaSettings()
	intOptions := []struct {
		key    string
		target *int
	}{
		{key: "QuotaForNewUser", target: &next.QuotaForNewUser},
		{key: "QuotaForInviter", target: &next.QuotaForInviter},
		{key: "QuotaForInvitee", target: &next.QuotaForInvitee},
		{key: "QuotaRemindThreshold", target: &next.QuotaRemindThreshold},
		{key: "PreConsumedQuota", target: &next.PreConsumedQuota},
	}
	for _, option := range intOptions {
		value, ok := values[option.key]
		if !ok {
			continue
		}
		parsed, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("%s must be an integer: %w", option.key, err)
		}
		*option.target = parsed
	}
	if value, ok := values["AffiliateEnabled"]; ok {
		parsed, err := strconv.ParseBool(value)
		if err != nil {
			return fmt.Errorf("AffiliateEnabled must be a boolean: %w", err)
		}
		next.AffiliateEnabled = parsed
	}

	publishUserQuotaSettings(next)
	return nil
}

func publishUserQuotaSettings(next UserQuotaSettings) {
	userQuotaSettings.Store(&next)
}
