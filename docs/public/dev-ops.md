# 本地开发运维手册

日常开发环境:5 个独立进程(systemd user services 管理)+ docker 中间件 + 监控栈。

## 进程架构

```
gate(:11086, TCP 网关)  role(:25011)  chat(:25041)  friend(:25021)  guild(:25031)  account(:18080 HTTP)
        └── 每个 app 独立进程,通过 Consul 服务发现通信;account 签发登录 gate_token(prelogin)
```

多进程模式的意义:app↔app 消息走真实 TCP + protobuf 序列化(protoactor-go 对同地址 PID 走本地投递,all-in-one 会掩盖序列化/路由/超时问题)。

## 进程管理(systemd)

模板 unit:`~/.config/systemd/user/gserver@.service`(一个文件管所有 app),已 `enable`(WSL 重启自启)+ `Restart=always`(崩溃 3s 自愈)。

```bash
# 全部启动
systemctl --user start gserver@gate gserver@role gserver@chat gserver@friend gserver@guild gserver@account
# 全部停止
systemctl --user stop gserver@gate gserver@role gserver@chat gserver@friend gserver@guild gserver@account
# 单个重启 / 状态 / 日志
systemctl --user restart gserver@role
systemctl --user status gserver@role
journalctl --user -u gserver@role -f
```

二进制:`bin/gserver-node`(`go build -o bin/gserver-node ./node`,已 gitignore)。

## 端口规划

| 服务 | actor 端口 | metrics 端口 | 说明 |
|---|---|---|---|
| gate | 25001 | 9090 | TCP 网关入口 :11086 |
| role | 25011 | 9091 | |
| chat | 25041 | 9093 | 9092 被 prometheus 容器占用 |
| friend | 25021 | 9094 | |
| guild | 25031 | 9095 | |
| account | 25106 | —(无) | HTTP :18080,prelogin 签发 gate_token |

## 监控

- **Grafana**: `http://localhost:3000`,账号 `admin` / `admin`(compose env GF_SECURITY_ADMIN_PASSWORD;旧卷为 gserver123,401 时切换重试)
- **Prometheus**: `http://localhost:9092`(容器),抓取 5 个 app 的 metrics 端点
- **job 名约定**: `game-services`(与 k8s prometheus.yaml 及 dashboard 变量一致,勿改)
- 排障提示:多次登录失败会触发 Grafana 登录封禁(`docker logs gserver-grafana | grep login`),封禁在 `grafana.db` 的 `login_attempt` 表,清理需停容器改库并 `chown 472:472`

## 日志链路(Loki)

```
gxylog(zap) → JSON 文件 log/<app>/<app>.log → promtail → Loki(:3100) → Grafana Explore
```

- **Loki**: `http://localhost:3100`,容器 `gserver-loki` + `gserver-promtail`
- **promtail** 采集 `../log` 挂载:compose 在 `deploy/docker/`,相对路径用 **`../../log`**(一级会指向不存在的 `deploy/log`)
- **数据源**: Grafana 4 个(Prometheus / Prometheus-K8s / Tempo / Loki),provisioning 在 `deploy/docker/grafana/provisioning/datasources/datasources.yml`
- **日志↔trace 联动**:Loki derivedFields(`trace_id` 正则 → Tempo)+ Tempo tracesToLogs,双向
- 日志字段规范、反模式:`docs/public/logging.md`

### 排查命令

```bash
# 日志文件(JSON 行)
tail -f log/role/role.log | jq
# Loki 查询(注意时间范围,默认 1h 窗口查不到旧日志)
curl -sG 'http://localhost:3100/loki/api/v1/query_range' \
  --data-urlencode 'query={job="gserver"} | json | level="error"' \
  --data-urlencode "start=$(date -d '6 hours ago' +%s000000000)" --data-urlencode "end=$(date +%s000000000)"
# Tempo trace 存在性(经 grafana 容器)
docker exec gserver-grafana curl -s "http://tempo:3200/api/traces/<traceID>?start=$(date -d '6 hours ago' +%s)&end=$(date +%s)"
```

### 基础设施

```bash
# 数据库(密码来自 build/env/dev_zy.env.toml)
PGPASSWORD='@zyc0131' psql -h 127.0.0.1 -U postgres -d gserver -c '\dt'
# redis
redis-cli -h 127.0.0.1 -p 6379 -a '@zyc0131' --no-auth-warning ping
# consul(服务注册与健康;gate 是 TCP 入口不注册,预期行为)
curl -s http://127.0.0.1:8500/v1/catalog/services | jq -r 'keys[]'
curl -s 'http://127.0.0.1:8500/v1/health/service/role?passing' | jq -r 'length'
```

### dlv 调试(玩家路径)

**yama ptrace_scope=1 挡 `dlv attach`(sudo 需密码)→ 用 launch 方式**(dlv 自起进程不受限):

```bash
systemctl --user stop gserver@role   # 腾出 25011/9091 端口
# debug 工具 launch:program=bin/gserver-node args=[--config, config/role.toml] cwd=项目根 adapter=dlv
# 断点:src/apps/role/internal/logic/role_main.go:250(HandleClientMsg,玩家消息统一入口)
# continue → gserver_github/bin/hy 登录触发 → stack_trace / variables(r.RoleID 应等于玩家 role_id)
# 结束:terminate → systemctl --user start gserver@role 恢复托管
```

玩家路径加日志:同文件 `HandleClientMsg` 里 `gxylog.Debug(ctx, "role recv client msg", msg_id/msg_name/role_id)`,构建重启后用 hy 触发即可在 `log/role/role.log` 看到(带 trace_id)。

### Grafana provisioning 的坑(都踩过)

1. **`${}` 环境变量插值吞模板**:derivedFields 的 `url: '${__value.raw}'` 会被 provisioning 当成环境变量解析为空 → 点击 Tempo 跳转 `query:""` 无数据。**必须写成 `url: '$${__value.raw}'`**($$ 转义)。
2. **变更 provisioned 数据源 uid 导致启动崩溃**:`Datasource provisioning error: data source not found` 循环重启。改了 uid 需清 `grafana-data` 卷重建(数据源/面板全由 provisioning 重建;admin 密码回落环境变量需重置)。
3. **uid 交叉引用**:derivedFields/tracesToLogs 的 `datasourceUid` 引用需要目标数据源已存在——首次配置联动时,先建 uid 再加重用引用,或清卷一次性重建。

### 已修复:启动日志孤儿 trace_id

**现象(已修复)**:`server started` 等启动日志带 trace_id,但 Tempo 查不到对应 trace——点击跳转 404。

**根因**:goframe `gcmd.doRun` 会给命令执行创建根 span,并把带 span 的 ctx 传入命令 Func;main.go 若把该 ctx 传给 `StartModule`,启动链上所有 app 日志都会继承这个根 span 的 trace_id。而该 span 创建时全局 provider 还是 goframe 默认的(gtrace `init()` 设置的、**无 SpanProcessor 不导出**),因此 trace_id 是孤儿。

**修复**:`node/main.go` 的 `StartModule` 改用 `context.Background()`,不继承 gcmd 的 span(与 `AddModule` 一致)。启动日志不再带 trace_id;业务日志(actor/ghttp span,创建于 OTLP provider 就绪后)不受影响,仍正常导出 Tempo。

## 配置生成(重要)

`config/*.toml` 是**生成文件**(svr_init.sh 渲染模板产出),**不要直接修改**——重新生成会覆盖。

```bash
# 改配置的正确流程:改 env → 重新生成
vim build/env/dev_<name>.env.toml     # 端口/密码/各 app metrics 端口
./build/script/svr_init.sh dev_<name> # 重新生成 config/ + hack/ 脚本
```

**坑**:env 必须含 per-app metrics 段(`[metrics_gate]` 等 5 段),缺了模板渲染出 `addr = ":"` 导致服务 metrics 起不来(降级)。参照 `dev.env.toml` 补全。

- env 模板:`build/env/dev.env.toml`(含 per-app metrics 端口配置)
- 模板:`build/template/config/*.toml.template`
- 改完模板后重启对应服务才生效:`systemctl --user restart gserver@<app>`

## 调试客户端(hy_client)

新登录协议:客户端先 POST account `/prelogin` 拿 gate_token(JWT),再带 token 连 gate 握手。旧 `hy_client` 目录(直发账号)已不兼容,用 `gserver_github/bin/hy`:

```bash
# 交互式(推荐)——stdin 第一行是 platform_uid
./bin/hy --account-server=http://127.0.0.1:18080 --platform=guest --client-version=1.0.0

# 管道驱动自测:第一行是 platform_uid!
printf 'account_xxx\nquit\n' | ./bin/hy --account-server=http://127.0.0.1:18080 --platform=guest --client-version=1.0.0
# 判定:prelogin ok → handshake ok → login ok
```

**坑**:hy 会从 stdin 读第一行作为 **platform_uid**(不是账号);`--account-server` 触发 prelogin 并自动连接返回的 gate 地址。

## k8s 部署访问

- **集群**:kind `game-cluster`(单节点),配置 `deploy/k8s/kind-config.yaml`(hostPort 映射:10086→gate 30086、30080→account、30999→prometheus)
- **部署**:`kubectl apply -f deploy/k8s/` + **`kubectl apply -f deploy/k8s/config/`(configmap 在子目录,apply 不递归!)**
- **访问**:
  - gate:`127.0.0.1:10086`(hostPort)或 NodePort `30086`
  - account(prelogin):`127.0.0.1:30080`
  - prometheus:`127.0.0.1:30999`(Grafana 数据源 Prometheus-K8s)
- **冒烟**(新协议,复用本地 gserver_github/bin/hy):
  ```bash
  printf '<platform_uid>\nquit\n' | ./bin/hy --account-server=http://127.0.0.1:30080 --platform=guest --client-version=1.0.0
  # 判定:prelogin ok → handshake ok → login ok
  ```
- **启停**:`./hack/k8s-toggle.sh start|stop`(gate deployment + role/chat/friend/guild gameserverset 扩缩容)

### k8s 镜像与 OpenKruise 注意(踩过的坑)

- **镜像构建**:`docker build -f deploy/Dockerfile -t gserver:latest .`,然后 `kind load docker-image gserver:latest --name game-cluster`
- **节点内拉 docker.io 超时**:kind 节点 containerd 不继承宿主机代理,`imagePullPolicy: Always` 会拉取失败 → 改 `IfNotPresent` 用本地 load 的镜像;镜像名必须写完整 `docker.io/` 前缀(无前缀与 load 的 full-name 在 containerd 里不匹配,仍会尝试拉取)
- **OpenKruise 安装**(gameserverset 依赖):
  ```bash
  helm repo add openkruise https://openkruise.github.io/charts/
  # 核心 kruise(提供 AdvancedStatefulSet/PodProbeMarker CRD + webhook)
  helm template kruise openkruise/kruise --version 1.8.3 -n kruise-system > /tmp/kruise-core.yaml
  # sed 替换镜像为本地可用:openkruise-registry.cn-shanghai.cr.aliyuncs.com/openkruise/kruise-manager:v1.8.0
  kubectl apply -f /tmp/kruise-core.yaml
  # kruise-game(GameServerSet CRD)
  helm install kruise-game openkruise/kruise-game --version 1.0.0 -n kruise-game-system --create-namespace
  # 同样处理镜像(imagePullPolicy + docker.io/ 前缀 + kind load)
  ```
- **kruise-game 依赖核心 kruise**:GSS controller 创建 AdvancedStatefulSet 时调核心 webhook,核心 kruise 必须 Running;缺失时 GSS 卡在 DESIRED 2 / CURRENT 0
- **account/gate liveness 误杀**:服务启动慢(初始化 redis/pg/consul)超 probe 时限会被循环重启,等稳定即可(最终 1/1 Running)
