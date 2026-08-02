-- 渠道限流原子 acquire 脚本
-- KEYS[1]: RPM 滑动窗口 ZSET 键
-- KEYS[2]: 并发计数键
-- ARGV[1]: RPM 上限 (0 = 不限)
-- ARGV[2]: 滑动窗口秒数
-- ARGV[3]: 并发上限 (0 = 不限)
-- ARGV[4]: 唯一 token (用于 ZSET 成员去重，由调用方提供)
-- 返回:
--   {"1"}                      => 已通过
--   {"0", "rpm_exceeded"}       => RPM 达限
--   {"0", "concurrency_exceeded"} => 并发达限

local rpmKey = KEYS[1]
local concKey = KEYS[2]
local rpmLimit = tonumber(ARGV[1])
local window = tonumber(ARGV[2])
local concLimit = tonumber(ARGV[3])
local token = ARGV[4]

local now = redis.call('TIME')
local nowMs = tonumber(now[1]) * 1000 + math.floor(tonumber(now[2]) / 1000)

-- 1. RPM 检查（滑动窗口）
if rpmLimit > 0 then
    redis.call('ZREMRANGEBYSCORE', rpmKey, 0, nowMs - window * 1000)
    local count = redis.call('ZCARD', rpmKey)
    if count >= rpmLimit then
        return { '0', 'rpm_exceeded' }
    end
end

-- 2. 并发检查
if concLimit > 0 then
    local current = tonumber(redis.call('GET', concKey)) or 0
    if current >= concLimit then
        return { '0', 'concurrency_exceeded' }
    end
end

-- 3. 原子预占
if rpmLimit > 0 then
    redis.call('ZADD', rpmKey, nowMs, token)
    redis.call('PEXPIRE', rpmKey, (window + 5) * 1000)
end
if concLimit > 0 then
    redis.call('INCR', concKey)
    redis.call('EXPIRE', concKey, 60)
end
return { '1' }
