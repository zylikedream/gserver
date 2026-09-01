# 压测记录：2026-09-02 login-capacity-medium

> 状态：`通过`
>
> 创建时间：2026-09-02 00:51:36 CST
> 运行时间：2026-09-02 00:52:31 CST — 2026-09-02 01:03:06 CST（计划流量窗口 10m，停止后连接清理完成）

## 1. 实验定义

- **类型**：容量测试 Medium 阶段
- **目标**：在当前 `pressure_zy` 环境下，以 300 个 bot、30/s 启动速率运行 10 分钟，验证比 Smoke 更高负载下的登录、基础业务链路和服务稳定性。
- **通过标准**：300 个 bot 完成启动；10 分钟窗口内服务持续存活；结束后登录 inflight/queue 回落到 0；没有未解释的服务重启或持续错误。
- **不在本次范围内**：最终容量上限、饱和点、CPU/内存/数据库连接的完整资源基线。

## 2. 环境

| 项目 | 值 |
|---|---|
| Git commit | `df0654b` |
| 分支 | `fix/grafana-p95-range` |
| 环境配置 | `pressure_zy` |
| 环境文件 | `build/env/pressure_zy.env.toml`（本地忽略文件） |
| 日志级别 | `info`，关闭 Debug，保留 Info/Warn/Error |
| 环境 | 本地 WSL2 |
| 服务拓扑 | account + gate + role + chat + friend + guild |
| Gate 地址 | `127.0.0.1:11086` |
| Account prelogin | `http://127.0.0.1:18080` |
| Prometheus | `http://127.0.0.1:9092` |
| 数据库/缓存 | 本地 PostgreSQL + Redis + Consul |
| 数据状态 | 现有本地测试数据；使用唯一账号前缀 |
| 机器规格 | 未记录 |

前置检查结果：

- 6 个 systemd 用户服务均为 active/running。
- `up{job="game-services"}`：account、gate、role、chat、friend、guild 全部为 `1`。
- `login_inflight{node="gate"}`：`0`。
- `login_queue_length{node="gate"}`：`0`。
- 压测前 `online_players{node="gate"}`：`0`。
- 压测前登录计数器因本次服务重启未返回序列。

## 3. 服务端参数

本轮使用 `pressure_zy` 生成的 Gate 配置：

```toml
[login_limit]
enabled = true
rate = 200.0
burst = 400
max_inflight = 100
queue_size = 500
wait_timeout = "3s"

[log]
level = "info"
```

生成命令：

```bash
./build/script/svr_init.sh pressure_zy
```

## 4. 客户端参数

### 4.1 bench 配置

- 配置文件：`/tmp/gserver-pressure-2026-09-02-medium.yaml`
- 配置 checksum：`c54326459ced85e6e207107411411e254ff0447ec80c23009e62431df12c44fc`
- 账号前缀：`pressure_20260902_medium_%d`
- 机器人类型与权重：`newbie: 40`、`planter: 25`、`order: 15`
- Chat mixin：`chance=0.1`、`channel=1`
- 完整 YAML：保留在上述临时配置文件；关键字段如下：

```yaml
addr: "127.0.0.1:11086"
account_server: "http://127.0.0.1:18080"
platform: "guest"
account_pattern: "pressure_20260902_medium_%d"
total_bots: 300
startup_rate: 30
report_interval: 5s
silent: true
```

业务脚本使用 `client/cmd/bench/bench.yaml` 的 newbie、planter、order 脚本和 chat mixin，未改变脚本逻辑。

### 4.2 负载阶段

| 阶段 | Bots | 启动速率 | 预热 | 稳态时长 | 实际开始/结束 | 停止方式 |
|---|---:|---:|---:|---:|---|---|
| Medium | 300 | 30/s | 未单独设置 | 10m | 00:52:31 — 01:02:31 CST | `SIGINT` via `timeout` |

### 4.3 实际命令

```bash
timeout -s INT -k 30s 10m ./bin/bench -config /tmp/gserver-pressure-2026-09-02-medium.yaml \
  2>&1 | tee /tmp/gserver-pressure-2026-09-02-login-capacity-medium.log
```

- bench 输出：`/tmp/gserver-pressure-2026-09-02-login-capacity-medium.log`
- 停止信号：`SIGINT`，由 `timeout` 在 10m 时发送
- 退出状态：`124`；这是 GNU `timeout` 达到时限后的预期返回值
- 是否提前停止：否；按计划触发停止

## 5. 观测与证据

### 5.1 Prometheus

- 数据源：`http://127.0.0.1:9092`
- 流量窗口：2026-09-02 00:52:31 — 01:02:31 CST
- 边界查询时间：`1788281551` — `1788282151`

实际查询：

```promql
min_over_time(up{job="game-services"}[12m])
max_over_time(sum(online_players{node="gate"})[12m:])
login_limit_total
client_requests_total{result!="ok"}
```

结果：

| 指标 | 结果 | 单位 | 备注 |
|---|---:|---|---|
| 服务存活 | 6/6 | nodes | account、gate、role、chat、friend、guild 均为 `1` |
| 在线峰值 | 300 | players | Gate |
| 登录成功 | 300 | requests | `login_limit_total{result="ok"}` 末值；压测前无序列 |
| 登录限流 | 未返回 | requests | `rate_limited` |
| 排队满 | 未返回 | requests | `queue_full` |
| 排队超时 | 未返回 | requests | `queue_timeout` |
| 登录错误 | 未返回 | requests | `error` |
| `ReqPlotWater` 非 OK | 380 | requests | `result="limited"` 末值；压测前无序列 |
| 协议错误 | 0 | requests | `ReqFlowerStartBreed`；压测前后均为 0 |

延迟查询（在 `1788282151` 评估）：

```promql
histogram_quantile(0.50, sum by (le) (rate(client_request_duration_seconds_bucket[5m])))
histogram_quantile(0.95, sum by (le) (rate(client_request_duration_seconds_bucket[5m])))
histogram_quantile(0.99, sum by (le) (rate(client_request_duration_seconds_bucket[5m])))
```

结果：p50=`2.50ms`、p95=`4.75ms`、p99=`4.95ms`。

结束后复核：`online_players{node="gate"}=0`、`login_inflight{node="gate"}=0`、`login_queue_length{node="gate"}=0`。

### 5.2 Grafana/Loki

- 数据源：本地 Loki `http://127.0.0.1:3100`
- 时间范围：2026-09-02 00:52:31 — 01:03:06 CST
- 查询：

```logql
{job="gserver"} | json | level="error"
{job="gserver"} |= `handle client protocol failed`
```

- 两个查询均返回 `0` 行。
- 未发现真实 `level="error"` 日志或 `handle client protocol failed`。

### 5.3 资源与服务状态

| 时间 | 服务/主机 | CPU | 内存 | DB/Redis/Consul | 重启/错误 |
|---|---|---:|---:|---|---|
| 压测窗口 | 全部服务 | 未测 | 未测 | 6 个 target 持续 `up=1` | 未发现服务重启 |

bench 日志显示 300 个 bot 全部启动；停止前多次报告为 `alive: 300/300`。最终报告为 `alive: 0/300, dead: 300`，对应主动关闭连接。

- 计划 bots：300；实际启动：300；实际在线峰值：300
- 计划启动速率：30/s；实际启动耗时：10.000407874s
- 稳态窗口：00:52:31 — 01:02:31 CST；有效时长：10m
- 登录成功：300；登录限流/排队满/排队超时/登录错误：未返回对应序列
- 协议非 OK：`ReqPlotWater limited` 末值 380；这是已有业务限流结果
- 协议错误：`ReqFlowerStartBreed error` 增量 0
- 请求延迟：p50 2.50ms、p95 4.75ms、p99 4.95ms
- 服务重启：未发现；6 个服务在窗口内均为 `up=1`

## 7. 结论

**结论：通过。**

本轮 Medium 阶段满足既定标准：300 个 bot 全部启动并保持在线 10 分钟，6 个服务持续存活，300 次登录成功，结束后在线数、登录 inflight 和登录队列均回落到 0；Loki 未发现真实 error 级别日志或 handler failed 日志。

`ReqPlotWater limited` 共 380 次是业务限流结果，不等同于系统故障。CPU、内存、数据库连接资源本轮未测，因此本轮可以作为 `300 bot / 30/s / 10m` 的稳定性基线，但不能推导系统容量上限。

## 8. 清理与后续

- bench：已按计划发送 `SIGINT`；300 个 bot 均完成连接关闭；supervisor 返回 `124`，原因为 `timeout` 达到时限
- 服务：未停止，6 个 systemd 用户服务继续运行
- 临时配置/凭据：未产生凭据；bench 配置保留在 `/tmp` 用于复核
- 数据重置/保留：未重置；本轮新增压力账号和业务数据保留
- 保留的证据：`/tmp/gserver-pressure-2026-09-02-medium.yaml`、`/tmp/gserver-pressure-2026-09-02-login-capacity-medium.log`、本记录中的 Prometheus/Loki 查询与结果
- 后续：今天压测结束，不自动升档到 Target
