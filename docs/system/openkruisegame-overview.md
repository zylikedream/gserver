# OpenKruiseGame 概览

本文整理 OpenKruiseGame（OKG）的核心概念、能力边界，以及它在 GServer 项目里的适用方式。目标是先把 OKG 理解清楚，再决定如何把当前 Kind / K8s 部署逐步迁移过去。

## OKG 是什么

OpenKruiseGame 是 OpenKruise 面向游戏服务器场景的 Kubernetes 工作负载控制器。它不是数据库、注册中心或服务网格，而是替代原生 `Deployment` / `StatefulSet` 的游戏服实例管理层。

它的核心资源关系是：

```text
GameServerSet -> GameServer -> Pod
```

- `GameServerSet`：管理一组游戏服实例，类似游戏版 `StatefulSet`。
- `GameServer`：表示一个具体游戏服实例，是 OKG 增加的实例级管理对象。
- `Pod`：实际运行游戏服务进程。

相比原生 K8s 工作负载，OKG 更关注游戏服常见问题：稳定实例 ID、单服定向操作、原地升级、优雅下线、按业务状态扩缩容、云厂商网络接入。

## 核心能力

### 稳定实例身份

OKG 创建的游戏服实例会有稳定名称，例如：

```text
role-0
role-1
role-2
```

业务可以通过 Downward API 把 `metadata.name` 注入容器，例如 `GS_NAME`。这对 role/chat/friend/guild 这类节点池比较友好，因为每个节点本来就需要清晰的运行身份。

### 单实例管理

OKG 允许直接管理某个 `GameServer`，而不是只能操作整个工作负载。例如：

- 指定某个实例优先更新。
- 指定某个实例优先删除。
- 标记某个实例进入维护状态。
- 标记某个实例等待删除。
- 标记某个实例已经被匹配系统分配。

这比原生 `StatefulSet` 更适合游戏服，因为游戏服不是完全无状态实例，每个实例上可能承载不同玩家和不同运行状态。

### 原地升级和热更新

OKG 支持 `podUpdatePolicy: InPlaceIfPossible`。当变更满足原地升级条件时，可以尽量避免重建 Pod。

常见更新策略包括：

- `OnDelete`：手动删除 Pod 后再更新。
- `RollingUpdate`：滚动更新。
- `InPlaceIfPossible`：能原地更新就原地更新，否则重建。
- `partition`：控制更新范围。
- `updatePriority`：控制某些 GameServer 优先更新。

这对游戏服很重要，因为频繁重建 Pod 会带来玩家断线、注册中心抖动和尾延迟问题。

### 优雅下线

OKG 的生命周期钩子比原生 `preStop` 更适合游戏服。它可以在删除或更新前进入 `PreDelete` / `PreUpdate` 阶段，并根据业务条件决定是否继续阻塞删除。

典型流程：

```text
1. role 节点正常服务玩家
2. 准备缩容或维护
3. 标记 GameServer OpsState = WaitToBeDeleted
4. 业务停止给该节点分配新玩家
5. 等待玩家下线或迁移
6. 保存脏数据
7. 生命周期钩子放行
8. Pod 真正删除
```

这正好对应 GServer 之前遇到的问题：关服或缩容时，大量玩家同时下线会集中触发数据库保存，造成 PostgreSQL 压力激增。

### 按业务状态扩缩容

OKG 缩容不是随机删 Pod，而是按 `OpsState` 和优先级排序。

缩容优先级大致是：

```text
WaitToBeDeleted -> None -> Allocated -> Maintaining
```

含义是：

- `WaitToBeDeleted`：最适合被删除。
- `None`：默认状态。
- `Allocated`：已经分配给业务，尽量晚删。
- `Maintaining`：维护中，通常最后删。

还可以通过 `deletionPriority` 进一步控制同状态实例的删除顺序。

### 网络模型

OKG 提供 Cloud Provider & Network Plugin，用来把游戏服实例暴露给外部网络。

官方支持的网络形态包括：

- Kubernetes HostPort
- Kubernetes NodePort
- Kubernetes Ingress
- AlibabaCloud SLB / NLB / EIP / NATGW
- Volcengine CLB
- AWS NLB
- Tencent Cloud CLB
- Huawei ELB
- JD Cloud 网络插件

对当前 GServer 来说，第一阶段不需要立刻使用 OKG 网络插件。`gate` 仍然可以保持现有 NodePort 暴露方式，role/chat/friend/guild 继续走内部 Redis 注册发现。

## 状态模型

OKG 里最重要的是区分 `State` 和 `OpsState`。

### State

`State` 是系统状态，主要由 OKG 根据 Pod 生命周期维护，业务不应该直接修改。

常见值：

```text
Creating
Ready
NotReady
Crash
Deleting
Updating
PreDelete
PreUpdate
Unknown
```

它表达的是 Pod / GameServer 当前运行阶段。

### OpsState

`OpsState` 是运维和业务状态，可以由业务或运维系统修改。

常见值：

```text
None
Allocated
Maintaining
WaitToBeDeleted
Kill
```

对 GServer 最有价值的是：

- `Allocated`：该节点正在承载玩家或已经被分配。
- `Maintaining`：该节点处于维护状态，尽量不要缩容删除。
- `WaitToBeDeleted`：该节点可以等待删除，适合优雅缩容。
- `Kill`：强制删除该 GameServer。

## 对 GServer 的适用方式

当前 GServer 的 K8s 结构是：

```text
gate              Deployment + NodePort
role              StatefulSet
chat              StatefulSet
friend            StatefulSet
guild             StatefulSet
redis             Deployment / Service
prometheus        Deployment / Service
grafana           本地 Docker Compose
```

第一阶段建议只把游戏服节点池迁移到 OKG：

```text
role-statefulset.yaml   -> role-gameserverset.yaml
chat-statefulset.yaml   -> chat-gameserverset.yaml
friend-statefulset.yaml -> friend-gameserverset.yaml
guild-statefulset.yaml  -> guild-gameserverset.yaml
```

暂时保持不变：

- `gate` 继续使用 `Deployment`。
- `gate` 继续通过 NodePort 对外提供入口。
- Redis 注册发现不变。
- PostgreSQL 不变。
- Prometheus / Grafana 不变。
- 业务 actor 寻址逻辑不变。
- 不使用 OKG 网络插件。

这样迁移风险最低，OKG 只先接管 Pod 生命周期，不改变业务链路。

## 第一阶段建议

第一阶段目标是：在 Kind 里跑通 OKG 的 `GameServerSet`，行为尽量等价于当前 `StatefulSet`。

每个 `GameServerSet` 保留现有配置：

- `replicas`
- `app` / `node` 标签
- Prometheus scrape annotations
- 镜像和 `imagePullPolicy: IfNotPresent`
- `--config /config/*.toml`
- `POD_IP` 环境变量
- actor 端口和 metrics 端口
- readiness / liveness probe
- ConfigMap 挂载
- log volume
- `hostAliases`
- `terminationGracePeriodSeconds`

同时可以新增：

```yaml
env:
  - name: GS_NAME
    valueFrom:
      fieldRef:
        fieldPath: metadata.name
```

用于让进程知道自己对应的稳定 GameServer 名称。

## 后续增强方向

### 缩容保护

后续可以让 role 节点在准备缩容时进入：

```text
OpsState = WaitToBeDeleted
```

业务侧停止分配新玩家到该节点，然后等待已有玩家下线，再放行删除。

GServer 里通过 `NodeEnv` 完成这一步：K8s 环境下进程根据 `GS_NAME` 查询自己的 OKG `GameServer`，把 `OpsState` 映射成 registry 里的服务状态。

```text
None / Allocated / 空值  -> serving
WaitToBeDeleted / Kill  -> draining
Maintaining             -> maintaining
```

selector 只会选择 `serving` 节点，所以 `draining` 和 `maintaining` 节点会自动从新请求分配路径里摘掉。

### 玩家数上报

OKG 本身不知道某个 role 节点有多少玩家。这个信息需要业务提供。

可选方式：

- 通过 HTTP probe 查询节点玩家数。
- 通过脚本读取本地状态。
- 通过 Prometheus / Redis / 管理接口间接判断。
- 由业务管理面主动修改 GameServer `OpsState`。

### 优雅关服

可以把之前的 role save limiter 和 OKG 生命周期钩子结合起来：

```text
PreDelete
  -> 停止新玩家进入
  -> 等待在线玩家下降
  -> 批量保存脏数据
  -> 确认保存完成
  -> 允许 Pod 删除
```

这样比单纯依赖 SIGTERM 更可控。

### 阿里云迁移

迁移到阿里云 ACK 后，可以再评估：

- ACK + OpenKruiseGame
- PolarDB
- Tair
- 阿里云 Prometheus
- SLS
- NLB / SLB / EIP / NATGW

但这些不应该和第一阶段 Kind 验证混在一起做。

## 风险和注意事项

### OKG 不替代业务状态

OKG 只管理工作负载生命周期。它不会自动知道：

- 节点上有多少玩家。
- 节点是否还有脏数据。
- 某个玩家是否应该迁移。
- actor 是否已经注销。
- Redis 注册是否已经刷新。

这些仍然需要 GServer 自己处理。

### 避免双跑

迁移期间要避免同时运行同一组服务的 `StatefulSet` 和 `GameServerSet`。

例如不要同时存在：

```text
role StatefulSet
role GameServerSet
```

否则可能造成：

- Redis 注册重复。
- Prometheus 目标重复。
- Grafana 指标混乱。
- 网关路由到非预期节点。

### Headless Service 选择器

OKG 官方建议如果要使用每个实例的 DNS，可以创建和 `GameServerSet` 同名的 Headless Service，并使用类似下面的 selector：

```yaml
selector:
  game.kruise.io/owner-gss: role
```

当前 GServer 主要靠 Redis 注册发现，不强依赖实例 DNS。但如果保留 Headless Service，需要确认 selector 不会同时选中旧 StatefulSet 和新 GameServerSet。

### 原地升级有限制

`InPlaceIfPossible` 不是所有变更都能原地完成。配置挂载、环境变量、部分 Pod spec 变更仍然可能触发重建。

第一阶段不要把“原地升级”作为必须成功的目标，先保证 `GameServerSet` 能稳定替代当前 `StatefulSet`。

## 参考资料

- OpenKruiseGame 简介：https://openkruise.io/zh/kruisegame/introduction
- 部署 GameServerSet：https://openkruise.io/zh/kruisegame/user-manuals/deploy-gameservers
- GameServer 状态：https://openkruise.io/zh/kruisegame/user-manuals/gameserver-state
- 更新策略：https://openkruise.io/zh/kruisegame/user-manuals/update-strategy
- 扩缩容：https://openkruise.io/zh/kruisegame/user-manuals/gameservers-scale
- 网络模型：https://openkruise.io/zh/kruisegame/user-manuals/network
- 生命周期管理：https://openkruise.io/zh/kruisegame/user-manuals/lifecycle
- CRD 字段说明：https://openkruise.io/zh/kruisegame/user-manuals/crd-field-description
