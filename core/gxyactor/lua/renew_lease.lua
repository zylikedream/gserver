-- 续租:仅当令牌仍匹配时续期;返回 0 表示租约已不属于本实例,
-- 由调用方自栅(fence)。
--
-- KEYS: 节点 lease 键
-- ARGV: 本实例的 lease token、新的 TTL(毫秒)
local leaseKey = KEYS[1]
if redis.call("GET", leaseKey) == ARGV[1] then
    return redis.call("PEXPIRE", leaseKey, ARGV[2])
end
return 0
