# 压测记录：2026-09-02 login-capacity-smoke

> 状态：`通过`
>
> 创建时间：2026-09-02 00:23:53 CST
> 运行时间：2026-09-02 00:25:00 CST — 2026-09-02 00:30:23 CST（流量窗口至 00:30:00）

## 1. 实验定义

- **类型**：容量测试 Smoke 阶段
- **目标**：验证当前本地服务拓扑在 100 个 bench bot、20/s 启动速率下的登录和基础业务链路，建立后续 Medium/Target 阶段的可比基线。
- **通过标准**：100 个 bot 能完成启动；压测期间服务持续存活；结束后登录 inflight/queue 回落到 0；没有未解释的服务重启或持续错误。
- **不在本次范围内**：容量上限、饱和点、长时间稳定性、p95/p99 的正式 SLO 基线。

## 2. 环境

| 项目 | 值 |
|---|---|
| Git commit | `c0fdaf0` |
| 分支 | `fix/grafana-p95-range` |
| 环境 | 本地 WSL2 |
| 服务拓扑 | account + gate + role + chat + friend + guild |
| Gate 地址 | `127.0.0.1:11086` |
| Account prelogin | `http://127.0.0.1:18080` |
| Prometheus | `http://127.0.0.1:9092` |
| 数据库/缓存 | 本地 PostgreSQL + Redis + Consul |
| 数据状态 | 现有本地测试数据；本轮使用唯一账号前缀 |
| 机器规格 | 未记录 |

前置检查结果：

- account、gate、role、chat、friend、guild systemd 用户服务均为 active/running。
- `up{job="game-services"}`：6 个节点均为 `1`。
- `login_inflight`：检查时各节点为 `0`。
- `login_queue_length`：检查时各节点为 `0`。
- Gate 当前 `login_limit`：`enabled=true`、`rate=200.0`、`burst=400`、`max_inflight=100`、`queue_size=500`、`wait_timeout=3s`。
- 压测前 Gate `login_limit_total{result="ok"}`：`1800`；其他结果未在基线快照中返回。
- 压测前 `online_players{node="gate"}`：`0`。

## 3. 服务端参数

本轮使用当前已生成的 Gate 登录限流配置，不做临时覆盖：

```toml
[login_limit]
enabled = true
rate = 200.0
burst = 400
max_inflight = 100
queue_size = 500
wait_timeout = "3s"
```

配置来源：`config/gate.toml`，由当前环境配置生成。配置生成环境名：`dev_zy`。

## 4. 客户端参数

### 4.1 bench 配置

- 配置文件：`/tmp/gserver-pressure-2026-09-02-smoke.yaml`
- 配置 checksum：`4da5250c98fea72bb6a7c95698e05bc2d25f99b7e3a4d2f2ae5c920d355a938d`
- 账号前缀：`pressure_20260902_smoke_%d`
- 机器人类型与权重：`newbie: 40`、`planter: 25`、`order: 15`；实际类型选择逻辑按 bench 配置执行
- Chat mixin：`chance=0.1`、`channel=1`

完整 YAML：

```yaml
addr: "127.0.0.1:11086"
account_server: "http://127.0.0.1:18080"
platform: "guest"
account_pattern: "pressure_20260902_smoke_%d"
total_bots: 100
startup_rate: 20
report_interval: 5s
silent: true

bot_types:
  - id: newbie
    weight: 40
    script:
      - login: {}
      - gm: {cmd: "add_goods 3 10000"}
      - wait_range: {min: 0, max: 5}
      - breed: {flower_id: 101}
      - wait_for_breed: {extra_max: 2}
      - finish_breed: {flower_id: 101}
      - claim_task: {task_id: 1003}
      - plant: {plot_ids: [1], flower_id: 101}
      - claim_task: {task_id: 1004}
      - water: {plot_ids: [1]}
      - claim_task: {task_id: 1005}
      - wait_for_harvest: {extra_max: 2}
      - harvest: {plot_ids: [1]}
      - claim_task: {task_id: 1006}
      - claim_task: {task_id: 1007}
      - loop:
          count: 0
          script:
            - plant_cycle: {plot_max: 3}
            - wait_range: {min: 3, max: 8}

  - id: planter
    weight: 25
    script:
      - login: {}
      - gm: {cmd: "add_goods 3 10000"}
      - ensure_breed: {flower_id: 101}
      - loop:
          count: 0
          script:
            - plant_cycle: {plot_max: 100000}
            - wait_range: {min: 3, max: 8}

  - id: order
    weight: 15
    script:
      - login: {}
      - gm: {cmd: "add_goods 3 10000"}
      - ensure_breed: {flower_id: 101}
      - loop:
          count: 0
          script:
            - check_orders: {}
            - submit_orders: {}
            - plant_cycle: {plot_max: 2}
            - wait_range: {min: 5, max: 15}

chat_mixin:
  chance: 0.1
  channel: 1
  messages:
    - "大家好"
    - "加油种花"
    - "hello ~"
    - "你好"
    - "你好吗"
    - "好无聊"
```

### 4.2 负载阶段

| 阶段 | Bots | 启动速率 | 预热 | 稳态时长 | 实际开始/结束 | 停止方式 |
|---|---:|---:|---:|---:|---|---|
| Smoke | 100 | 20/s | 未单独设置 | 5m | 00:25:00 — 00:30:00 CST | `SIGINT` via `timeout` |

### 4.3 实际命令

```bash
timeout -s INT -k 30s 5m ./bin/bench -config /tmp/gserver-pressure-2026-09-02-smoke.yaml \
  2>&1 | tee /tmp/gserver-pressure-2026-09-02-login-capacity-smoke.log
```

- bench 输出：`/tmp/gserver-pressure-2026-09-02-login-capacity-smoke.log`
- 停止信号：`SIGINT`，由 `timeout` 在 5m 时发送
- 退出状态：`124`；这是 GNU `timeout` 在达到时间限制后的预期返回值，不代表 bench 启动失败
- 是否提前停止：否；按计划触发停止

## 5. 观测与证据

### 5.1 Prometheus

数据源：`http://127.0.0.1:9092`。流量窗口：2026-09-02 00:25:00 — 00:30:00 CST；Prometheus 边界时间：`1788279900` — `1788280200`。

实际查询与结果：

```promql
min_over_time(up{job="game-services"}[6m])
```

结果：account、gate、role、chat、friend、guild 全部为 `1`。

```promql
max_over_time(sum(online_players{node="gate"})[6m:])
```

结果：`100 players`。

登录计数器边界查询：

```promql
login_limit_total
client_requests_total{result!="ok"}
```

结果：

| 指标 | 00:25:00 | 00:30:00 | 窗口增量 |
|---|---:|---:|---:|
| `login_limit_total{result="ok"}` | 1800 | 1900 | +100 |
| `client_requests_total{msg_name="ReqFlowerStartBreed",result="error"}` | 440 | 440 | +0 |
| `client_requests_total{msg_name="ReqPlotWater",result="limited"}` | 1411 | 1511 | +100 |

窗口内未返回 `login_limit_total` 的 `rate_limited`、`queue_full`、`queue_timeout`、`error` 序列。

```promql
histogram_quantile(0.50, sum by (le) (rate(client_request_duration_seconds_bucket[5m])))
histogram_quantile(0.95, sum by (le) (rate(client_request_duration_seconds_bucket[5m])))
histogram_quantile(0.99, sum by (le) (rate(client_request_duration_seconds_bucket[5m])))
```

以上查询在 `00:30:00 CST` 评估，全节点汇总结果：p50=`2.55ms`、p95=`4.85ms`、p99=`17.94ms`。

窗口结束时：`login_inflight{node="gate"}=0`、`login_queue_length{node="gate"}=0`。

### 5.2 Grafana/Loki

- 数据源：本地 Loki `http://127.0.0.1:3100`
- 选择时间：`2026-09-02 00:25:00 — 00:30:00 CST`
- 查询：

```logql
{job="gserver"} | json | level="error"
```

- 结果：`0` 行真实 `level="error"` 日志。
- 补充：`{job="gserver"} |= "error"` 返回的匹配主要是 debug payload 中包含的字符串，不能直接当作错误计数；因此使用 JSON level 过滤确认真实错误级别。
- account 日志在 `00:25:12 — 00:25:14` 记录 100 个压力账号创建；role/gate 在停止阶段记录正常 session logout/connection close。

### 5.3 资源与服务状态

| 时间 | 服务/主机 | CPU | 内存 | DB/Redis/Consul | 重启/错误 |
|---|---|---:|---:|---|---|
| 压测窗口 | 全部服务 | 未测 | 未测 | 服务持续可用 | 未发现服务重启；真实 error 日志 0 行 |

## 6. 结果

bench 日志显示 100 个 bot 全部启动，启动耗时 `5.00063299s`；稳态期间多次报告为 `alive: 100/100`。停止后最终报告为 `alive: 0/100, dead: 100`，对应主动关闭连接。

- 计划 bots：100；实际启动：100；实际在线峰值：100
- 计划启动速率：20/s；实际启动耗时：5.00063299s（约 20/s）
- 稳态窗口：00:25:00 — 00:30:00 CST；有效时长：5m
- 登录成功：+100；登录限流：未返回对应序列；排队满/超时/登录错误：未返回对应序列
- 协议非 OK：`ReqPlotWater limited +100`；`ReqFlowerStartBreed error +0`
- 客户端请求延迟：p50 2.55ms、p95 4.85ms、p99 17.94ms
- 服务重启：未发现；窗口内 `up` 六节点均为 `1`

## 7. 结论

**结论：通过。**

本轮 Smoke 阶段满足预定标准：100 个 bot 全部启动并保持在线，5 分钟窗口内六个服务均存活，登录成功计数增加 100，结束时 Gate 的登录 inflight 和 queue 均为 0；未观察到真实 `level="error"` 日志或服务重启。`ReqPlotWater limited +100` 是已有业务限流分支的正常结果，不等同于登录链路失败。

本轮只能作为 100 bot / 20/s 的低负载基线，不能推导容量上限。CPU、内存和数据库连接资源未测。

## 8. 清理与后续

- bench：已按计划发送 `SIGINT` 并完成 100 个 bot 的连接关闭；supervisor 返回 `124`，原因是 `timeout` 达到时限
- 服务：未停止，六个 systemd 用户服务继续运行
- 临时配置/凭据：未产生凭据；bench 配置保留在 `/tmp`，用于复核
- 数据重置/保留：未重置；本轮新增压力账号和业务数据保留
- 保留的证据：`/tmp/gserver-pressure-2026-09-02-smoke.yaml`、`/tmp/gserver-pressure-2026-09-02-login-capacity-smoke.log`、本记录中的 Prometheus/Loki 查询与结果
- 后续实验：创建独立的 Medium 记录，建议 300 bots、30/s、10m，并补充 CPU、内存、数据库连接和更完整的吞吐统计
