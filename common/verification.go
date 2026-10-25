package common

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/google/uuid"
)

type verificationValue struct {
	code string
	time time.Time
}

const (
	EmailVerificationPurpose = "v"
	PasswordResetPurpose     = "r"
	// emailBindPurposePrefix eflow: 已登录用户绑定邮箱的验证码按用户隔离，不能被其他账号使用。
	emailBindPurposePrefix = "bind:"
)

var verificationMutex sync.Mutex
var verificationMap map[string]verificationValue
var verificationMapMaxSize = 10000
var VerificationValidMinutes = 10

var ErrVerificationStoreUnavailable = errors.New("verification store unavailable; please retry later")

const verificationStoreTimeout = 2 * time.Second

func verificationRedisKey(key, purpose string) string {
	return "verification:" + purpose + ":" + key
}

var reuseVerificationCode = redis.NewScript(`
local code = redis.call('GET', KEYS[1])
if code then return code end
redis.call('SET', KEYS[1], ARGV[1], 'PX', ARGV[2])
return ARGV[1]
`)

var consumeVerificationCode = redis.NewScript(`
if redis.call('GET', KEYS[1]) ~= ARGV[1] then return 0 end
redis.call('DEL', KEYS[1])
return 1
`)

func GenerateVerificationCode(length int) string {
	code := uuid.New().String()
	code = strings.Replace(code, "-", "", -1)
	if length == 0 {
		return code
	}
	return code[:length]
}

func RegisterVerificationCodeWithKey(key string, code string, purpose string) error {
	if RedisEnabled {
		if RDB == nil {
			return ErrVerificationStoreUnavailable
		}
		ctx, cancel := context.WithTimeout(context.Background(), verificationStoreTimeout)
		defer cancel()
		if err := RDB.Set(ctx, verificationRedisKey(key, purpose), code, time.Duration(VerificationValidMinutes)*time.Minute).Err(); err != nil {
			return ErrVerificationStoreUnavailable
		}
		return nil
	}
	verificationMutex.Lock()
	defer verificationMutex.Unlock()
	storeVerificationCodeLocked(purpose+key, code, time.Now())
	return nil
}

func GetOrCreateVerificationCodeWithKey(key string, purpose string, length int) (string, error) {
	if RedisEnabled {
		if RDB == nil {
			return "", ErrVerificationStoreUnavailable
		}
		ctx, cancel := context.WithTimeout(context.Background(), verificationStoreTimeout)
		defer cancel()
		code, err := reuseVerificationCode.Run(ctx, RDB, []string{verificationRedisKey(key, purpose)}, GenerateVerificationCode(length), (time.Duration(VerificationValidMinutes) * time.Minute).Milliseconds()).Text()
		if err != nil {
			return "", ErrVerificationStoreUnavailable
		}
		return code, nil
	}
	verificationMutex.Lock()
	defer verificationMutex.Unlock()
	mapKey := purpose + key
	now := time.Now()
	if value, ok := verificationMap[mapKey]; ok && int(now.Sub(value.time).Seconds()) < VerificationValidMinutes*60 {
		return value.code, nil
	}
	code := GenerateVerificationCode(length)
	storeVerificationCodeLocked(mapKey, code, now)
	return code, nil
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

func VerifyCodeWithKey(key string, code string, purpose string) (bool, error) {
	return verifyCodeWithKey(key, code, purpose, false)
}

// ConsumeVerificationCodeWithKey prevents two instances from reusing a reset token.
func ConsumeVerificationCodeWithKey(key string, code string, purpose string) (bool, error) {
	return verifyCodeWithKey(key, code, purpose, true)
}

func verifyCodeWithKey(key, code, purpose string, consume bool) (bool, error) {
	if RedisEnabled {
		if RDB == nil {
			return false, ErrVerificationStoreUnavailable
		}
		ctx, cancel := context.WithTimeout(context.Background(), verificationStoreTimeout)
		defer cancel()
		if consume {
			result, err := consumeVerificationCode.Run(ctx, RDB, []string{verificationRedisKey(key, purpose)}, code).Int()
			if err != nil {
				return false, ErrVerificationStoreUnavailable
			}
			return result == 1, nil
		}
		value, err := RDB.Get(ctx, verificationRedisKey(key, purpose)).Result()
		if errors.Is(err, redis.Nil) {
			return false, nil
		}
		if err != nil {
			return false, ErrVerificationStoreUnavailable
		}
		return code == value, nil
	}
	verificationMutex.Lock()
	defer verificationMutex.Unlock()
	value, okay := verificationMap[purpose+key]
	now := time.Now()
	if !okay || int(now.Sub(value.time).Seconds()) >= VerificationValidMinutes*60 {
		return false, nil
	}
	valid := code == value.code
	if valid && consume {
		delete(verificationMap, purpose+key)
	}
	return valid, nil
}

func DeleteKey(key string, purpose string) error {
	if RedisEnabled {
		if RDB == nil {
			return ErrVerificationStoreUnavailable
		}
		ctx, cancel := context.WithTimeout(context.Background(), verificationStoreTimeout)
		defer cancel()
		if err := RDB.Del(ctx, verificationRedisKey(key, purpose)).Err(); err != nil {
			return ErrVerificationStoreUnavailable
		}
		return nil
	}
	verificationMutex.Lock()
	defer verificationMutex.Unlock()
	delete(verificationMap, purpose+key)
	return nil
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

// EmailBindVerificationPurpose 返回指定用户绑定邮箱时使用的验证码用途。
// EmailBindOldAddressPurpose scopes the code sent to a user's current address
// when they replace it (eflow: replacing an email confirms both addresses).
func EmailBindOldAddressPurpose(userID int) string {
	return "eo" + strconv.Itoa(userID) + ":"
}

func EmailBindVerificationPurpose(userID int) string {
	return emailBindPurposePrefix + strconv.Itoa(userID)
}
