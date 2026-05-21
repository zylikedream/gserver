# K8s 部署指南

## 概述

本文档记录 GServer 在 K8s（Kind）上的部署方式、踩过的坑和注意事项。

## 环境搭建

### Kind 集群

```bash
kind create cluster --config deploy/k8s/kind-config.yaml
```

`kind-config.yaml` 内容：

```yaml
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
nodes:
  - role: control-plane
    extraPortMappings:
      - containerPort: 30086
        hostPort: 10086
      - containerPort: 30999
        hostPort: 30999
```

> **坑：Kind 节点自带 HTTP_PROXY 环境变量。** `kind create cluster` 会自动继承宿主机的 HTTP_PROXY，指向 `127.0.0.1:7897`。如果你的代理没在运行，Pod 拉镜像会超时失败（ImagePullBackOff）。详见下文"镜像拉取"。

### 本地镜像

```bash
# 构建
docker build -f deploy/Dockerfile -t gserver:latest .

# 导入到 Kind（不走 registry，直接给节点）
kind load docker-image gserver:latest
```

> **镜像拉取策略必须设为 `IfNotPresent`。** Kind 节点有 HTTP_PROXY 指向 127.0.0.1:7897，如果没运行代理，`imagePullPolicy: Always` 会尝试通过代理拉取导致失败。`IfNotPresent` 直接使用本地已导入的镜像。

## 基础服务部署

### PostgreSQL

```bash
kubectl apply -f deploy/k8s/postgres.yaml
```

### Redis

```bash
kubectl apply -f deploy/k8s/redis.yaml
```

### ConfigMap

每个 App 有对应的 ConfigMap，配置了连接信息（DB、Redis、注册中心等）。

```bash
kubectl apply -f deploy/k8s/config/
```

> **注意：** ConfigMap 中的 DB/Redis 地址指向 `host.docker.internal`（WSL2 上 Docker Engine 不支持该域名，需用 `--add-host` 或 Gateway IP）。详见下文"跨环境网络"。

## 游戏服务部署

### StatefulSet

```bash
# 有状态服务（每个角色有独立身份）
kubectl apply -f deploy/k8s/role-statefulset.yaml
kubectl apply -f deploy/k8s/chat-statefulset.yaml
kubectl apply -f deploy/k8s/friend-statefulset.yaml
kubectl apply -f deploy/k8s/guild-statefulset.yaml

# 无状态网关
kubectl apply -f deploy/k8s/gate-deployment.yaml
kubectl apply -f deploy/k8s/gate-service.yaml
kubectl apply -f deploy/k8s/headless-service.yaml
```

### OpenKruiseGame

OKG 路径用于把 `role`、`chat`、`friend`、`guild` 从原生 `StatefulSet` 迁移到 `GameServerSet`。`gate` 仍然保持 `Deployment + NodePort`，Redis 注册发现、PostgreSQL、Prometheus、Grafana 先不改。

OKG 官方推荐用 Helm 安装。若本机还没有 Helm，可以先安装：

```bash
curl https://raw.githubusercontent.com/helm/helm/main/scripts/get-helm-3 | bash
```

如果 Helm 不在 `PATH` 里，可以通过 `HELM` 变量指定：

```bash
make install-okg HELM=/path/to/helm
```

先安装 OpenKruise 和 OpenKruiseGame：

```bash
make install-okg
```

等价 Helm 命令：

```bash
helm repo add openkruise https://openkruise.github.io/charts/ || true
helm repo update
helm upgrade --install kruise openkruise/kruise --version 1.8.0 \
  --set manager.image.repository=openkruise-registry.cn-shanghai.cr.aliyuncs.com/openkruise/kruise-manager
helm upgrade --install kruise-game openkruise/kruise-game --version 1.0.0 \
  --set prometheus.enabled=false \
  --set image.repository=registry-cn-hangzhou.ack.aliyuncs.com/acs/kruise-game-manager \
  --set image.pullPolicy=IfNotPresent
kubectl patch deployment kruise-controller-manager -n kruise-system --type='json' \
  -p='[{"op":"replace","path":"/spec/template/spec/containers/0/imagePullPolicy","value":"IfNotPresent"}]'
kubectl patch daemonset kruise-daemon -n kruise-system --type='json' \
  -p='[{"op":"replace","path":"/spec/template/spec/containers/0/imagePullPolicy","value":"IfNotPresent"}]'
```

`make install-okg` 默认使用上面的国内镜像。如果要切回 Docker Hub，可以覆盖变量：

```bash
make install-okg \
  KRUISE_IMAGE_REPO=openkruise/kruise-manager \
  OKG_IMAGE_REPO=openkruise/kruise-game-manager
```

部署 OKG 版本：

```bash
make deploy-k8s-okg
```

这个目标会做几件事：

- 构建并导入 `game-server:latest` 到 Kind。
- 应用 `deploy/k8s/config/`、`game-service-rbac.yaml`、`prometheus.yaml`、`gate-service.yaml`。
- 删除旧的 `role/chat/friend/guild` StatefulSet，避免和 OKG Pod 双跑。
- 应用 `role/chat/friend/guild` 的 `GameServerSet`。
- 应用 `gate-deployment.yaml`。

如果本地已经有可用的 `game-server:latest`，或者 Docker Hub 基础镜像拉取很慢，可以跳过构建，只导入并应用：

```bash
kind load docker-image game-server:latest --name game-cluster
make apply-k8s-okg
```

OKG 版本新增的 manifest：

```text
deploy/k8s/role-gameserverset.yaml
deploy/k8s/chat-gameserverset.yaml
deploy/k8s/friend-gameserverset.yaml
deploy/k8s/guild-gameserverset.yaml
deploy/k8s/game-service-rbac.yaml
```

每个游戏服 Pod 会通过 `GS_NAME` 知道自己的 `GameServer` 名称，并通过 `game-service-rbac.yaml` 授权读取自己的 OKG `GameServer` 状态。进程内的 `NodeEnv` 会把 OKG `OpsState` 映射到 registry 状态：

```text
None / Allocated / 空值  -> serving
WaitToBeDeleted / Kill  -> draining
Maintaining             -> maintaining
```

selector 只会选择 `serving` 节点。因此手动执行：

```bash
kubectl patch gs role-1 --type merge -p '{"spec":{"opsState":"WaitToBeDeleted"}}'
```

几秒后，`role-1` 会在 registry 中变成 `draining`，后续新请求不会再被分配到该节点。

查看 OKG 状态：

```bash
make status-k8s-okg
```

也可以直接执行：

```bash
kubectl get crd | grep -E 'gameserver|gameserverset|kruise'
kubectl get gss
kubectl get gs
kubectl get pods -l app=role
kubectl get pods -l app=chat
kubectl get pods -l app=friend
kubectl get pods -l app=guild
```

删除 OKG 游戏服节点池：

```bash
make delete-k8s-okg
```

如果要回到 StatefulSet 路径，先执行 `make delete-k8s-okg`，再重新 apply 原来的 `*-statefulset.yaml`。

> **注意：** 不要同时运行同一组服务的 StatefulSet 和 GameServerSet。它们会使用相同的 `app` 标签，容易造成 Redis 注册、Prometheus 目标和网关路由混乱。

## 可观测性

### Prometheus

```bash
kubectl apply -f deploy/k8s/prometheus.yaml
```

配置文件要点：

- **RBAC：** Prometheus 默认 ServiceAccount 没有权限 List Pod。需要手动创建 ClusterRole + ClusterRoleBinding，授予 `pods/services/endpoints/nodes` 的 `get/list/watch` 权限
- **Relabeling：** K8s 环境的 relabel 配置与本地开发环境不同。需要通过 relabel 规则补充 `app`/`node` 等标签，保持与本地 dashbboard 兼容
- **镜像策略：** 同上，`imagePullPolicy: IfNotPresent`

### Grafana（本地 Docker）

Grafana 不在 K8s 里跑，而是用本地 Docker Compose 启动，通过 datasource 连接 K8s 的 Prometheus。

```yaml
# docker/grafana/provisioning/datasources/datasources.yml
datasources:
  - name: Prometheus-K8s
    type: prometheus
    url: http://host.docker.internal:30999
```

> **坑：Linux/WSL2 上 Docker Engine 不支持 `host.docker.internal`。** Docker Desktop 自动注册了这个域名，但 Docker Engine on Linux 不会。两种解法：
> 1. 用 Gateway IP（`ip route show default` 看，通常是 `172.17.0.1`），但 Docker 网络重启后可能变化
> 2. 在 `docker-compose.yml` 加 `extra_hosts: ["host.docker.internal:host-gateway"]`，然后 `docker compose up -d --force-recreate` 重建容器

### Prometheus 查询

K8s Prometheus 暴露在 `NodePort:30999`，并通过 kind 的 `extraPortMappings` 映射到宿主机 `30999`，Grafana 通过 `http://host.docker.internal:30999` 访问。

## 注册中心：Redis

原方案使用 Consul，后改为 Redis。详见 `docs/system/gxyregistery-architecture.md`。

### Key 结构

```
gserver:svc:{serviceName}       ← Hash，存服务列表
gserver:locate:node:actor:{kind}:{id}  ← String，存 Actor 所在节点
```

### 配置变更

```toml
# config/*.toml — registery 段
[registery]
  type = "redis"
[registery.redis]
  interval = 10  # 注册刷新间隔（秒）
```

## FAQ / 踩坑记录

### 1. 镜像拉取失败（ImagePullBackOff）

**原因：** Kind 节点自动继承宿主机 HTTP_PROXY，如果代理没在运行，`imagePullPolicy: Always` 的拉取会超时。

**解决：** 所有 K8s 部署 YAML 加上 `imagePullPolicy: IfNotPresent`，并先用 `kind load docker-image` 导入。

### 2. host.docker.internal 不可用

**原因：** Linux 上的 Docker Engine（非 Docker Desktop）不会自动注册 `host.docker.internal` 域名。

**解决：** Docker Compose 加 `extra_hosts: ["host.docker.internal:host-gateway"]`，或直接使用宿主机 Gateway IP。

### 3. Prometheus 没有目标

**原因：** 默认 ServiceAccount 没有 RBAC 权限。

**解决：** 创建以下 RBAC 资源：

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: prometheus
rules:
  - apiGroups: [""]
    resources: [pods, services, endpoints, nodes]
    verbs: [get, list, watch]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: prometheus
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: prometheus
subjects:
  - kind: ServiceAccount
    name: prometheus
    namespace: default
```

### 4. 同一套代码，K8s 和本地 Prometheus 标签不同

**原因：** K8s 和服务发现机制不同。本地通过 Pushgateway，标签来自进程配置；K8s 通过服务发现，标签来自 relabel 规则。

**解决：** 在 Prometheus 配置中添加 relabel 规则，补充 `app`、`node` 标签，使两个环境的 metrics 标签结构一致。

### 5. 镜像更新后如何重启服务

构建新镜像 → 导入 Kind → 滚动重启：

```bash
# 1. 构建
docker build -f deploy/Dockerfile -t gserver:latest .

# 2. 导入 Kind
kind load docker-image gserver:latest

# 3. 滚动重启所有服务
kubectl rollout restart deployment/gate -n gserver
kubectl rollout restart statefulset/role -n gserver
kubectl rollout restart statefulset/chat -n gserver
kubectl rollout restart statefulset/friend -n gserver
kubectl rollout restart statefulset/guild -n gserver

# 4. 查看状态
kubectl rollout status deployment/gate -n gserver
kubectl rollout status statefulset/role -n gserver
```

> `rollout restart` 会触发 Pod 滚动更新，新 Pod 拉取镜像时因为 `imagePullPolicy: IfNotPresent` 会直接用本地已导入的版本。

### 6. ConfigMap 更新后服务未生效

**原因：** K8s ConfigMap 挂载到 Pod 后默认不会热更新。StatefulSet 需要手动重启 Pod：

```bash
kubectl rollout restart statefulset role
```
