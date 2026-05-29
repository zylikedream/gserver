#!/bin/bash
# 初始化开发环境
# 用法: ./build/script/svr_init.sh <env_name>
# 示例: ./build/script/svr_init.sh dev     (使用 build/env/dev.env.toml)
# 示例: ./build/script/svr_init.sh dev_zyr (使用 build/env/dev_zyr.env.toml)

set -e

if [ -z "$1" ]; then
    echo "用法: $0 <env_name>"
    echo "示例: $0 dev       # 使用 dev.env.toml"
    echo "示例: $0 dev_zyr   # 使用 dev_zyr.env.toml"
    exit 1
fi

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ENV_NAME="$1"
ENV_FILE="$SCRIPT_DIR/../env/$ENV_NAME.env.toml"

if [ ! -f "$ENV_FILE" ]; then
    echo "错误: 环境文件不存在: $ENV_FILE"
    echo "提示: cp build/env/dev.env.toml build/env/$ENV_NAME.env.toml"
    exit 1
fi

echo "=== gserver 环境初始化 ==="
echo "环境: $ENV_NAME"
echo "文件: build/env/$ENV_NAME.env.toml"
echo ""

# 生成配置
echo ">>> 生成配置..."
python3 "$SCRIPT_DIR/gen_config.py" "$ENV_NAME"

echo ""
echo "=== 完成 ==="
echo "运行: go run node/main.go --config config/account.toml"
echo "运行: go run node/main.go --config config/gate.toml"
echo "运行: go run node/main.go --config config/all.toml"
