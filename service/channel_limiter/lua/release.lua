-- 释放并发槽位
-- KEYS[1]: 并发计数键
-- ARGV[1]: 过期秒数（与 acquire 一致）
-- 返回剩余并发计数（不应低于 0）

local concKey = KEYS[1]
local v = tonumber(redis.call('GET', concKey)) or 0
if v <= 1 then
    redis.call('DEL', concKey)
    return 0
end
local remain = redis.call('DECR', concKey)
redis.call('EXPIRE', concKey, tonumber(ARGV[1]))
return remain
