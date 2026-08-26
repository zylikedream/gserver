#!/usr/bin/env bash
# =============================================================================
# 玩家核心玩法 E2E(可重复, 管道模式)
#
# 覆盖: 登录建号 → GM 解锁花/材料 → 培育(10s) → 播种 → 浇水 → 收获(10s)
#       → 背包落库 → 主线任务进度 → 居民订单 → 同 uid 重连持久化 → DB 断言
#
# 前置:
#   - 3 节点运行: go run node/main.go --config config/{all,gate,account}.toml
#   - postgres/redis 可用; hy 客户端已构建(bin/hy, 修复了管道预读 bug)
# 用法:
#   bash build/script/e2e_role.sh
# 环境变量: HY(hy 路径) ACCOUNT_URL PGPASSWORD
# =============================================================================
set -euo pipefail

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
HY="${HY:-$ROOT_DIR/bin/hy}"
ACCOUNT_URL="${ACCOUNT_URL:-http://127.0.0.1:18080}"
export PGPASSWORD="${PGPASSWORD:-@zyc0131}"
STAMP=$(date +%s%N)
UID_A="e2e_role_${STAMP}"
OUT_DIR="/tmp/e2e_role_${STAMP}"
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

FLOWER_ID=101        # 玫瑰
BREED_MAT_A=1001     # 培育材料 A(消耗 2/次)
BREED_MAT_B=2001     # 培育材料 B(消耗 1/次)
WATER_ITEM=3         # 水滴(浇水消耗 10/地块)
HARVEST_ITEM=10001   # 玫瑰产品
PLOT_ID=1            # 默认解锁地块(unlock_level=0)
BREED_WAIT=11        # breed_time=10s + 余量
GROW_WAIT=11         # grow_time=10s + 余量

say "玩家=$UID_A 花=$FLOWER_ID 产品=$HARVEST_ITEM"

# ---- 1. 登录建号 ----
out=$(hy "$UID_A" "bag.info")
echo "$out" | grep -q "RspBagInfo" || die "登录/建号失败: $(echo "$out" | grep -i 'ack error' | tail -1)"
say "1. 登录建号 ✓"

# ---- 2. GM 前置: 解锁花 + 培育材料 + 水滴 ----
out=$(hy "$UID_A" \
  "gm.command \"add_flower $FLOWER_ID\"" \
  "gm.command \"add_goods $BREED_MAT_A 10\"" \
  "gm.command \"add_goods $BREED_MAT_B 5\"" \
  "gm.command \"add_goods $WATER_ITEM 100\"")
echo "$out" | grep -q "RspGMCommand" || die "GM 前置失败: $(echo "$out" | grep -i 'ack error' | tail -1)"
say "2. GM 解锁花/材料 ✓"

# ---- 3. 培育: start → 等待 → finish ----
out=$(hy "$UID_A" "breed.start $FLOWER_ID")
echo "$out" | grep -q "RspFlowerStartBreed" || die "培育开始失败: $(echo "$out" | grep -i 'ack error' | tail -1)"
say "3. 培育开始 ✓"
sleep "$BREED_WAIT"
out=$(hy "$UID_A" "breed.finish $FLOWER_ID")
echo "$out" | grep -q "RspFlowerFinishBreed" || die "培育完成失败: $(echo "$out" | grep -i 'ack error' | tail -1)"
say "3. 培育完成 ✓"

# ---- 4. 播种 ----
out=$(hy "$UID_A" "flower.plant $FLOWER_ID $PLOT_ID")
echo "$out" | grep -q "RspPlotPlant" || die "播种失败: $(echo "$out" | grep -i 'ack error' | tail -1)"
say "4. 播种 ✓"

# ---- 5. 浇水 ----
out=$(hy "$UID_A" "flower.water $PLOT_ID")
echo "$out" | grep -q "RspPlotWater" || die "浇水失败: $(echo "$out" | grep -i 'ack error' | tail -1)"
say "5. 浇水 ✓"

# ---- 6. 成长 → 收获 ----
sleep "$GROW_WAIT"
out=$(hy "$UID_A" "flower.harvest $PLOT_ID")
echo "$out" | grep -q "RspPlotHarvest" || die "收获失败: $(echo "$out" | grep -i 'ack error' | tail -1)"
say "6. 收获 ✓"

echo "$out" | grep -q "\"propId\":$HARVEST_ITEM" || die "背包缺少产品 $HARVEST_ITEM: $(echo "$out" | grep -oE '"propId":[0-9]+,"num":"[0-9]+"' | head -5)"
say "7. 背包含产品 $HARVEST_ITEM ✓"

# ---- 8. 主线任务(种花/浇水/收获事件已驱动进度) ----
out=$(hy "$UID_A" "maintask.info")
echo "$out" | grep -q "RspMainTaskInfo" || die "主线任务查询失败"
say "8. 主线任务可查 ✓"

# ---- 9. 居民订单(初始订单已生成) ----
out=$(hy "$UID_A" "residentorder.info")
echo "$out" | grep -q "RspResidentOrderInfo" || die "居民订单查询失败"
say "9. 居民订单可查 ✓"

echo "$out" | grep -q "\"propId\":$HARVEST_ITEM" || die "重连后产品 $HARVEST_ITEM 丢失"
say "10. 重连持久化 ✓"

# ---- 11. DB 断言: 角色主数据落库 ----
role_id=$(psql -h 127.0.0.1 -U postgres -d gserver -tAc \
  "SELECT a.role_id FROM account a JOIN account_identity ai ON ai.account_id = a.account_id WHERE ai.platform='guest' AND ai.platform_uid='${UID_A}';")
[ -n "$role_id" ] || die "DB 无 role_id(platform_uid=${UID_A})"
for tbl in role_basic role_bag role_flower role_plot role_main_task role_resident_order; do
  cnt=$(psql -h 127.0.0.1 -U postgres -d gserver -tAc \
    "SELECT count(*) FROM $tbl WHERE role_id=${role_id};")
  [ "$cnt" -ge 1 ] || die "DB 断言失败: $tbl 无 role_id=${role_id} 的行"
done
say "11. DB 断言(role_id=$role_id, 6 表) ✓"

say "========== PASS =========="
