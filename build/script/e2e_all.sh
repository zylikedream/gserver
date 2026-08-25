#!/usr/bin/env bash
# =============================================================================
# GServer 真实 E2E 统一编排：启动 account/gate/game 三节点 → chat + guild 回归。
#
# 前置：
#   - postgres/redis/consul 可用
#   - 已构建根 bin/hy（make build 或 make e2e 自动构建）
# 用法：
#   bash build/script/e2e_all.sh
# 环境变量：HY ACCOUNT_URL PGPASSWORD WAIT_TIMEOUT E2E_OUT_DIR
# =============================================================================
set -euo pipefail

export PGPASSWORD="${PGPASSWORD:-@zyc0131}"
ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
STAMP=$(date +%s%N)
OUT_DIR="${E2E_OUT_DIR:-$ROOT_DIR/.e2e-out/$STAMP}"
mkdir -p "$OUT_DIR/logs"

say() { printf '\033[36m[E2E]\033[0m %s\n' "$*"; }
die() { printf '\033[31m[E2E FAIL]\033[0m %s\n' "$*" >&2; exit 1; }

# ---- 前置检查（每项独立失败退出）----
[[ -x "$ROOT_DIR/bin/hy" ]] || die "缺少 bin/hy，请先 make build"
command -v psql >/dev/null || die "找不到 psql"
command -v go >/dev/null || die "找不到 go"
if command -v redis-cli >/dev/null 2>&1; then
  redis-cli -h 127.0.0.1 -a "${PGPASSWORD:-@zyc0131}" ping >/dev/null 2>&1 || die "redis 不可用"
else
  # 无 redis-cli 环境(如 CI runner):仅探测端口连通
  timeout 2 bash -c 'echo > /dev/tcp/127.0.0.1/6379' 2>/dev/null || die "redis 端口不可达"
fi
psql -h 127.0.0.1 -U postgres -d gserver -tAc "SELECT 1;" >/dev/null 2>&1 || die "postgres 不可用"
curl -fsS http://127.0.0.1:8500/v1/status/leader >/dev/null 2>&1 || die "consul 不可用"

# ---- 端口占用检查：拒绝误杀已有服务 ----
check_port_free() {
  local port=$1 name=$2
  if (ss -ltn 2>/dev/null || netstat -ltn 2>/dev/null) | grep -qE ":$port +[0-9].*LISTEN|LISTEN.*:$port "; then
    die "端口 $port($name) 已被占用，拒绝启动；请先停止已有服务"
  fi
}
check_port_free 18080 account
check_port_free 11086 gate
check_port_free 25101 game

# ---- 启动并等待三节点 ----
NODE_PIDS=()
start_node() {
  local name=$1 config=$2 ready_port=$3
  say "启动 $name($config)..."
  setsid go run node/main.go --config "config/$config" >"$OUT_DIR/logs/$name.log" 2>&1 &
  NODE_PIDS+=("$!")
}
wait_node_ready() {
  local name=$1 log_file=$2 ready_port=$3
  local deadline=$((SECONDS + 60))
  while ((SECONDS < deadline)); do
    if grep -q 'node start success' "$log_file" 2>/dev/null; then
      if [[ -z "$ready_port" ]] || (ss -ltn 2>/dev/null || netstat -ltn 2>/dev/null) | grep -q ":$ready_port "; then
        return 0
      fi
    fi
    if ! kill -0 "${NODE_PIDS[0]}" 2>/dev/null && [[ ${#NODE_PIDS[@]} -gt 0 ]]; then
      break
    fi
    sleep 0.5
  done
  die "节点 $name ready 超时，最后 100 行日志：\n$(tail -n 100 "$log_file" 2>/dev/null || echo '<log missing>')"
}

cleanup() {
  local status=$?
  trap - EXIT INT TERM
  for pid in "${NODE_PIDS[@]:-}"; do
    if [[ -n "$pid" ]] && kill -0 "$pid" 2>/dev/null; then
      kill -- "-$pid" 2>/dev/null || kill "$pid" 2>/dev/null || true
    fi
  done
  if ((status == 0)); then
    rm -rf -- "$OUT_DIR"
  else
    printf 'E2E 输出保留在 %s\n' "$OUT_DIR" >&2
  fi
  exit "$status"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

cd "$ROOT_DIR"
start_node account account.toml 18080
wait_node_ready account "$OUT_DIR/logs/account.log" 18080
start_node gate gate.toml 11086
wait_node_ready gate "$OUT_DIR/logs/gate.log" 11086
start_node game all.toml 25101
wait_node_ready game "$OUT_DIR/logs/game.log" 25101
say "三节点就绪"

# ---- 业务回归 ----
say "运行聊天 E2E..."
HY="$ROOT_DIR/bin/hy" bash build/script/e2e_chat.sh || die "聊天 E2E 失败"
say "运行公会 E2E..."
HY="$ROOT_DIR/bin/hy" bash build/script/e2e_guild.sh || die "公会 E2E 失败"

say "========== E2E ALL PASS =========="
