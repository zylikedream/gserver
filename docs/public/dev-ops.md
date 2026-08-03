# 本地开发运维手册

日常开发环境:5 个独立进程(systemd user services 管理)+ docker 中间件 + 监控栈。

## 进程架构

```
gate(:11086, TCP 网关)  role(:25011)  chat(:25041)  friend(:25021)  guild(:25031)
        └── 每个 app 独立进程,通过 Consul 服务发现通信
```

多进程模式的意义:app↔app 消息走真实 TCP + protobuf 序列化(protoactor-go 对同地址 PID 走本地投递,all-in-one 会掩盖序列化/路由/超时问题)。

## 进程管理(systemd)

模板 unit:`~/.config/systemd/user/gserver@.service`(一个文件管所有 app),已 `enable`(WSL 重启自启)+ `Restart=always`(崩溃 3s 自愈)。

```bash
# 全部启动
systemctl --user start gserver@gate gserver@role gserver@chat gserver@friend gserver@guild
# 全部停止
systemctl --user stop gserver@gate gserver@role gserver@chat gserver@friend gserver@guild
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

## 监控

- **Grafana**: `http://localhost:3000`,账号 `admin` / `gserver123`(已重置)
- **Prometheus**: `http://localhost:9092`(容器),抓取 5 个 app 的 metrics 端点
- **job 名约定**: `game-services`(与 k8s prometheus.yaml 及 dashboard 变量一致,勿改)
- 排障提示:多次登录失败会触发 Grafana 登录封禁(`docker logs gserver-grafana | grep login`),封禁在 `grafana.db` 的 `login_attempt` 表,清理需停容器改库并 `chown 472:472`

## 配置生成(重要)

`config/*.toml` 是**生成文件**(svr_init.sh 渲染模板产出),**不要直接修改**——重新生成会覆盖。

```bash
# 改配置的正确流程:改 env → 重新生成
vim build/env/dev_<name>.env.toml     # 端口/密码/各 app metrics 端口
./build/script/svr_init.sh dev_<name> # 重新生成 config/ + hack/ 脚本
```

- env 模板:`build/env/dev.env.toml`(含 per-app metrics 端口配置)
- 模板:`build/template/config/*.toml.template`
- 改完模板后重启对应服务才生效:`systemctl --user restart gserver@<app>`

## 调试客户端(hy_client)

```bash
# 交互式(推荐)
./bin/hy --addr=127.0.0.1:11086 --config=config/default.toml

# 管道驱动自测:第一行是账号输入!
printf 'account_xxx\nquit\n' | ./bin/hy --addr=127.0.0.1:11086
```

**坑**:hy 会从 stdin 读第一行作为账号,`--account` 参数只在 stdin 为空时兜底。

## k8s 部署访问

- NodePort:`172.18.0.2:30086`(kind 节点 IP)
- hostPort:`127.0.0.1:10086`(kind-config 映射,宿主机直接访问)
- 启停:`./hack/k8s-toggle.sh start|stop`
