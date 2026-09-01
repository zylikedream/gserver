# GServer 压测记录

本目录保存 GServer 每一次压测的完整记录。压测不是只保存一条 QPS 数字：必须同时保留参数、环境、观测证据、结果和结论，才能比较不同版本与不同限流配置。

## 记录规则

- 每次运行一个文件：`YYYY-MM-DD-<run-name>.md`。
- **启动流量前创建记录文件**，先填目标、环境、参数和验收标准；运行结束后补结果与结论。
- 同一文件可以包含多个明确分段（例如 warm-up、steady、saturation），但每个分段都要有独立参数和结果。参数发生变化时优先新建记录。
- 失败、提前中止和未产生流量的运行也要记录，结论标记为 `部分完成` 或 `无效`，不能删除失败记录。
- 所有时间写明时区；Prometheus/Grafana 查询写明完整查询语句和选择的时间范围。
- 不记录密码、Cookie、Token、数据库凭据或其他敏感信息。
- 当前允许直接清理本地/测试数据；仍须在记录中写明清理范围，且每次使用唯一账号前缀。

## 标准流程

1. **定义实验**：明确是链路冒烟、限流分支验证、容量测试还是回归比较，并写出通过标准。
2. **前置检查**：确认分支、构建产物、PostgreSQL/Redis/Consul、account、gate、逻辑服务和 Prometheus target 状态。
3. **固定参数**：使用独立的 `build/env/pressure_<operator>.env.toml`，从个人开发环境复制后设置 `[log].level = "info"`；通过 `./build/script/svr_init.sh pressure_<operator>` 生成服务配置。记录服务配置、bench YAML、账号池、机器人类型权重、启动速率、稳态时长和停止方式。
4. **运行压测**：`client/cmd/bench` 没有内置 duration，运行到 `SIGINT`/`SIGTERM`；使用外部 timeout 时记录命令、信号和退出状态。
5. **采集证据**：保存 bench 输出、Prometheus 数值、Grafana/Loki 查询、时间范围、服务日志和异常时间线。
6. **结束与清理**：停止 bench 和临时服务，清理临时配置/凭据，记录数据重置和证据保留情况。
7. **完成结论**：以验收标准为依据判断 `通过`、`不通过`、`部分完成` 或 `无效`，不得用推测值替代未测指标。

## 常用观测指标

| 目标 | 指标或查询 | 说明 |
|---|---|---|
| 服务存活 | `up{job="game-services"}` | 逐节点记录，不能只看总状态 |
| 在线峰值 | `max_over_time(sum(online_players{node="gate"})[15m:])` | 与实际稳态窗口对应 |
| 登录结果 | `increase(login_limit_total{result="..."}[$__range])` | 记录 `ok`、`rate_limited`、`queue_full`、`queue_timeout`、`error` |
| 登录排队 | `login_inflight`、`login_queue_length` | 记录峰值、结束值及是否回落 |
| 协议非 OK | `increase(client_requests_total{result!="ok"}[$__range])` | 按 `msg_name` 聚合 |
| 协议真正错误 | `increase(client_requests_total{result="error"}[$__range])` | 与预期业务拒绝区分 |
| 日志 | `{job="gserver"} |= \`handle client protocol failed\`` | Loki selector 必须带非空 matcher |

`increase()` 用于选择窗口内的计数；`rate(counter[5m])` 用于每秒速率。空结果、查询错误和服务无数据是三种不同状态，分别记录。

## 文件模板

新建记录时复制 [template.md](template.md)：

```bash
run_name="login-capacity-smoke"
cp docs/pressure/template.md "docs/pressure/$(date +%F)-${run_name}.md"
```

## 历史记录

| 日期 | 运行 | 类型 | 结论 |
|---|---|---|---|
| 2026-08-31 | [login-limit-phase-a](2026-08-31-login-limit-phase-a.md) | 低阈值登录限流分支验证 | 通过（以会话记录为依据） |
