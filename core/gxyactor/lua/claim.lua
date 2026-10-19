-- Claim:申请方自证租约有效后,与现任做一次原子仲裁。
-- 返回值为 {状态, owner值};状态由 claimResult 及 invalid_lease 分支解释。
--
-- 三个身份:
--   claimant = 本次申请方(调用方)
--   owner    = 记录里的现任
--   成功后成为 owner 的,就是 claimant
-- 申请方与现任可以是同一台节点(重复 Claim → already_owned)。
--
-- KEYS: 玩家 owner 键、全局 epoch 键
-- ARGV: 申请方节点 id、申请方 lease token、租约键前缀
local ownerKey = KEYS[1]         -- 玩家 owner 键
local epochKey = KEYS[2]         -- 全局 epoch 键
local claimantNode = ARGV[1]     -- 申请方节点 id
local claimantToken = ARGV[2]    -- 申请方节点 token
local leasePrefix = ARGV[3]      -- 租约键前缀

-- 租约键统一由前缀在脚本内构造,规则只有这一处。
local claimantLeaseKey = nodeLeaseKey(leasePrefix, claimantNode)

-- 申请方节点无法证明自己仍持有 lease 时必须 fail closed,禁止抢占。
if redis.call("GET", claimantLeaseKey) ~= claimantToken then
    return {"invalid_lease", ""}
end

-- 检查现任节点是否活跃。
local ownerRecord = redis.call("GET", ownerKey)
if ownerRecord then
    -- 解析现任节点记录。
    local ownerNode, _, ownerToken = parseOwner(ownerRecord)
    if ownerNode then
        -- 仅当现任仍持有节点租约,现任才算活跃。
        if ownerStillHoldsLease(leasePrefix, ownerNode, ownerToken) then
            if ownerNode == claimantNode then
                -- 同一节点重复 Claim:保留原 epoch,避免无意义地生成新 owner。
                return {"already_owned", ownerRecord}
            end
            -- 其他节点仍是活跃 owner:申请方不能越权接管。
            return {"owned_by_other", ownerRecord}
        end
    end
end

-- 记录缺失、格式非法或现任 lease 已失效:递增 epoch 后由申请方原子接管。
local epoch = redis.call("INCR", epochKey)
local owner = claimantNode .. "|" .. epoch .. "|" .. claimantToken
redis.call("SET", ownerKey, owner)
return {"acquired", owner}
