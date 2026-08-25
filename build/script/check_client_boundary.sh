#!/usr/bin/env bash
# 检查 client module 不依赖 gserver 实现（黑盒 seam 门禁）。
# 1. client/go.mod 不得 require gserver，不得使用 replace 绕过。
# 2. client 下任何 .go 文件不得 import gserver 实现包（AST 级检查）。
set -euo pipefail

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
CLIENT_DIR="$ROOT_DIR/client"

fail() {
  printf 'client boundary check FAILED: %s\n' "$*" >&2
  exit 1
}

# 1) module 配置检查
mod_json=$(go -C "$CLIENT_DIR" mod edit -json)
if echo "$mod_json" | grep -q '"Path": "gserver"'; then
  fail "client/go.mod require gserver"
fi
if echo "$mod_json" | grep -q '"Replace": {'; then
  fail "client/go.mod 存在 replace directive"
fi

# 2) AST import 检查
go run ./build/tools/checkclientboundary -root "$CLIENT_DIR" || fail "client import gserver 实现"

printf 'client boundary OK: no gserver implementation dependency\n'
