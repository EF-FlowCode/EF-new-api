-- 释放并发槽位
-- KEYS[1]: 并发计数键
-- 返回剩余并发计数（不应低于 0）

local concKey = KEYS[1]
local v = tonumber(redis.call('GET', concKey)) or 0
if v <= 1 then
    redis.call('DEL', concKey)
    return 0
end
local remain = redis.call('DECR', concKey)
redis.call('EXPIRE', concKey, 60)
return remain
