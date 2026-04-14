# Grain Activator 统一续约实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** grainActivator 统一管理所有 child grain 的 Redis 续约，解决当前只续约第一个 child 的 bug，支持注册失败时原子性停止本地 actor。

**Architecture:** Redis 注册改用 SETNX 原子语义；grainActivator 维护 childs map，Terminated 时立即 DEL key，locate_tick timer 批量续约所有 child。

**Tech Stack:** Go, Redis (go-redis), protoactor-go

---

## 文件变更总览

| 文件 | 操作 |
|------|------|
| `core/gxylocator/gxylocator.go` | RegisterNode 改 SETNX，返回是否成功 |
| `core/gxyactor/grain_manager.go` | 全面修改：renewAllGrainNodes、timer flag、ActorCheckResult、Terminated |

---

## Task 1: Redis 注册改 SETNX 原子语义

**文件:**
- Modify: `core/gxylocator/gxylocator.go`

- [ ] **Step 1: 修改 `RegisterNode` 为 SETNX**

将 `Register` 方法的 SET 改为 SETNX，返回 error 表示 key 是否被抢注：

```go
// Register 注册键和节点的映射关系（SETNX 语义：只有 key 不存在时才写入）
func (l *Locator) Register(ctx context.Context, t string, key string, node string, expireTime time.Duration) error {
    redisCli := gxyredis.Redis()
    redisKey := l.formatKey(t, key)
    ok, err := redisCli.SetNX(ctx, redisKey, node, expireTime).Result()
    if err != nil {
        return gerror.Wrapf(err, "failed to register key %s in Redis", redisKey)
    }
    if !ok {
        return gerror.Newf("key %s already registered by another node", redisKey)
    }
    return nil
}
```

原有 `RegisterNode` 方法不变（调用 `Register` 即可）。

- [ ] **Step 2: 验证编译**

```bash
go build ./core/gxylocator/... 2>&1
```

预期: 通过

- [ ] **Step 3: Commit**

```bash
git add core/gxylocator/gxylocator.go
git commit -m "feat(locator): RegisterNode改SETNX原子语义,key被抢时返回error"
```

---

## Task 2: grainActivator 添加 renewAllGrainNodes 和 timer flag

**文件:**
- Modify: `core/gxyactor/grain_manager.go`

- [ ] **Step 1: 给 grainActivator 加 timerStarted flag**

在 `grainActivator` 结构体中添加 `timerStarted bool` 字段，防止重复启动 timer：

```go
type grainActivator struct {
    *ActorBase
    kind         string
    manager      *grainManager
    childs       map[PID]string
    meta         *grainMeta
    timerStarted bool  // 新增: 防止重复启动 timer
}
```

- [ ] **Step 2: 添加 renewAllGrainNodes 方法**

```go
// renewAllGrainNodes 批量续约所有 child grain 的 Redis key
func (g *grainActivator) renewAllGrainNodes(ctx context.Context) {
    for pid, id := range g.childs {
        key := getGrainLocateKey(g.kind, id)
        if err := g.registerGrainNode(ctx, key, pid); err != nil {
            glog.Errorf(ctx, "renew grain node %s failed: %v", key, err)
            // 续约失败可能是 key 已被抢，停止该 grain
            g.StopActor(pid)
            delete(g.childs, pid)
        }
    }
}
```

- [ ] **Step 3: 验证编译**

```bash
go build ./core/gxyactor/... 2>&1
```

预期: 通过

- [ ] **Step 4: Commit**

```bash
git add core/gxyactor/grain_manager.go
git commit -m "feat(grain): grainActivator添加timerStarted flag和renewAllGrainNodes"
```

---

## Task 3: 修改 ActorCheckResult 处理 — 注册失败停止 actor

**文件:**
- Modify: `core/gxyactor/grain_manager.go:70-95`

- [ ] **Step 1: 修改 `ActorCheckResult` 处理逻辑**

当前代码 spawn 后等待 `ActorCheckResult` 才加入 childs 和注册 Redis。改后逻辑：

```go
case *ActorCheckResult:
    sender := msg.Sender
    if msg.Err != nil {
        // Touch 确认失败，停止本地 actor（spawn 时已有 pid，但未加入 childs）
        if msg.Pid != nil {
            g.StopActor(msg.Pid)
        }
        g.Send(sender, &pb.ActorError{Reason: "touch grain failed"})
        return nil
    }
    pid := msg.Pid
    id := msg.ID

    // 加入 childs
    g.childs[pid] = id
    g.meta.mgr.Add(id, pid)

    // 注册 Redis（SETNX 语义）
    key := getGrainLocateKey(g.kind, id)
    if err := g.registerGrainNode(g.ctx, key, pid); err != nil {
        // key 被抢，停止本地 actor
        g.StopActor(pid)
        delete(g.childs, pid)
        g.meta.mgr.Remove(id)
        g.Send(sender, &pb.ActorError{Reason: "registration failed, key taken"})
        return nil
    }

    // 启动 timer（只启动一次）
    if !g.timerStarted {
        g.timerStarted = true
        act := g.ActorBase
        act.Timer().AddTick(g.ctx, &gxytimer.Tick{
            Name:     "locate_tick",
            Interval: GrainLocateUpdateInterval,
        }, func(ctx context.Context, _ gxytimer.TimerActiveInfo) {
            g.renewAllGrainNodes(ctx)
        })
    }

    g.Send(sender, &remote.ActorPidResponse{Pid: pid})
    return nil
```

**注意**: `g.ActorBase` 实现了 `IActor`，其 `Timer()` 方法来自 `ActorBase`。

- [ ] **Step 2: 验证编译**

```bash
go build ./core/gxyactor/... 2>&1
```

预期: 通过

- [ ] **Step 3: Commit**

```bash
git add core/gxyactor/grain_manager.go
git commit -m "feat(grain): ActorCheckResult处理注册失败时StopActor,timer只启动一次"
```

---

## Task 4: Terminated 处理 — 立即注销 Redis key

**文件:**
- Modify: `core/gxyactor/grain_manager.go`

- [ ] **Step 1: 修改 `actor.Terminated` 处理逻辑**

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
    // 立即删除 Redis key，不等 TTL 过期
    if err := a.DeregisterGrainNode(a.ctx, key, child); err != nil {
        glog.Errorf(a.ctx, "deregister grain node %s failed: %v", key, err)
    }
    a.meta.mgr.Remove(id)
    delete(a.childs, child)
    return nil
```

- [ ] **Step 2: 验证编译**

```bash
go build ./core/gxyactor/... 2>&1
```

预期: 通过

- [ ] **Step 3: Commit**

```bash
git add core/gxyactor/grain_manager.go
git commit -m "feat(grain): Terminated时立即DEL Redis key,不等TTL过期"
```

---

## Task 5: 全量编译验证

- [ ] **Step 1: 编译整个项目**

```bash
go build ./... 2>&1
```

预期: 全部通过，无错误。

- [ ] **Step 2: 提交所有变更**

```bash
git add -A
git commit -m "feat(grain): grainActivator统一续约优化

- RegisterNode改SETNX,key被抢返回error
- child grain注册失败时原子性StopActor
- Terminated时立即DEL Redis key
- locate_tick timer批量续约所有childs
- timer只启动一次(timerStarted flag)"
```

---

## 自查清单

- [ ] `Locator.Register` 使用 `SetNX`，key 被抢时返回 error
- [ ] `ActorCheckResult` 中注册失败调用 `StopActor`
- [ ] `Terminated` 收到后立即 `DeregisterGrainNode`
- [ ] `renewAllGrainNodes` 批量续约所有 childs
- [ ] `timerStarted` flag 防止 timer 重复启动
- [ ] `go build ./...` 通过
