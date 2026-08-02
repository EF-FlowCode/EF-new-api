package common

import (
	"testing"
	"time"
)

func TestRegisterVerificationCodeEvictsOldestWhenCapacityStillExceeded(t *testing.T) {
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

	RegisterVerificationCodeWithKey("old", "1", EmailVerificationPurpose)
	time.Sleep(time.Millisecond)
	RegisterVerificationCodeWithKey("newer", "2", EmailVerificationPurpose)
	time.Sleep(time.Millisecond)
	RegisterVerificationCodeWithKey("newest", "3", EmailVerificationPurpose)

	if VerifyCodeWithKey("old", "1", EmailVerificationPurpose) {
		t.Fatal("oldest verification code should have been evicted")
	}
	if !VerifyCodeWithKey("newer", "2", EmailVerificationPurpose) || !VerifyCodeWithKey("newest", "3", EmailVerificationPurpose) {
		t.Fatal("newest verification codes should remain valid")
	}
}

func TestGetOrCreateVerificationCodeReusesActiveCode(t *testing.T) {
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
	first := GetOrCreateVerificationCodeWithKey(key, EmailVerificationPurpose, 6)
	resent := GetOrCreateVerificationCodeWithKey(key, EmailVerificationPurpose, 6)
	if resent != first {
		t.Fatal("resending within the validity window should reuse the active code")
	}
	if !VerifyCodeWithKey(key, resent, EmailVerificationPurpose) {
		t.Fatal("the resent verification code should remain valid")
	}

	verificationMutex.Lock()
	value := verificationMap[EmailVerificationPurpose+key]
	value.time = time.Now().Add(-time.Duration(VerificationValidMinutes) * time.Minute)
	verificationMap[EmailVerificationPurpose+key] = value
	verificationMutex.Unlock()

	refreshed := GetOrCreateVerificationCodeWithKey(key, EmailVerificationPurpose, 6)
	if refreshed == first {
		t.Fatal("an expired verification code should be replaced")
	}
	if !VerifyCodeWithKey(key, refreshed, EmailVerificationPurpose) {
		t.Fatal("the refreshed verification code should be valid")
	}
}
