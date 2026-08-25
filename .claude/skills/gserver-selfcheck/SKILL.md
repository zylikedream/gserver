---
name: gserver-selfcheck
description: GServer 核心链路自检——验证"部署→运行→日志→Loki→Grafana→trace 关联→基础设施(db/redis/consul)→调试闭环(dlv/日志)"是否完整。当用户要求"自检"、"闭环验证"、"健康检查"、"链路验证"、"检查服务器是否正常"时触发。
---

# GServer 核心链路自检

按序执行,每步有明确判定标准。全部通过 = 闭环完整;任何一步失败 = 报告缺口并定位。

## 前置:环境/协议/命令细节

环境前提(目录、systemd、客户端、中间件密码)、登录协议、hy 客户端用法、构建命令等**一律查 `skill://gserver-dev`**,本技能只给流程与判定标准。

## 自检范围

```
代码层 → 进程层 → 容器层 → 业务冒烟 → 日志(Loki) → trace(Tempo) → Grafana 联动 → git 状态 → 数据库 → redis → consul → dlv 调试 → 加日志 → k8s 环境
```

## 流程

### 1. 代码层

```bash
go build ./...          # 编译全包
make test               # 全量测试
```

判定:编译零错误 + 测试无 FAIL。

### 2. 进程层

```bash
systemctl --user is-active gserver@gate gserver@role gserver@chat gserver@friend gserver@guild gserver@account
```

判定:6 个全 `active`。

### 3. 容器层

```bash
docker ps --format '{{.Names}} {{.Status}}' | sort
```

判定:postgres/redis/consul/prometheus/grafana/tempo/loki/promtail 8 个全 Up(postgres/redis 带 healthy)。

### 4. 业务冒烟(hy 客户端)

```bash
cd /home/zyr/workspace/gserver_github && printf '<platform_uid>\nquit\n' | ./bin/hy --account-server=http://127.0.0.1:18080 --platform=guest --client-version=1.0.0
```

判定:输出 `prelogin ok, role_id=<n>` → `handshake ok` → `login ok`(stdin 第一行是 **platform_uid**,不是账号)。

### 5. 日志链路(Loki)

```bash
# 文件层:新日志是单行 JSON,带 mod/roleID/trace_id
tail -1 log/role/role.log | jq '{level, msg, mod, roleID, trace_id}'
# Loki 层:6h 窗口有数据(默认 1h 窗口查不到旧日志,必须显式 start/end)
curl -sG 'http://localhost:3100/loki/api/v1/query_range' \
  --data-urlencode 'query={job="gserver"}' \
  --data-urlencode "start=$(date -d '6 hours ago' +%s000000000)" \
  --data-urlencode "end=$(date +%s000000000)" --data-urlencode 'limit=1' \
  | jq '.data.result | length'
```

判定:文件是 JSON 行(非 glog 文本)+ Loki result 长度 > 0。

### 6. trace 链路(Tempo)

```bash
# 从最新业务日志拿 trace_id(排除启动日志——见已知限制)
TID=$(grep -oE '"trace_id":"[0-9a-f]{32}"' log/role/role.log | tail -1 | grep -oE '[0-9a-f]{32}')
# Tempo 直查(经 grafana 容器,宿主机无法直接访问 tempo:3200)
docker exec gserver-grafana curl -s \
  "http://tempo:3200/api/traces/$TID?start=$(date -d '6 hours ago' +%s)&end=$(date +%s)" \
  -H 'Accept: application/json' | jq -r '.batches | length'
```

判定:返回 `1`(有 batches,含 service.name + spans)。**必须用业务日志的 trace_id**(actor 消息),启动日志是孤儿(见限制)。

### 7. Grafana 联动

```bash
# 数据源:4 个全在,derivedFields 的 url 必须非空(${__value.raw})
curl -s -u admin:admin http://localhost:3000/api/datasources | jq -r '.[].name'
curl -s -u admin:admin http://localhost:3000/api/datasources/uid/loki | jq -r '.jsonData.derivedFields[0].url'
```

判定:4 数据源(Prometheus/Prometheus-K8s/Tempo/Loki)+ url 为 `${__value.raw}`(非空)。**Grafana 密码:新卷 `admin`(compose env);旧卷 `gserver123`——401 时切换重试**。

### 8. git 状态

```bash
git status --short; git log --oneline -3
```

判定:worktree clean(无未提交改动)或仅有已声明的 WIP。

### 9. 数据库(postgres)

```bash
PGPASSWORD='@zyc0131' psql -h 127.0.0.1 -U postgres -d gserver -c '\dt'
# 增删改查示例(用临时行,演示后删除)
PGPASSWORD='@zyc0131' psql -h 127.0.0.1 -U postgres -d gserver \
  -c "insert into role_basic (role_name, level) values ('zz_test', 1) returning role_id;" \
  -c "update role_basic set level=2 where role_name='zz_test' returning role_id, level;" \
  -c "select role_id, role_name, level from role_basic where role_name='zz_test';" \
  -c "delete from role_basic where role_name='zz_test' returning role_id;"
```

判定:INSERT/UPDATE/SELECT/DELETE 全部有返回值。业务表:account / role_basic / role_bag / chat_private_message / friend_data / guild 等。

### 10. redis

```bash
redis-cli -h 127.0.0.1 -p 6379 -a '@zyc0131' --no-auth-warning ping
redis-cli -h 127.0.0.1 -p 6379 -a '@zyc0131' --no-auth-warning set zz_test_key hello
redis-cli -h 127.0.0.1 -p 6379 -a '@zyc0131' --no-auth-warning get zz_test_key
redis-cli -h 127.0.0.1 -p 6379 -a '@zyc0131' --no-auth-warning del zz_test_key
```

判定:PONG + set/get/del 正常;业务 key 存在(gserver:locate:node:actor:role:<id>、chat:lobby:* 等,有玩家在线时)。

### 11. consul

```bash
curl -s http://127.0.0.1:8500/v1/catalog/services | jq -r 'keys[]'
for svc in account role chat friend guild chat-http guild-http chat_channel; do
  echo -n "$svc: "; curl -s "http://127.0.0.1:8500/v1/health/service/$svc?passing" | jq -r 'length'
done
```

判定:7 个服务注册且 passing(gate 是 TCP 入口**不注册**,预期行为)。

### 12. dlv 调试(玩家路径断点)

**坑:yama ptrace_scope=1 挡 `dlv attach`(sudo 要密码),必须用 launch 方式**(dlv 自起进程不受限):

```bash
systemctl --user stop gserver@role          # 腾出 25011/9091 端口
# 用 debug 工具(或 dlv exec --headless)launch:
#   program=bin/gserver-node args=[--config, config/role.toml] cwd=gserver_github adapter=dlv
#   set_breakpoint: src/apps/role/internal/logic/role_main.go:250 (HandleClientMsg,玩家消息统一入口)
#   continue → 用 hy 登录触发 → stack_trace / variables
#   breakpoint 处可求值:r.RoleID 应与 hy 登录的 role_id 对应
# 结束:terminate 会话 → systemctl --user start gserver@role 恢复托管
```

判定:断点命中,堆栈完整(protoactor mailbox → gxyactor.Receive → AutoHandleMsg → HandleClientMsg),`r.RoleID` 与玩家对应。玩家业务路径入口:
- `RoleMain.HandleClientMsg`(role_main.go:250)—— 所有玩家客户端消息
- `Session.handleHandshake`(gateway session.go)—— 登录握手

### 13. 加日志(玩家路径)

位置:`src/apps/role/internal/logic/role_main.go` 的 `HandleClientMsg`,`clientMessageMetricLabels` 拿到 msgID/msgName 后加:

```go
gxylog.Debug(ctx, "role recv client msg",
    gxylog.Str("msg_id", msgID),
    gxylog.Str("msg_name", msgName),
    gxylog.Num("role_id", r.RoleID),
)
```

```bash
gofmt -w src/apps/role/internal/logic/role_main.go
go build -o bin/gserver-node ./node
systemctl --user restart gserver@role
# 触发:monorepo bin/hy 登录(见第 4 步)
grep 'role recv client msg' log/role/role.log | tail -3 | jq '{level, msg, msg_id, msg_name, role_id, trace_id}'
```

判定:日志出现 `ReqAccountLogin`/`ReqAccountLogout` 等玩家消息,JSON 行带 trace_id。

### 14. k8s 环境(可选,按需)

```bash
kind get clusters                              # game-cluster 存在
docker ps --format '{{.Names}} {{.Status}}' | grep game-cluster   # control-plane Up
kubectl get pods                               # 13 个 1/1 Running(gate x2/account x2/role x2/chat x2/friend x2/guild x2/prometheus)
ss -ltn | grep -E ':10086|:30080|:30999'       # hostPort 映射监听
# k8s 冒烟(新协议,经 k8s account 30080 + k8s gate 10086)
cd /home/zyr/workspace/gserver_github && printf '<uid>\nquit\n' | ./bin/hy --account-server=http://127.0.0.1:30080 --platform=guest --client-version=1.0.0
# k8s prometheus(独立于本地)
curl -s http://127.0.0.1:30999/-/healthy        # 200
curl -s http://127.0.0.1:30999/api/v1/targets | jq -r '.data.activeTargets[] | select(.health=="up") | .labels.job' | sort -u  # game-services
```

判定:`login ok`;game-services targets up。详细部署/踩坑见 `docs/public/dev-ops.md` k8s 章节。

## 已知限制与坑

|坑|说明|
|---|---|
|**孤儿 trace_id**|启动日志(`server started` 等)可能带 trace_id 但对应 span 不导出 Tempo——点击跳转 404。判定时**只用业务日志的 trace_id**。已修复:StartModule 不继承 gcmd 幽灵 span(commit e35330d)|
|**dlv attach 被 yama 挡**|`ptrace_scope=1` + sudo 需密码 → attach 被内核拒绝。用 launch 方式(见第 12 步),或先 `sudo sysctl kernel.yama.ptrace_scope=0`|
|**Tempo search 索引**|`/api/search` 对旧 block 可能 `inspected_traces=0`(容器重建后),但 **traceID 直查(`/api/traces/{id}`)正常**——Grafana 跳转走直查,不受影响|
|**Loki 时间窗口**|默认 1h 窗口查不到旧日志,排查前先切 Last 6h/24h;日志点 Tempo 跳转继承当前窗口|
|**derivedFields url 插值**|provisioning 里必须写 `url: '$${__value.raw}'`(双 $ 转义)。单 `${...}` 会被 Grafana 当环境变量插值吞掉 → 跳转 query 为空 → 无数据|
|**数据源 uid 变更**|provisioning 里变更已有数据源 uid 会导致 Grafana 启动循环崩溃(`data source not found`),需清 `grafana-data` 卷重建|
|**promtail 挂载**|compose 在 `deploy/docker/`,日志挂载必须 `../../log`(一级 `../log` 指向不存在的 deploy/log)|
|**config 是生成文件**|`config/*.toml` / `docker-compose.yml` 由 `./build/script/svr_init.sh dev_zy` 生成,勿手改;改 env(`build/env/dev_zy.env.toml`)后重新生成|

## 判定标准

1-14 全部通过 → 闭环完整。出现失败:
- 进程/容器挂 → 重启(systemd `restart`,docker `up -d`)后重测
- 冒烟失败 → 查 journalctl(`journalctl --user -u gserver@<app> -f`);确认用的是 gclient_github 新 hy(prelogin 协议)
- Loki 无数据 → 查 promtail 日志 + 挂载路径 + 时间窗口
- Tempo 无 trace → 确认用的是业务日志 trace_id + 时间窗口;再查 gxytrace exporter 配置
- 跳转无数据 → 先查 derivedFields url(第 7 步),再查时间窗口
- Grafana 401 → 密码是 admin(新卷)还是 gserver123(旧卷),切换重试
- dlv 断点不命中 → 确认断点在 `HandleClientMsg` 且玩家消息确实经过 role(看第 13 步日志)
- k8s pod 起不来 → 查镜像(imagePullPolicy/前缀)、OpenKruise(核心 kruise + kruise-game 是否 Running)、configmap 是否 apply(config/ 子目录单独 apply)
