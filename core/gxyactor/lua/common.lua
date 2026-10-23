-- 公共函数:构建期被拼到每个脚本前面(见 actor_locator_scripts.go)。
--
-- 为什么用拼接而不是 import:Redis 不提供模块机制,脚本里没有 require/dofile,
-- 也不允许全局状态;每个脚本仍是一次独立、自包含的 EVAL。
--
-- owner 记录格式:节点|epoch|令牌,与 Go 侧 encodeActorOwner 保持一致。
-- 节点名不含 "|";epoch 是十进制;令牌是十六进制。
local function parseOwner(record)
    local node, epoch, token = string.match(record, "^([^|]+)|([0-9]+)|(.+)$")
    return node, epoch, token
end

-- 节点租约键的**唯一**构造规则:前缀 + 节点名。
-- 前缀由调用方传入(对应 Go 侧 actorLocatorLeaseKeyPrefix),脚本里不写死。
local function nodeLeaseKey(prefix, node)
    return prefix .. node
end

-- 现任是否仍持有节点租约:租约键当前的值必须等于 owner 记录里记下的令牌。
-- 这是"记录说谁持有"与"租约说谁有权持有"两条判据的汇合点:
-- 相等 ⇒ 记录里的现任仍是那个活着的实例。
local function ownerStillHoldsLease(prefix, ownerNode, ownerToken)
    return redis.call("GET", nodeLeaseKey(prefix, ownerNode)) == ownerToken
end
