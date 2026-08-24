#!/usr/bin/env bash
# =============================================================================
# 聊天/好友全链路 E2E（双 hy 实时会话）
#
# 前置：
#   - 3 节点运行：config/{all,gate,account}.toml
#   - postgres/redis 可用；gclient hy 已构建
# 用法：
#   bash build/script/e2e_chat.sh
# 环境变量：HY ACCOUNT_URL PGPASSWORD WAIT_TIMEOUT
# =============================================================================
set -euo pipefail

HY="${HY:-$HOME/workspace/gclient_github/bin/hy}"
ACCOUNT_URL="${ACCOUNT_URL:-http://127.0.0.1:18080}"
export PGPASSWORD="${PGPASSWORD:-@zyc0131}"
WAIT_TIMEOUT="${WAIT_TIMEOUT:-15}"
STAMP="$(date +%s%N)"
UID_A="e2e_chat_a_${STAMP}"
UID_B="e2e_chat_b_${STAMP}"
WORLD_MSG="e2e_world_${STAMP}"
PRIVATE_MSG="e2e_private_${STAMP}"
OUT_DIR="/tmp/e2e_chat_${STAMP}"
mkdir -p "$OUT_DIR"

say() { printf '\033[36m[E2E]\033[0m %s\n' "$*"; }
die() { printf '\033[31m[E2E FAIL]\033[0m %s\n' "$*" >&2; exit 1; }

[[ -x "$HY" ]] || die "hy 不可执行：$HY"
command -v psql >/dev/null || die "找不到 psql"

set_client_value() {
  local client=$1 suffix=$2 value=$3
  printf -v "${client}_${suffix}" '%s' "$value"
}

client_value() {
  local client=$1 suffix=$2 variable
  variable="${client}_${suffix}"
  printf '%s' "${!variable-}"
}

client_log() {
  client_value "$1" LOG
}

mark_log() {
  local log
  log=$(client_log "$1")
  wc -c <"$log" | tr -d ' '
}

log_since() {
  local client=$1 offset=$2 log
  log=$(client_log "$client")
  tail -c "+$((offset + 1))" "$log"
}

print_client_log() {
  local client=$1 log
  log=$(client_log "$client")
  printf '\n--- client %s log (last 40 lines) ---\n' "$client" >&2
  if [[ -f "$log" ]]; then
    tail -n 40 "$log" >&2
  else
    printf '<log unavailable>\n' >&2
  fi
  printf '%s\n' '--- end log ---' >&2
}

wait_log() {
  local client=$1 pattern=$2 offset=$3
  local deadline=$((SECONDS + WAIT_TIMEOUT)) pid
  while ((SECONDS < deadline)); do
    if log_since "$client" "$offset" | grep -Eq -- "$pattern"; then
      return 0
    fi
    pid=$(client_value "$client" PID)
    if [[ -n "$pid" ]] && ! kill -0 "$pid" 2>/dev/null; then
      print_client_log "$client"
      die "客户端 $client 提前退出，等待：$pattern"
    fi
    sleep 0.1
  done
  print_client_log "$client"
  die "客户端 $client 等待超时：$pattern"
}

send() {
  local client=$1 command=$2 fd
  fd=$(client_value "$client" FD)
  [[ "$fd" =~ ^[0-9]+$ ]] || die "客户端 $client 未启动"
  printf '%s\n' "$command" >&"$fd"
}

stop_client() {
  local client=$1 fd pid
  fd=$(client_value "$client" FD)
  pid=$(client_value "$client" PID)

  if [[ "$fd" =~ ^[0-9]+$ ]]; then
    printf 'quit\n' >&"$fd" 2>/dev/null || true
  fi
  if [[ "$pid" =~ ^[0-9]+$ ]] && kill -0 "$pid" 2>/dev/null; then
    sleep 0.2
    if kill -0 "$pid" 2>/dev/null; then
      kill "$pid" 2>/dev/null || true
    fi
  fi
  if [[ "$pid" =~ ^[0-9]+$ ]]; then
    wait "$pid" 2>/dev/null || true
  fi
  if [[ "$fd" =~ ^[0-9]+$ ]]; then
    exec {fd}>&-
  fi
  set_client_value "$client" FD ""
  set_client_value "$client" PID ""
}

start_client() {
  local client=$1 uid=$2 fifo log fd pid role_id
  stop_client "$client"
  fifo="$OUT_DIR/${client}.in"
  log="$OUT_DIR/${client}.log"
  rm -f "$fifo"
  mkfifo "$fifo"
  : >"$log"
  exec {fd}<>"$fifo"

  "$HY" --account-server="$ACCOUNT_URL" --platform=guest --client-version=1.0.0 \
    <&"$fd" >"$log" 2>&1 &
  pid=$!
  set_client_value "$client" FD "$fd"
  set_client_value "$client" PID "$pid"
  set_client_value "$client" FIFO "$fifo"
  set_client_value "$client" LOG "$log"

  printf '%s\n' "$uid" >&"$fd"
  wait_log "$client" 'login ok\.' 0
  role_id=$(sed -n 's/.*prelogin ok, role_id=\([0-9][0-9]*\).*/\1/p' "$log" | tail -n 1)
  [[ -n "$role_id" ]] || {
    print_client_log "$client"
    die "客户端 $client 未取得 role_id"
  }
  set_client_value "$client" ROLE_ID "$role_id"
}

cleanup() {
  local status=$?
  trap - EXIT INT TERM
  stop_client A || true
  stop_client B || true
  rm -rf -- "$OUT_DIR"
  exit "$status"
}
trap cleanup EXIT INT TERM

say "玩家A=$UID_A 玩家B=$UID_B"
start_client A "$UID_A"
start_client B "$UID_B"
A_ROLE_ID=$(client_value A ROLE_ID)
B_ROLE_ID=$(client_value B ROLE_ID)
[[ -n "$A_ROLE_ID" && -n "$B_ROLE_ID" && "$A_ROLE_ID" != "$B_ROLE_ID" ]] || die "role_id 无效"
say "1. 双客户端登录 A=$A_ROLE_ID B=$B_ROLE_ID ✓"
