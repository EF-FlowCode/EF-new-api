package model

import (
	"context"
	"fmt"
	"strconv"

	"eflow.local/new-api/common"
	"eflow.local/new-api/constant"
	"eflow.local/new-api/dto"

	"github.com/gin-gonic/gin"
)

// UserBase struct remains the same as it represents the cached data structure
type UserBase struct {
	Id                      int       `json:"id"`
	Group                   string    `json:"group"`
	AdditionalCustomerTiers GroupList `json:"additional_customer_tiers"`
	Email                   string    `json:"email"`
	Quota                   int       `json:"quota"`
	Status                  int       `json:"status"`
	Role                    int       `json:"role"`
	Username                string    `json:"username"`
	Setting                 string    `json:"setting"`
	CacheSchema             int       `json:"-"`
}

const userCacheSchemaVersion = 1

func (user *UserBase) WriteContext(c *gin.Context) {
	common.SetContextKey(c, constant.ContextKeyUserGroup, user.Group)
	common.SetContextKey(c, constant.ContextKeyUserAdditionalCustomerTiers, user.AdditionalCustomerTiers.Values())
	common.SetContextKey(c, constant.ContextKeyUserQuota, user.Quota)
	common.SetContextKey(c, constant.ContextKeyUserStatus, user.Status)
	common.SetContextKey(c, constant.ContextKeyUserEmail, user.Email)
	common.SetContextKey(c, constant.ContextKeyUserName, user.Username)
	common.SetContextKey(c, constant.ContextKeyUserSetting, user.GetSetting())
}

func (user *UserBase) GetSetting() dto.UserSetting {
	setting := dto.UserSetting{}
	if user.Setting != "" {
		err := common.Unmarshal([]byte(user.Setting), &setting)
		if err != nil {
			common.SysLog("failed to unmarshal setting: " + err.Error())
		}
	}
	return setting
}

// getUserCacheKey returns the key for user cache
func getUserCacheKey(userId int) string {
	return fmt.Sprintf("user:%d", userId)
}

func userCacheTTLSeconds() int {
	ttl := common.RedisKeyCacheSeconds()
	if ttl <= 0 {
		return 60
	}
	return ttl
}

// invalidateUserCache clears user cache
func invalidateUserCache(userId int) error {
	if !common.RedisEnabled {
		return nil
	}
	return common.RedisDelKey(getUserCacheKey(userId))
}

// InvalidateUserCache is the exported version of invalidateUserCache.
// 供 controller 等上层包在用户状态变更（如禁用、删除、角色变更）后主动清理缓存。
func InvalidateUserCache(userId int) error {
	return invalidateUserCache(userId)
}

// InvalidateUserAuthCaches 在数据库已提交用户状态变更（封禁/解封）后清理用户与其令牌缓存，
// 使下一次鉴权直接读取数据库状态；失败只记录日志，数据库仍是状态权威。
func InvalidateUserAuthCaches(userId int) {
	if err := InvalidateUserCache(userId); err != nil {
		common.SysLog(fmt.Sprintf("failed to invalidate user cache for user %d: %s", userId, err.Error()))
	}
	if err := InvalidateUserTokensCache(userId); err != nil {
		common.SysLog(fmt.Sprintf("failed to invalidate tokens cache for user %d: %s", userId, err.Error()))
	}
}

func populateUserCache(user User) error {
	if !common.RedisEnabled {
		return nil
	}
	_, err := cacheInitUser(*user.ToBaseUser())
	return err
}

// cacheInitUser 只在缓存为空或旧格式时发布数据库快照；活跃哈希中的 Quota
// 可能已经领先于批量落库进度，绝不能被稍早读取的数据库快照覆盖。
func cacheInitUser(user UserBase) (int, error) {
	if !common.RedisEnabled {
		return 0, nil
	}
	const script = `
local cachedId = tonumber(redis.call('HGET', KEYS[1], 'Id') or '0')
local schema = tonumber(redis.call('HGET', KEYS[1], 'CacheSchema') or '0')
if cachedId == tonumber(ARGV[1]) and schema == tonumber(ARGV[10]) then
  redis.call('EXPIRE', KEYS[1], ARGV[11])
  return 2
end
if redis.call('EXISTS', KEYS[1]) == 1 then
  redis.call('DEL', KEYS[1])
end
redis.call('HSET', KEYS[1],
  'Id', ARGV[1], 'Group', ARGV[2], 'AdditionalCustomerTiers', ARGV[3],
  'Email', ARGV[4], 'Quota', ARGV[5], 'Status', ARGV[6], 'Role', ARGV[7],
  'Username', ARGV[8], 'Setting', ARGV[9], 'CacheSchema', ARGV[10])
redis.call('EXPIRE', KEYS[1], ARGV[11])
return 1`
	return common.RDB.Eval(context.Background(), script, []string{getUserCacheKey(user.Id)},
		user.Id, user.Group, string(user.AdditionalCustomerTiers), user.Email, user.Quota,
		user.Status, user.Role, user.Username, user.Setting, userCacheSchemaVersion,
		userCacheTTLSeconds(),
	).Int()
}

// updateUserCache refreshes non-quota user cache fields.
// Quota is maintained by atomic quota delta paths and must not be overwritten
// by stale user snapshots from profile/settings updates.
// 在数据库提交后刷新缓存中的资料字段。所有字段在同一脚本内一次写入，
// 避免并发读取到新旧混合的等级组合；刷新失败时退化为删除缓存，下次读取回源数据库，
// 只有删除也失败才返回错误（此时缓存可能仍是旧值）。
func updateUserCache(user User) error {
	if !common.RedisEnabled {
		return nil
	}
	status := common.UserStatusEnabled
	if user.Status != common.UserStatusEnabled {
		status = common.UserStatusDisabled
	}
	err := updateUserCacheFieldsIfPresent(user.Id, []any{
		"Group", user.Group,
		"AdditionalCustomerTiers", string(user.AdditionalCustomerTiers),
		"Email", user.Email,
		"Status", strconv.Itoa(status),
		"Role", user.Role,
		"Username", user.Username,
		"Setting", user.Setting,
	})
	if err == nil {
		return nil
	}
	common.SysLog(fmt.Sprintf("refresh user cache for user %d failed, invalidating: %s", user.Id, err.Error()))
	return invalidateUserCache(user.Id)
}

// GetUserCache gets complete user cache from hash
func GetUserCache(userId int) (userCache *UserBase, err error) {
	if common.RedisEnabled {
		userCache, err = cacheGetUserBase(userId)
		if err == nil {
			return userCache, nil
		}
	}

	user, err := GetUserById(userId, false)
	if err != nil {
		return nil, err
	}
	userCache = user.ToBaseUser()
	if common.RedisEnabled {
		if _, cacheErr := cacheInitUser(*userCache); cacheErr != nil {
			common.SysLog("failed to init user cache: " + cacheErr.Error())
		}
	}
	return userCache, nil
}

func cacheGetUserBase(userId int) (*UserBase, error) {
	if !common.RedisEnabled {
		return nil, fmt.Errorf("redis is not enabled")
	}
	var userCache UserBase
	// Try getting from Redis first
	err := common.RedisHGetObj(getUserCacheKey(userId), &userCache)
	if err != nil {
		return nil, err
	}
	if userCache.Id != userId || userCache.CacheSchema != userCacheSchemaVersion {
		return nil, fmt.Errorf("user cache is incomplete or stale")
	}
	return &userCache, nil
}

// Add atomic quota operations using hash fields
func cacheIncrUserQuota(userId int, delta int64) error {
	if !common.RedisEnabled {
		return nil
	}
	_, err := cacheApplyUserQuotaDelta(userId, delta)
	return err
}

func cacheDecrUserQuota(userId int, delta int64) error {
	return cacheIncrUserQuota(userId, -delta)
}

// syncCreditUserQuotaCache applies a committed top-up delta to an existing
// Redis user cache. A cache miss is harmless: the next read hydrates from DB.
func syncCreditUserQuotaCache(userId int, quota int, operation string) {
	if quota <= 0 {
		return
	}
	if err := cacheIncrUserQuota(userId, int64(quota)); err != nil {
		common.SysLog(fmt.Sprintf("failed to sync %s credit to user quota cache: %s", operation, err.Error()))
	}
}

// Helper functions to get individual fields if needed
func getUserGroupCache(userId int) (string, error) {
	cache, err := GetUserCache(userId)
	if err != nil {
		return "", err
	}
	return cache.Group, nil
}

func getUserQuotaCache(userId int) (int, error) {
	cache, err := GetUserCache(userId)
	if err != nil {
		return 0, err
	}
	return cache.Quota, nil
}

func getUserStatusCache(userId int) (int, error) {
	cache, err := GetUserCache(userId)
	if err != nil {
		return 0, err
	}
	return cache.Status, nil
}

func getUserNameCache(userId int) (string, error) {
	cache, err := GetUserCache(userId)
	if err != nil {
		return "", err
	}
	return cache.Username, nil
}

func getUserSettingCache(userId int) (dto.UserSetting, error) {
	cache, err := GetUserCache(userId)
	if err != nil {
		return dto.UserSetting{}, err
	}
	return cache.GetSetting(), nil
}

// New functions for individual field updates
func updateUserQuotaCache(userId int, quota int) error {
	if !common.RedisEnabled {
		return nil
	}
	return updateUserCacheFieldIfPresent(userId, "Quota", strconv.Itoa(quota))
}

func updateUserGroupCache(userId int, group string) error {
	if !common.RedisEnabled {
		return nil
	}
	return updateUserCacheFieldIfPresent(userId, "Group", group)
}

func UpdateUserGroupCache(userId int, group string) error {
	return updateUserGroupCache(userId, group)
}

func updateUserNameCache(userId int, username string) error {
	if !common.RedisEnabled {
		return nil
	}
	return updateUserCacheFieldIfPresent(userId, "Username", username)
}

func updateUserSettingCache(userId int, setting string) error {
	if !common.RedisEnabled {
		return nil
	}
	return updateUserCacheFieldIfPresent(userId, "Setting", setting)
}

// updateUserCacheFieldsIfPresent 原子写入多个字段，fields 为交替的字段名与值。
func updateUserCacheFieldsIfPresent(userId int, fields []any) error {
	if !common.RedisEnabled {
		return nil
	}
	const script = `
if tonumber(redis.call('HGET', KEYS[1], 'Id') or '0') ~= tonumber(ARGV[1])
  or tonumber(redis.call('HGET', KEYS[1], 'CacheSchema') or '0') ~= tonumber(ARGV[2]) then
  return 0
end
redis.call('HSET', KEYS[1], unpack(ARGV, 3))
return 1`
	args := append([]any{userId, userCacheSchemaVersion}, fields...)
	return common.RDB.Eval(context.Background(), script, []string{getUserCacheKey(userId)}, args...).Err()
}

func updateUserCacheFieldIfPresent(userId int, field string, value any) error {
	if !common.RedisEnabled {
		return nil
	}
	const script = `
if tonumber(redis.call('HGET', KEYS[1], 'Id') or '0') ~= tonumber(ARGV[1])
  or tonumber(redis.call('HGET', KEYS[1], 'CacheSchema') or '0') ~= tonumber(ARGV[2]) then
  return 0
end
redis.call('HSET', KEYS[1], ARGV[3], ARGV[4])
return 1`
	return common.RDB.Eval(context.Background(), script, []string{getUserCacheKey(userId)},
		userId, userCacheSchemaVersion, field, value).Err()
}

// GetUserLanguage returns the user's language preference from cache
// Uses the existing GetUserCache mechanism for efficiency
func GetUserLanguage(userId int) string {
	userCache, err := GetUserCache(userId)
	if err != nil {
		return ""
	}
	return userCache.GetSetting().Language
}
