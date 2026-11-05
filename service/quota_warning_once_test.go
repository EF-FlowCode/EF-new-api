package service

import (
	"testing"

	"eflow.local/new-api/common"
	"eflow.local/new-api/dto"

	"github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"
)

func TestTokenQuotaWarningLine(t *testing.T) {
	percent := func(v float64) *float64 { return &v }
	cases := []struct {
		name    string
		setting dto.UserSetting
		total   int
		line    int
		ok      bool
	}{
		{"default five percent", dto.UserSetting{}, 1000000, 50000, true},
		{"custom percent", dto.UserSetting{TokenQuotaWarningPercent: percent(20)}, 1000000, 200000, true},
		{"amount above percent wins", dto.UserSetting{TokenQuotaWarningAmount: 80000}, 1000000, 80000, true},
		{"percent above amount wins", dto.UserSetting{TokenQuotaWarningPercent: percent(10), TokenQuotaWarningAmount: 80000}, 1000000, 100000, true},
		{"percent off, amount only", dto.UserSetting{TokenQuotaWarningPercent: percent(0), TokenQuotaWarningAmount: 30000}, 1000000, 30000, true},
		{"both off", dto.UserSetting{TokenQuotaWarningPercent: percent(0)}, 1000000, 0, false},
		{"disabled", dto.UserSetting{TokenQuotaWarningDisabled: true, TokenQuotaWarningAmount: 30000}, 1000000, 0, false},
		{"empty token", dto.UserSetting{}, 0, 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			line, ok := tokenQuotaWarningLine(tc.setting, tc.total)
			if line != tc.line || ok != tc.ok {
				t.Fatalf("line, ok = %d, %v; want %d, %v", line, ok, tc.line, tc.ok)
			}
		})
	}
}

func assertQuotaWarningSequence(t *testing.T) {
	t.Helper()
	key := tokenQuotaWarnedKey(42)
	releaseQuotaWarning(key)
	steps := []struct {
		below bool
		send  bool
	}{
		{false, false}, // above the line: nothing
		{true, true},   // first crossing: send
		{true, false},  // still below: no repeat
		{true, false},
		{false, false}, // topped up: re-armed
		{true, true},   // crosses again: send once more
	}
	for i, step := range steps {
		if got := claimQuotaWarning(key, step.below); got != step.send {
			t.Fatalf("step %d: claim(below=%v) = %v, want %v", i, step.below, got, step.send)
		}
	}
	releaseQuotaWarning(key)
}

func TestClaimQuotaWarningOncePerCrossingMemory(t *testing.T) {
	previous := common.RedisEnabled
	common.RedisEnabled = false
	t.Cleanup(func() { common.RedisEnabled = previous })
	assertQuotaWarningSequence(t)
}

func TestClaimQuotaWarningOncePerCrossingRedis(t *testing.T) {
	server := miniredis.RunT(t)
	previousRDB, previousEnabled := common.RDB, common.RedisEnabled
	common.RDB = redis.NewClient(&redis.Options{Addr: server.Addr()})
	common.RedisEnabled = true
	t.Cleanup(func() {
		_ = common.RDB.Close()
		common.RDB, common.RedisEnabled = previousRDB, previousEnabled
	})
	assertQuotaWarningSequence(t)
	if server.Exists(walletQuotaWarnedKey(7)) {
		t.Fatal("unexpected wallet mark")
	}
	if !claimQuotaWarning(walletQuotaWarnedKey(7), true) || server.TTL(walletQuotaWarnedKey(7)) <= 0 {
		t.Fatal("wallet mark should be set with an expiry")
	}
}
