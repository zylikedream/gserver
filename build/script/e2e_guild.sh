#!/usr/bin/env bash
# =============================================================================
# 公会全流程 E2E(可重复, 管道模式)
#
# 覆盖: 建会(金币+等级前置) → 申请 → 批量审批(含无效 ID 跳过, 验证 #17)
#       → 入会验证 → 同 uid 重连持久化 → DB 断言 → 踢人+解散清理
#
# 前置:
#   - 3 节点运行: go run node/main.go --config config/{all,gate,account}.toml
#   - postgres/redis 可用; hy 客户端已构建(bin/hy, 修复了管道预读 bug)
# 用法:
#   bash build/script/e2e_guild.sh
# 环境变量: HY(hy 路径) ACCOUNT_URL PGPASSWORD
# =============================================================================
set -euo pipefail

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
HY="${HY:-$ROOT_DIR/bin/hy}"
ACCOUNT_URL="${ACCOUNT_URL:-http://127.0.0.1:18080}"
PGPASSWORD="${PGPASSWORD:-@zyc0131}"
STAMP=$(date +%s)
UID_A="e2e_guild_a_${STAMP}"
UID_B="e2e_guild_b_${STAMP}"
GNAME="E2E公会${STAMP}"
OUT_DIR="/tmp/e2e_guild_${STAMP}"
mkdir -p "$OUT_DIR"

say() { printf '\033[36m[E2E]\033[0m %s\n' "$*"; }
die() { printf '\033[31m[E2E FAIL]\033[0m %s\n' "$*"; exit 1; }

# hy 管道会话: $1=uid, 其余为命令序列
hy() {
  local uid=$1
  shift
  {
    printf '%s\n' "$uid"
    for c in "$@"; do printf '%s\n' "$c"; done
    printf 'quit\n'
  } | "$HY" --account-server="$ACCOUNT_URL" --platform=guest --client-version=1.0.0
}

say "玩家A=$UID_A 玩家B=$UID_B 公会=$GNAME"

# ---- 1. A: 金币 + 等级 + 建会(need_approval=1) ----
out=$(hy "$UID_A" \
  'gm.command "add_goods 1 10000"' \
  'gm.command "set_player_level 15"' \
  "guild.create \"$GNAME\" \"e2e_desc\" \"icon\" 1")
guild_id=$(echo "$out" | grep -o '"guildId":"[0-9]*"' | head -1 | grep -oE '[0-9]+' || true)
[ -n "$guild_id" ] || die "建会失败: $(echo "$out" | grep -i 'ack error' | tail -1)"
say "1. 建会成功 guild_id=$guild_id"

# ---- 2. B: 申请 ----
out=$(hy "$UID_B" "guild.apply $guild_id")
echo "$out" | grep -q "RspGuildApply" || die "B 申请失败"
say "2. B 申请 ✓"

# ---- 3. A: 批量审批 [0(无效), 1(有效)]: 无效跳过, 有效入会 ----
out=$(hy "$UID_A" "guild.approve_apply 1 0 1")
echo "$out" | grep -q "RspGuildApproveApply" || die "审批失败: $(echo "$out" | grep -i 'ack error' | tail -1)"
say "3. 批量审批(无效跳过) ✓"

# ---- 4. B: 入会验证 ----
out=$(hy "$UID_B" "guild.info")
echo "$out" | grep -q "\"id\":\"${guild_id}\"" || die "B 未入会"
say "4. B 入会验证 ✓"

# ---- 5. B 同 uid 重连: 公会关系持久化 ----
out=$(hy "$UID_B" "guild.info")
echo "$out" | grep -q "\"id\":\"${guild_id}\"" || die "B 重连后公会关系丢失"
say "5. B 重连持久化 ✓"

# ---- 6. DB 断言: role_guild 2 条 ----
cnt=$(psql -h 127.0.0.1 -U postgres -d gserver -tAc \
  "SELECT count(*) FROM role_guild WHERE guild_id=${guild_id};")
[ "$cnt" = "2" ] || die "role_guild 应为 2 条, 实际 $cnt"
say "6. DB role_guild=$cnt 条 ✓"

# ---- 7. 清理: A 踢 B → 解散 → DB 归零 ----
b_role_id=$(psql -h 127.0.0.1 -U postgres -d gserver -tAc \
  "SELECT role_id FROM role_guild WHERE guild_id=${guild_id} AND role_id <> (SELECT leader_id FROM guild WHERE id=${guild_id}) LIMIT 1;" 2>/dev/null || true)
[ -n "$b_role_id" ] || b_role_id=$(psql -h 127.0.0.1 -U postgres -d gserver -tAc \
  "SELECT role_id FROM role_guild WHERE guild_id=${guild_id} LIMIT 1;")
hy "$UID_A" "guild.kick $b_role_id" "guild.disband" >/dev/null 2>&1
cnt=$(psql -h 127.0.0.1 -U postgres -d gserver -tAc \
  "SELECT count(*) FROM role_guild WHERE guild_id=${guild_id};")
[ "$cnt" = "0" ] || die "清理失败: role_guild 残留 $cnt 条"
say "7. 清理完成(踢人+解散) ✓"

say "========== PASS =========="
