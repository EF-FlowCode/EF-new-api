-- multi_key_scheduler 写 cool 标记
-- KEYS[1] ctrl hash
-- ARGV[1] coolUntilMs, [2] last429Msg (max 256 chars 由调用方裁), [3] nowMs
-- 返回 coolUntilMs

local ctrlKey   = KEYS[1]
local coolUntil = tonumber(ARGV[1])
local msg       = ARGV[2]
local nowMs     = tonumber(ARGV[3])

redis.call('HSET', ctrlKey, 'cool_until', tostring(coolUntil))
redis.call('HSET', ctrlKey, 'last_429_at', tostring(nowMs))
if msg and msg ~= '' then
    redis.call('HSET', ctrlKey, 'last_429_msg', msg)
end
redis.call('EXPIRE', ctrlKey, 300)

return tostring(coolUntil)