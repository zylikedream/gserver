# 压测记录：<YYYY-MM-DD> <run-name>

> 状态：`计划中` / `通过` / `不通过` / `部分完成` / `无效`
>
> 创建时间：<YYYY-MM-DD HH:mm:ss TZ>
> 运行时间：<YYYY-MM-DD HH:mm:ss TZ> — <YYYY-MM-DD HH:mm:ss TZ>

## 1. 实验定义

- **类型**：链路冒烟 / 限流分支验证 / 容量测试 / 稳态测试 / 回归比较
- **目标**：
- **通过标准**：
- **不在本次范围内**：

## 2. 环境

| 项目 | 值 |
|---|---|
| Git commit | `<commit>` |
| 分支 | `<branch>` |
| 环境 | 本地 / 测试 / 云上；区域：`<region>` |
| 服务拓扑 | `<account/gate/role/chat/friend/guild>` |
| Gate 地址 | `<host:port>` |
| Account prelogin | `<base-url>`（不含凭据） |
| 数据库/缓存 | `<版本与实例信息>` |
| 数据状态 | 新建 / 已有数据 / 已重置；范围：`<scope>` |
| 机器规格 | `<CPU/内存/实例>` |

## 3. 服务端参数

记录实际生效的完整相关配置，不只记录“默认”。配置通过 `./build/script/svr_init.sh <env>` 生成时，写明环境名和生成时间。

```toml
[login_limit]
enabled = <true|false>
rate = <number>
burst = <integer>
max_inflight = <integer>
queue_size = <integer>
wait_timeout = "<duration>"
```

其他影响本次结果的配置：

- `<配置项>`：`<值>`，原因：`<为什么设置>`

## 4. 客户端参数

### 4.1 bench 配置

- 配置文件：`<path>`
- 配置 checksum：`<sha256>`（可选但推荐）
- 账号前缀：`<unique-prefix>`
- 机器人类型与权重：`<id: weight, ...>`
- Chat mixin：`<chance/channel>` 或 `未启用`
- 完整 YAML：

```yaml
addr: "<host:port>"
account_server: "<base-url>"
platform: "<platform>"
account_pattern: "<unique-prefix>_%d"
total_bots: <integer>
startup_rate: <integer>/s
report_interval: <duration>
silent: <true|false>
# bot_types 与脚本必须记录完整内容或指向不可变文件
```

### 4.2 分阶段负载

| 阶段 | Bots | 启动速率 | 预热 | 稳态时长 | 实际开始/结束 | 停止方式 |
|---|---:|---:|---:|---:|---|---|
| Smoke | `<n>` | `<n>/s` | `<duration>` | `<duration>` | `<timestamps>` | SIGINT / timeout |

### 4.3 实际命令

```bash
<完整启动命令>
```

- bench 输出：`<path>`
- 停止信号：`<SIGINT/SIGTERM>`
- 退出状态：`<code>`
- 是否提前停止：否 / 是，原因：`<reason>`

## 5. 观测与证据

所有查询写明数据源、完整语句、选择时间范围、执行时间和返回状态。不要只粘贴截图而省略查询。

### 5.1 Prometheus

```promql
<query>
```

- 数据源：`<Prometheus URL/name>`
- 时间范围：`<absolute range or $__range expansion>`
- 结果：

| 指标 | 结果 | 单位 | 备注 |
|---|---:|---|---|
| 服务存活 | `<value>` | 0/1 | `<node>` |
| 在线峰值 | `<value>` | players | `<window>` |
| 登录成功 | `<value>` | requests | `ok` |
| 登录限流 | `<value>` | requests | `rate_limited` |
| 排队满 | `<value>` | requests | `queue_full` |
| 排队超时 | `<value>` | requests | `queue_timeout` |
| 登录错误 | `<value>` | requests | `error` |
| 协议非 OK | `<value>` | requests | `<msg_name>` |
| 协议错误 | `<value>` | requests | `<msg_name>` |

### 5.2 Grafana/Loki

- 数据源：`<name>`
- 查询：

```logql
{job="gserver"} |= `<message or term>`
```

- 时间范围：`<range>`
- 结果：无匹配 / 返回 `<n>` 行 / 查询错误（原文：`<error>`）
- 关键日志与上下文：

### 5.3 资源与服务状态

| 时间 | 服务/主机 | CPU | 内存 | DB/Redis/Consul | 重启/错误 |
|---|---|---:|---:|---|---|
| `<timestamp>` | `<target>` | `<value>` | `<value>` | `<observation>` | `<observation>` |

## 6. 结果

### 6.1 负载达成

- 计划 bots：`<n>`；实际启动：`<n>`；实际在线峰值：`<n>`
- 计划启动速率：`<n>/s`；实际：`<n>/s` 或 `未测`
- 稳态窗口：`<start/end>`；有效时长：`<duration>`

### 6.2 延迟与错误

| 项目 | 结果 | 目标 | 判定 |
|---|---:|---:|---|
| 登录 p50 | `<value>` | `<target>` | 通过/不通过/未测 |
| 登录 p95 | `<value>` | `<target>` | 通过/不通过/未测 |
| 登录 p99 | `<value>` | `<target>` | 通过/不通过/未测 |
| 客户端错误 | `<value>` | `<target>` | 通过/不通过/未测 |
| 服务重启 | `<count>` | 0 | 通过/不通过 |

### 6.3 异常时间线

- `<timestamp>`：`<observation>` → `<impact/evidence>`

## 7. 结论

**结论：通过 / 不通过 / 部分完成 / 无效**

- 结论依据：
- 已确认事实：
- 未测或不确定项：
- 是否可以作为容量基线：是 / 否；原因：

## 8. 清理与后续

- bench/服务清理：`<what and when>`
- 临时配置/凭据清理：`<what and when>`
- 数据重置/保留：`<scope>`
- 保留的证据：`<logs/metrics/config paths>`
- 后续实验或修复：`<action — owner — condition>`
