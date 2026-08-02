package common

import (
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

type verificationValue struct {
	code string
	time time.Time
}

const (
	EmailVerificationPurpose = "v"
	PasswordResetPurpose     = "r"
)

var verificationMutex sync.Mutex
var verificationMap map[string]verificationValue
var verificationMapMaxSize = 10000
var VerificationValidMinutes = 10

func GenerateVerificationCode(length int) string {
	code := uuid.New().String()
	code = strings.Replace(code, "-", "", -1)
	if length == 0 {
		return code
	}
	return code[:length]
}

func RegisterVerificationCodeWithKey(key string, code string, purpose string) {
	verificationMutex.Lock()
	defer verificationMutex.Unlock()
	storeVerificationCodeLocked(purpose+key, code, time.Now())
}

func GetOrCreateVerificationCodeWithKey(key string, purpose string, length int) string {
	verificationMutex.Lock()
	defer verificationMutex.Unlock()
	mapKey := purpose + key
	now := time.Now()
	if value, ok := verificationMap[mapKey]; ok && int(now.Sub(value.time).Seconds()) < VerificationValidMinutes*60 {
		return value.code
	}
	code := GenerateVerificationCode(length)
	storeVerificationCodeLocked(mapKey, code, now)
	return code
}

func storeVerificationCodeLocked(key string, code string, now time.Time) {
	verificationMap[key] = verificationValue{
		code: code,
		time: now,
	}
	if len(verificationMap) > verificationMapMaxSize {
		removeExpiredPairs()
		removeOldestPairs(verificationMapMaxSize)
	}
}

func VerifyCodeWithKey(key string, code string, purpose string) bool {
	verificationMutex.Lock()
	defer verificationMutex.Unlock()
	value, okay := verificationMap[purpose+key]
	now := time.Now()
	if !okay || int(now.Sub(value.time).Seconds()) >= VerificationValidMinutes*60 {
		return false
	}
	return code == value.code
}

func DeleteKey(key string, purpose string) {
	verificationMutex.Lock()
	defer verificationMutex.Unlock()
	delete(verificationMap, purpose+key)
}

// no lock inside, so the caller must lock the verificationMap before calling!
func removeExpiredPairs() {
	now := time.Now()
	for key := range verificationMap {
		if int(now.Sub(verificationMap[key].time).Seconds()) >= VerificationValidMinutes*60 {
			delete(verificationMap, key)
		}
	}
}

func removeOldestPairs(maxSize int) {
	for len(verificationMap) > maxSize {
		var oldestKey string
		var oldestTime time.Time
		for key, value := range verificationMap {
			if oldestKey == "" || value.time.Before(oldestTime) {
				oldestKey = key
				oldestTime = value.time
			}
		}
		if oldestKey == "" {
			return
		}
		delete(verificationMap, oldestKey)
	}
}

func init() {
	verificationMutex.Lock()
	defer verificationMutex.Unlock()
	verificationMap = make(map[string]verificationValue)
}
