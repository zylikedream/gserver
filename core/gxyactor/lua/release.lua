-- Release:条件删除。整串(节点|epoch|令牌)匹配才删,
-- 旧实例因此删不掉新持有者的记录。
--
-- KEYS: 玩家 owner 键
-- ARGV: 期望的 owner 记录
local ownerKey = KEYS[1]
local expectedToken = ARGV[1]
if redis.call("GET", ownerKey) == expectedToken then
    redis.call("DEL", ownerKey)
    return 1
end
return 0
