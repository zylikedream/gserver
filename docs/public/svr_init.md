# 开发环境初始化

## 前置依赖

- Go 1.25+
- Python 3 + pip（用于配置生成）
- PostgreSQL 18（docker-compose）
- Redis 7（docker-compose）
- Consul 1.15（docker-compose）

安装 Python 依赖：

```bash
pip3 install toml jinja2
```

## 一键初始化

```bash
# 1. 克隆并进入项目
git clone <repo-url>
cd gserver

# 2. 初始化子模块
git submodule update --init

# 3. 启动基础设施（PostgreSQL / Redis / Consul）
docker compose -f deploy/docker/docker-compose.yml up -d

# 4. 创建自己的环境配置
cp build/env/dev.env.toml build/env/dev_<your-name>.env.toml

# 5. 修改数据库密码等参数
vim build/env/dev_<your-name>.env.toml

# 6. 生成配置文件
./build/script/svr_init.sh dev_<your-name>

# 7. 启动服务
go run node/main.go --config config/all.toml
```

## svr_init.sh 做了什么

`./build/script/svr_init.sh <env_name>` 会：

1. 读取 `build/env/<env_name>.env.toml` 中的环境参数
2. 渲染 `build/template/config/` 下的 Jinja2 模板，生成 `config/*.toml`
3. 渲染 `build/template/script/` 下的 Jinja2 模板，生成 `hack/` 中的脚本

## 配置文件说明

| 文件 | 说明 |
|---|---|
| `build/env/dev.env.toml` | 环境参数模板，**不要直接修改** |
| `build/env/dev_<name>.env.toml` | 个人环境配置，已 `.gitignore` |
| `build/template/config/*.toml.template` | 服务配置的 Jinja2 模板 |
| `build/template/script/*.sh.template` | 脚本的 Jinja2 模板 |
| `build/script/gen_config.py` | 配置生成器 |

生成的 `config/*.toml` 和 `hack/*.sh` 已加入 `.gitignore`，不会提交到仓库。

## 重新生成

修改了环境参数或模板后，重新运行：

```bash
./build/script/svr_init.sh dev_<your-name>
```

## 重置数据

```bash
bash hack/db_reset.sh
```

## 启动服务

**最少需要 2 个节点：gate（网关）+ game（逻辑服），gate 是客户端入口，必须先启动。**

### 开发常用（gate + 单节点 all）

```bash
# 终端 1: 启动网关（TCP 连接入口，必须第一个启动）
go run node/main.go --config config/gate.toml

# 终端 2: 启动所有逻辑服务（role/chat/friend/guild 在同一个节点）
go run node/main.go --config config/all.toml
```

### 分开部署（gate + 独立节点）

```bash
# 终端 1: 启动网关
go run node/main.go --config config/gate.toml

# 终端 2: 启动 role
go run node/main.go --config config/role.toml

# 终端 3: 启动 chat
go run node/main.go --config config/chat.toml

# 终端 4: 启动 friend
go run node/main.go --config config/friend.toml

# 终端 5: 启动 guild
go run node/main.go --config config/guild.toml
```
