-- multi_key_scheduler 只读 peek，不预占
-- KEYS / ARGV 同 acquire（无 token 参数）
-- 返回:
--   { "1", rpmCount, tpmCount, inFlight, "0" }
--   { "0", reason, rpmCount, tpmCount, inFlight, coolUntilMs }

local rpmKey  = KEYS[1]
local tpmKey  = KEYS[2]
local ctrlKey = KEYS[3]
local rpmMax  = tonumber(ARGV[1])
local tpmMax  = tonumber(ARGV[2])
local concMax = tonumber(ARGV[3])
local window  = tonumber(ARGV[4])
local nowMs   = tonumber(ARGV[5])

local coolUntil = tonumber(redis.call('HGET', ctrlKey, 'cool_until') or '0')
if coolUntil > nowMs then
    return { '0', 'cooling', '0', '0', '0', tostring(coolUntil) }
end

local cutoff = nowMs - window * 1000

local rpmCount = 0
if rpmMax > 0 then
    redis.call('ZREMRANGEBYSCORE', rpmKey, 0, cutoff)
    rpmCount = tonumber(redis.call('ZCARD', rpmKey))
end
local tpmCount = 0
if tpmMax > 0 then
    redis.call('ZREMRANGEBYSCORE', tpmKey, 0, cutoff)
    local members = redis.call('ZRANGE', tpmKey, 0, -1)
    for _, m in ipairs(members) do
        local sep = string.find(m, ':')
        if sep then
            local n = tonumber(string.sub(m, 1, sep - 1))
            if n then tpmCount = tpmCount + n end
        end
    end
end
local inFlight = tonumber(redis.call('HGET', ctrlKey, 'in_flight') or '0')

if rpmMax > 0 and rpmCount >= rpmMax then
    return { '0', 'rpm_exceeded', tostring(rpmCount), tostring(tpmCount), tostring(inFlight), '0' }
end
if tpmMax > 0 and tpmCount >= tpmMax then
    return { '0', 'tpm_exceeded', tostring(rpmCount), tostring(tpmCount), tostring(inFlight), '0' }
end
if concMax > 0 and inFlight >= concMax then
    return { '0', 'concurrency_exceeded', tostring(rpmCount), tostring(tpmCount), tostring(inFlight), '0' }
end

return { '1', tostring(rpmCount), tostring(tpmCount), tostring(inFlight), '0' }