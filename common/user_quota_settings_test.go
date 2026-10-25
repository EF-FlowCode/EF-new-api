package common

import (
	"strconv"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func preserveUserQuotaSettings(t *testing.T) {
	t.Helper()
	original := GetUserQuotaSettings()
	t.Cleanup(func() {
		userQuotaSettingsMu.Lock()
		defer userQuotaSettingsMu.Unlock()
		publishUserQuotaSettings(original)
	})
}

func TestUpdateUserQuotaSettingsRejectsInvalidBatchWithoutPublishing(t *testing.T) {
	preserveUserQuotaSettings(t)
	require.NoError(t, UpdateUserQuotaSettings(userQuotaSettingsOptionValues(UserQuotaSettings{
		QuotaForNewUser: 1, QuotaForInviter: 2, QuotaForInvitee: 3,
		QuotaRemindThreshold: 4, PreConsumedQuota: 5, AffiliateEnabled: true,
	})))
	before := GetUserQuotaSettings()

	err := UpdateUserQuotaSettings(map[string]string{
		"QuotaForNewUser": "99",
		"QuotaForInviter": "not-an-integer",
	})

	require.ErrorContains(t, err, "QuotaForInviter")
	assert.Equal(t, before, GetUserQuotaSettings())
}

func TestUserQuotaSettingsConcurrentReadersObserveCompleteVersions(t *testing.T) {
	preserveUserQuotaSettings(t)
	wantA := UserQuotaSettings{
		QuotaForNewUser: 11, QuotaForInviter: 12, QuotaForInvitee: 13,
		QuotaRemindThreshold: 14, PreConsumedQuota: 15, AffiliateEnabled: true,
	}
	wantB := UserQuotaSettings{
		QuotaForNewUser: 21, QuotaForInviter: 22, QuotaForInvitee: 23,
		QuotaRemindThreshold: 24, PreConsumedQuota: 25, AffiliateEnabled: false,
	}
	versionA := userQuotaSettingsOptionValues(wantA)
	versionB := userQuotaSettingsOptionValues(wantB)
	require.NoError(t, UpdateUserQuotaSettings(versionA))

	invalid := make(chan UserQuotaSettings, 1)
	var writers sync.WaitGroup
	for _, values := range []map[string]string{versionA, versionB} {
		writers.Add(1)
		go func() {
			defer writers.Done()
			for range 1000 {
				if err := UpdateUserQuotaSettings(values); err != nil {
					return
				}
			}
		}()
	}

	for range 5000 {
		got := GetUserQuotaSettings()
		if got != wantA && got != wantB {
			select {
			case invalid <- got:
			default:
			}
			break
		}
	}
	writers.Wait()
	select {
	case got := <-invalid:
		t.Fatalf("reader observed mixed user quota settings: %#v", got)
	default:
	}
}

func userQuotaSettingsOptionValues(settings UserQuotaSettings) map[string]string {
	return map[string]string{
		"QuotaForNewUser":      strconv.Itoa(settings.QuotaForNewUser),
		"QuotaForInviter":      strconv.Itoa(settings.QuotaForInviter),
		"QuotaForInvitee":      strconv.Itoa(settings.QuotaForInvitee),
		"QuotaRemindThreshold": strconv.Itoa(settings.QuotaRemindThreshold),
		"PreConsumedQuota":     strconv.Itoa(settings.PreConsumedQuota),
		"AffiliateEnabled":     strconv.FormatBool(settings.AffiliateEnabled),
	}
}
