-- multi_key_scheduler 释放并发槽位 + 可选记 TPM
-- KEYS[1] tpm zset, KEYS[2] ctrl hash
-- ARGV[1] tokens (>0 时记 TPM，<=0 跳过), [2] nowMs, [3] window seconds, [4] uniqueSuffix
-- 返回剩余 inFlight

local tpmKey  = KEYS[1]
local ctrlKey = KEYS[2]
local tokens  = tonumber(ARGV[1])
local nowMs   = tonumber(ARGV[2])
local window  = tonumber(ARGV[3])
local suffix  = ARGV[4]

local inFlight = tonumber(redis.call('HGET', ctrlKey, 'in_flight') or '0')
if inFlight > 0 then
    inFlight = redis.call('HINCRBY', ctrlKey, 'in_flight', -1)
    if inFlight < 0 then
        redis.call('HSET', ctrlKey, 'in_flight', 0)
        inFlight = 0
    end
end
redis.call('EXPIRE', ctrlKey, 300)

if tokens and tokens > 0 then
    -- member 形如 "<tokens>:<suffix>"，suffix 防同 ms 多笔合并
    redis.call('ZADD', tpmKey, nowMs, tostring(tokens) .. ':' .. suffix)
    redis.call('PEXPIRE', tpmKey, (window + 5) * 1000)
end

return inFlight