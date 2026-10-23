package gxyactor

import _ "embed"

// 本文件集中放定位与归属用的 Redis Lua 脚本。
//
// 为什么放 .lua 文件而不是 Go 字符串字面量:脚本难被静态检查,独立文件至少有
// 语法高亮与格式工具;缺文件在编译期就报错(//go:embed),部署也无需额外发文件。
//
// 为什么公共函数靠拼接而不是 import:Redis 不提供模块机制——脚本里没有
// require/dofile,也不允许全局状态。common.lua 因此在构建期被拼到每个脚本前面,
// 每个脚本仍是一次独立、自包含的 EVAL。
//
// 两条约定(改脚本前先看这里):
//   - 租约键的构造规则只此一条:节点租约键 = 前缀 + 节点名。两处实现必须同步:
//     Go 侧 actorLocatorLeaseKeyPrefix / actorLocatorLeaseKey,与
//     lua/common.lua 的 nodeLeaseKey;脚本里不写死键前缀;
//   - owner 记录格式为 节点|epoch|令牌:Go 侧 encodeActorOwner 与 common.lua 的
//     parseOwner 必须同时改。

//go:embed lua/common.lua
var luaCommon string

//go:embed lua/claim.lua
var luaClaim string

//go:embed lua/locate.lua
var luaLocate string

//go:embed lua/release.lua
var luaRelease string

//go:embed lua/release_node_lease.lua
var luaReleaseNodeLease string

//go:embed lua/renew_lease.lua
var luaRenewLease string

// 所有脚本统一前置公共函数:规则一致比省几个字节重要,也避免"为什么这个脚本没有"。
var (
	actorLocatorClaimScript        = luaCommon + luaClaim
	actorLocatorLocateScript       = luaCommon + luaLocate
	actorLocatorReleaseScript      = luaCommon + luaRelease
	actorLocatorReleaseLeaseScript = luaCommon + luaReleaseNodeLease
	actorLocatorRenewLeaseScript   = luaCommon + luaRenewLease
)
