-- Locate:返回当前持有者;现任节点 lease 已失效时视为无人持有。
--
-- KEYS: 玩家 owner 键
-- ARGV: 租约键前缀
local owner = redis.call("GET", KEYS[1])
if not owner then
    return ""
end
local node, _, token = parseOwner(owner)
if not node then
    return ""
end
if not ownerStillHoldsLease(ARGV[1], node, token) then
    return ""
end
return owner
