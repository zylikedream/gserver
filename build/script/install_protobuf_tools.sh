#!/usr/bin/env bash
# 安装固定版本的 protobuf 工具链（protoc 3.19.3 + protoc-gen-go v1.36.11）。
# 全部装入仓库内 .tools/，不写全局目录。幂等。
set -euo pipefail

ROOT=$(git rev-parse --show-toplevel)
TOOLS_DIR="$ROOT/.tools"
BIN_DIR="$TOOLS_DIR/bin"
INCLUDE_DIR="$TOOLS_DIR/include"
PROTOC_VERSION="3.19.3"
PROTOC_ZIP_SHA256="e7acbd3f3477c593cee58cd995490c0f95dcb4058dd4677d015535fc20a372f3"
GENGO_VERSION="v1.36.11"

mkdir -p "$BIN_DIR" "$INCLUDE_DIR"

# --- protoc ---
if [[ -x "$BIN_DIR/protoc" ]] && "$BIN_DIR/protoc" --version 2>/dev/null | grep -q "3.19.3"; then
  echo "protoc ${PROTOC_VERSION} already installed"
else
  TMP_ZIP="$(mktemp --suffix=.zip)"
  trap 'rm -f "$TMP_ZIP"' EXIT
  echo "downloading protoc ${PROTOC_VERSION}..."
  curl -L --fail --silent --show-error \
    "https://github.com/protocolbuffers/protobuf/releases/download/v${PROTOC_VERSION}/protoc-${PROTOC_VERSION}-linux-x86_64.zip" \
    -o "$TMP_ZIP"
  ACTUAL_SHA=$(sha256sum "$TMP_ZIP" | awk '{print $1}')
  if [[ "$ACTUAL_SHA" != "$PROTOC_ZIP_SHA256" ]]; then
    echo "protoc SHA256 mismatch: got $ACTUAL_SHA, want $PROTOC_ZIP_SHA256" >&2
    exit 1
  fi
  rm -rf "$BIN_DIR/protoc" "$INCLUDE_DIR/google"
  unzip -q -o "$TMP_ZIP" -d "$TOOLS_DIR"
  trap - EXIT
  rm -f "$TMP_ZIP"
  echo "protoc ${PROTOC_VERSION} installed"
fi

# --- protoc-gen-go ---
if [[ -x "$BIN_DIR/protoc-gen-go" ]] && "$BIN_DIR/protoc-gen-go" --version 2>/dev/null | grep -q "v1.36.11"; then
  echo "protoc-gen-go ${GENGO_VERSION} already installed"
else
  echo "installing protoc-gen-go ${GENGO_VERSION}..."
  GOBIN="$BIN_DIR" go install "google.golang.org/protobuf/cmd/protoc-gen-go@${GENGO_VERSION}"
  echo "protoc-gen-go ${GENGO_VERSION} installed"
fi

echo "toolchain ready:"
"$BIN_DIR/protoc" --version
"$BIN_DIR/protoc-gen-go" --version
