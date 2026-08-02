-- 渠道限流 peek（只读判定，不预占）
-- KEYS[1]: RPM 滑动窗口 ZSET 键
-- KEYS[2]: 并发计数键
-- ARGV[1]: RPM 上限 (0 = 不限)
-- ARGV[2]: 滑动窗口秒数
-- ARGV[3]: 并发上限 (0 = 不限)
-- 返回:
--   {"1"}                      => 当前有容量
--   {"0", "rpm_exceeded"}       => RPM 达限
--   {"0", "concurrency_exceeded"} => 并发达限

local rpmKey = KEYS[1]
local concKey = KEYS[2]
local rpmLimit = tonumber(ARGV[1])
local window = tonumber(ARGV[2])
local concLimit = tonumber(ARGV[3])

if rpmLimit > 0 then
    local now = redis.call('TIME')
    local nowMs = tonumber(now[1]) * 1000 + math.floor(tonumber(now[2]) / 1000)
    redis.call('ZREMRANGEBYSCORE', rpmKey, 0, nowMs - window * 1000)
    local count = redis.call('ZCARD', rpmKey)
    if count >= rpmLimit then
        return { '0', 'rpm_exceeded' }
    end
end

if concLimit > 0 then
    local current = tonumber(redis.call('GET', concKey)) or 0
    if current >= concLimit then
        return { '0', 'concurrency_exceeded' }
    end
end
return { '1' }
