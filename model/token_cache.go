package model

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"eflow.local/new-api/common"
)

func getTokenCacheKey(key string) string {
	return fmt.Sprintf("token:%s", common.GenerateHMAC(key))
}

func getTokenCacheFenceKey(key string) string {
	return fmt.Sprintf("token:fence:%s", common.GenerateHMAC(key))
}

func tokenCacheTTLSeconds() int {
	ttl := common.RedisKeyCacheSeconds()
	if ttl <= 0 {
		return 60
	}
	return ttl
}

const tokenCacheFenceSeconds = 10

func invalidateTokenCacheForMutation(key string) error {
	if !common.RedisEnabled || key == "" {
		return nil
	}
	ctx := context.Background()
	err := common.RDB.Set(ctx, getTokenCacheFenceKey(key), 1, time.Duration(tokenCacheFenceSeconds)*time.Second).Err()
	if err != nil {
		return err
	}
	return common.RDB.Del(ctx, getTokenCacheKey(key)).Err()
}

// cacheInitToken 只发布冷缓存快照；已有哈希中的余额可能已完成原子预扣，
// 此时仅刷新 TTL。fence 存在时拒绝发布变更前读取到的旧快照。
func cacheInitToken(token Token) (int, error) {
	if !common.RedisEnabled {
		return 0, nil
	}
	allowIps := ""
	if token.AllowIps != nil {
		allowIps = *token.AllowIps
	}
	const script = `
if redis.call('EXISTS', KEYS[2]) == 1 then
  return 0
end
if tonumber(redis.call('HGET', KEYS[1], 'Id') or '0') == tonumber(ARGV[1])
  and redis.call('HEXISTS', KEYS[1], 'RemainQuota') == 1
  and redis.call('HEXISTS', KEYS[1], 'UsedQuota') == 1 then
  redis.call('EXPIRE', KEYS[1], ARGV[16])
  return 2
end
if redis.call('EXISTS', KEYS[1]) == 1 then
  redis.call('DEL', KEYS[1])
end
redis.call('HSET', KEYS[1],
  'Id', ARGV[1], 'UserId', ARGV[2], 'Status', ARGV[3], 'Name', ARGV[4],
  'CreatedTime', ARGV[5], 'AccessedTime', ARGV[6], 'ExpiredTime', ARGV[7],
  'UnlimitedQuota', ARGV[8], 'ModelLimitsEnabled', ARGV[9], 'ModelLimits', ARGV[10],
  'AllowIps', ARGV[11], 'Group', ARGV[12], 'CrossGroupRetry', ARGV[13],
  'RemainQuota', ARGV[14], 'UsedQuota', ARGV[15])
redis.call('EXPIRE', KEYS[1], ARGV[16])
return 1`

	return common.RDB.Eval(context.Background(), script, []string{
		getTokenCacheKey(token.Key), getTokenCacheFenceKey(token.Key),
	}, token.Id, token.UserId, token.Status, token.Name, token.CreatedTime,
		token.AccessedTime, token.ExpiredTime, strconv.FormatBool(token.UnlimitedQuota),
		strconv.FormatBool(token.ModelLimitsEnabled), token.ModelLimits, allowIps,
		token.Group, strconv.FormatBool(token.CrossGroupRetry), token.RemainQuota,
		token.UsedQuota, tokenCacheTTLSeconds()).Int()
}

// CacheGetTokenByKey 从缓存中获取 token，如果缓存中不存在，则从数据库中获取
func cacheGetTokenByKey(key string) (*Token, error) {
	if !common.RedisEnabled {
		return nil, fmt.Errorf("redis is not enabled")
	}
	var token Token
	err := common.RedisHGetObj(getTokenCacheKey(key), &token)
	if err != nil {
		return nil, err
	}
	if token.Id <= 0 {
		return nil, fmt.Errorf("token cache is incomplete")
	}
	token.Key = key
	return &token, nil
}
