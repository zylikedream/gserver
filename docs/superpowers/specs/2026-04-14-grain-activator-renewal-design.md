# Grain Activator 统一续约设计

## 背景

当前 `grainActivator` 的续约机制存在问题：

1. **Bug**: `registerGrain` 的 timer 闭包只捕获了第一个 spawn 的 grain ID，其他 child grain 的 Redis key 无法续约
2. **延迟检测**: grain 崩溃后依赖 Redis TTL 40s 被动过期，无法立即感知
3. **无原子性**: Redis 注册用 `SETEX`，无法检测 key 是否已被其他节点抢注

## 目标

- grainActivator 统一管理所有 child grain 的续约
- grain 崩溃后立即注销 Redis key（不等 TTL）
- 注册失败时原子性地停止本地 actor

## 方案

### 核心流程

```
Child Grain Spawn:
  ActorActive → SpawnNamed → Touch 确认
    → 加入 childs map → registerGrainNode (SETNX)
    → 成功则启动 timer

Child Grain 终止:
  Terminated 消息 → DEL Redis key → 从 childs 删除

grainActivator Timer (30s):
  遍历 childs → 批量 SETEX 续约
```

### 关键变更

#### 1. Redis 注册改用 SETNX 原子语义

```go
// RegisterNode 改为 SETNX
func (l *Locator) RegisterNode(ctx context.Context, key string, node string, expireTime time.Duration) error {
    redisCli := gxyredis.Redis()
    redisKey := l.formatKey(LocatorTypeNode, key)
    // SETNX: 只有 key 不存在时才写入
    ok, err := redisCli.SetNX(ctx, redisKey, node, expireTime).Result()
    if err != nil || !ok {
        return gerror.New("key already registered by another node")
    }
    return nil
}
```

#### 2. spawn 失败时停止 actor

```go
case *pb.ActorActive:
    pid, err := a.SpawnNamed(props, msg.Id)
    if err != nil {
        if err == actor.ErrNameExists {
            a.Respond(&remote.ActorPidResponse{Pid: pid})
            return nil
        }
        a.Respond(ActorError(err.Error()))
        return nil
    }
    go func() {
        _, err = a.Call(pid, &actor.Touch{}, 2*time.Second)
        LocalSend(a.Self(), &ActorCheckResult{ID: msg.Id, Pid: pid, Err: err})
    }()

case *ActorCheckResult:
    if msg.Err != nil {
        // Touch 失败，停止本地 actor
        a.Send(msg.Sender, &pb.ActorError{Reason: "touch grain failed"})
        return nil
    }
    // 加入 childs
    a.childs[pid] = msg.ID
    a.meta.mgr.Add(msg.ID, pid)
    // 续约注册
    err := a.registerGrainNode(a.ctx, key, pid)
    if err != nil {
        // 注册失败（key 被抢），停止自己
        a.StopActor(pid)
        a.Send(msg.Sender, &pb.ActorError{Reason: "registration failed, key taken"})
        return nil
    }
    // 启动续约 timer
    act := ctx.Actor().(IActor)
    act.Timer().AddTick(ctx, &gxytimer.Tick{
        Name:     "locate_tick",
        Interval: GrainLocateUpdateInterval,
    }, func(ctx context.Context, _ gxytimer.TimerActiveInfo) {
        a.renewAllGrainNodes(ctx)
    })
    a.Send(msg.Sender, &remote.ActorPidResponse{Pid: pid})
```

#### 3. child grain 终止时立即注销

```go
case *actor.Terminated:
    child := msg.Who
    if child == nil {
        return nil
    }
    id := a.childs[child]
    if id == "" {
        return nil
    }
    key := getGrainLocateKey(a.kind, id)
    // 立即删除，不等 TTL
    a.DeregisterGrainNode(a.ctx, key, child)
    a.meta.mgr.Remove(id)
    delete(a.childs, child)
```

#### 4. 批量续约

```go
func (a *grainActivator) renewAllGrainNodes(ctx context.Context) {
    for pid, id := range a.childs {
        key := getGrainLocateKey(a.kind, id)
        if err := a.registerGrainNode(ctx, key, pid); err != nil {
            glog.Errorf(ctx, "renew grain node %s failed: %v", key, err)
            // 续约失败可能是 key 已被抢（节点重新选主），停止该 grain
            a.StopActor(pid)
            delete(a.childs, pid)
        }
    }
}
```

### 数据结构

```go
type grainActivator struct {
    *ActorBase
    kind    string
    manager *grainManager
    childs  map[PID]string  // child PID → grain ID (已有)
    meta    *grainMeta
}
```

### 时序图

```
正常 spawn:
  Client → GetGrain("role", "12345")
  → Redis 查不到
  → ServiceDiscovery 选 Node-A
  → Node-A grainActivator 收到 ActorActive
  → Spawn grain:12345
  → Touch 确认启动
  → childs[pid] = "12345"
  → RegisterNode SETNX(ttl=40s)
    → 成功 → 续约 timer (30s)
    → 失败 → StopActor + ActorError 返回

正常终止:
  grain:12345 崩溃/停止
  → protoactor 发 Terminated 给 grainActivator
  → DEL Redis key
  → 从 childs 删除
  → 下次 timer 不再续约

续约 timer (30s):
  grainActivator 遍历 childs
  → 每个 key SETEX(ttl=40s)
  → 如果失败 → StopActor + 删除 childs
```

## 风险与边界

1. **timer 只启动一次**: 当前设计每个 child spawn 都启动 timer，会重复。需加 flag 或 timer 移到 grainActivator 级别统一管理。
2. **etcd/Consul 注册**: 本次只改 Redis Locator，不涉及服务发现层的注册。
3. **grain 迁移**: 续约时发现 key 被抢 → StopActor → 客户端下次 GetGrain 会重新走服务发现 spawn 到新节点。

## 待验证

- [ ] timer 重复注册问题
- [ ] 多节点同时 spawn 同一 grain 时，只有一个能成功
- [ ] 续约批量操作的 Redis 压力
