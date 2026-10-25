package common

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/require"
)

func useVerificationMemory(t *testing.T) {
	t.Helper()
	previous := RedisEnabled
	RedisEnabled = false
	t.Cleanup(func() { RedisEnabled = previous })
}

func requireVerification(t *testing.T, key, code, purpose string, expected bool) {
	t.Helper()
	valid, err := VerifyCodeWithKey(key, code, purpose)
	require.NoError(t, err)
	require.Equal(t, expected, valid)
}

func TestRegisterVerificationCodeEvictsOldestWhenCapacityStillExceeded(t *testing.T) {
	useVerificationMemory(t)
	verificationMutex.Lock()
	oldMap, oldMax := verificationMap, verificationMapMaxSize
	verificationMap = make(map[string]verificationValue)
	verificationMapMaxSize = 2
	verificationMutex.Unlock()
	t.Cleanup(func() {
		verificationMutex.Lock()
		verificationMap = oldMap
		verificationMapMaxSize = oldMax
		verificationMutex.Unlock()
	})

	require.NoError(t, RegisterVerificationCodeWithKey("old", "1", EmailVerificationPurpose))
	time.Sleep(time.Millisecond)
	require.NoError(t, RegisterVerificationCodeWithKey("newer", "2", EmailVerificationPurpose))
	time.Sleep(time.Millisecond)
	require.NoError(t, RegisterVerificationCodeWithKey("newest", "3", EmailVerificationPurpose))

	requireVerification(t, "old", "1", EmailVerificationPurpose, false)
	requireVerification(t, "newer", "2", EmailVerificationPurpose, true)
	requireVerification(t, "newest", "3", EmailVerificationPurpose, true)
}

func TestGetOrCreateVerificationCodeReusesActiveCode(t *testing.T) {
	useVerificationMemory(t)
	verificationMutex.Lock()
	oldMap := verificationMap
	verificationMap = make(map[string]verificationValue)
	verificationMutex.Unlock()
	t.Cleanup(func() {
		verificationMutex.Lock()
		verificationMap = oldMap
		verificationMutex.Unlock()
	})

	key := "delayed@example.com"
	first, err := GetOrCreateVerificationCodeWithKey(key, EmailVerificationPurpose, 6)
	require.NoError(t, err)
	resent, err := GetOrCreateVerificationCodeWithKey(key, EmailVerificationPurpose, 6)
	require.NoError(t, err)
	if resent != first {
		t.Fatal("resending within the validity window should reuse the active code")
	}
	requireVerification(t, key, resent, EmailVerificationPurpose, true)

	verificationMutex.Lock()
	value := verificationMap[EmailVerificationPurpose+key]
	value.time = time.Now().Add(-time.Duration(VerificationValidMinutes) * time.Minute)
	verificationMap[EmailVerificationPurpose+key] = value
	verificationMutex.Unlock()

	refreshed, err := GetOrCreateVerificationCodeWithKey(key, EmailVerificationPurpose, 6)
	require.NoError(t, err)
	if refreshed == first {
		t.Fatal("an expired verification code should be replaced")
	}
	requireVerification(t, key, refreshed, EmailVerificationPurpose, true)
}

func useVerificationRedis(t *testing.T) *miniredis.Miniredis {
	t.Helper()
	server := miniredis.RunT(t)
	previous, enabled := RDB, RedisEnabled
	client := redis.NewClient(&redis.Options{Addr: server.Addr(), MaxRetries: -1})
	RDB, RedisEnabled = client, true
	t.Cleanup(func() { _ = client.Close(); RDB, RedisEnabled = previous, enabled })
	return server
}

func TestVerificationRedisCrossInstanceTTLAndDelete(t *testing.T) {
	server := useVerificationRedis(t)
	key := "cross-instance@example.com"
	first, err := GetOrCreateVerificationCodeWithKey(key, EmailVerificationPurpose, 6)
	require.NoError(t, err)
	// A fresh client has no process-local verification state.
	second := redis.NewClient(&redis.Options{Addr: server.Addr(), MaxRetries: -1})
	t.Cleanup(func() { _ = second.Close() })
	RDB = second
	requireVerification(t, key, first, EmailVerificationPurpose, true)
	requireVerification(t, key, first, PasswordResetPurpose, false)
	server.FastForward(time.Minute)
	resent, err := GetOrCreateVerificationCodeWithKey(key, EmailVerificationPurpose, 6)
	require.NoError(t, err)
	require.Equal(t, first, resent)
	require.Equal(t, time.Duration(VerificationValidMinutes-1)*time.Minute, server.TTL(verificationRedisKey(key, EmailVerificationPurpose)))
	server.FastForward(time.Duration(VerificationValidMinutes) * time.Minute)
	requireVerification(t, key, first, EmailVerificationPurpose, false)
	require.NoError(t, RegisterVerificationCodeWithKey(key, "replacement", EmailVerificationPurpose))
	require.NoError(t, DeleteKey(key, EmailVerificationPurpose))
	requireVerification(t, key, "replacement", EmailVerificationPurpose, false)
}

func TestVerificationRedisConcurrentReuseAndConsume(t *testing.T) {
	useVerificationRedis(t)
	const key = "parallel@example.com"
	var wg sync.WaitGroup
	codes := make(chan string, 16)
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			code, err := GetOrCreateVerificationCodeWithKey(key, PasswordResetPurpose, 0)
			if err != nil {
				t.Error(err)
				return
			}
			codes <- code
		}()
	}
	wg.Wait()
	close(codes)
	var code string
	for next := range codes {
		if code == "" {
			code = next
		}
		require.Equal(t, code, next)
	}
	require.NotEmpty(t, code)
	valid, err := ConsumeVerificationCodeWithKey(key, "wrong", PasswordResetPurpose)
	require.NoError(t, err)
	require.False(t, valid)
	var successes atomic.Int32
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			valid, err := ConsumeVerificationCodeWithKey(key, code, PasswordResetPurpose)
			if err != nil {
				t.Error(err)
			}
			if valid {
				successes.Add(1)
			}
		}()
	}
	wg.Wait()
	require.Equal(t, int32(1), successes.Load())
}

func TestVerificationRedisFailureNeverFallsBack(t *testing.T) {
	server := useVerificationRedis(t)
	server.SetError("ERR unavailable")
	verificationMutex.Lock()
	verificationMap[EmailVerificationPurpose+"local"] = verificationValue{code: "local-code", time: time.Now()}
	verificationMutex.Unlock()
	t.Cleanup(func() {
		verificationMutex.Lock()
		delete(verificationMap, EmailVerificationPurpose+"local")
		verificationMutex.Unlock()
	})
	for _, missingClient := range []bool{false, true} {
		if missingClient {
			RDB = nil
		}
		_, err := GetOrCreateVerificationCodeWithKey("local", EmailVerificationPurpose, 6)
		require.ErrorIs(t, err, ErrVerificationStoreUnavailable)
		require.ErrorIs(t, RegisterVerificationCodeWithKey("local", "new", EmailVerificationPurpose), ErrVerificationStoreUnavailable)
		valid, err := VerifyCodeWithKey("local", "local-code", EmailVerificationPurpose)
		require.ErrorIs(t, err, ErrVerificationStoreUnavailable)
		require.False(t, valid)
		valid, err = ConsumeVerificationCodeWithKey("local", "local-code", EmailVerificationPurpose)
		require.ErrorIs(t, err, ErrVerificationStoreUnavailable)
		require.False(t, valid)
		require.ErrorIs(t, DeleteKey("local", EmailVerificationPurpose), ErrVerificationStoreUnavailable)
	}
}
