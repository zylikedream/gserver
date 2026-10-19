-- 释放节点租约:仅当令牌仍匹配(只删自己那一份)。
--
-- KEYS: 节点 lease 键
-- ARGV: 本实例的 lease token
local leaseKey = KEYS[1]
if redis.call("GET", leaseKey) == ARGV[1] then
    redis.call("DEL", leaseKey)
    return 1
end
return 0
