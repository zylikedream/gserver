# Grain Activator 统一续约实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** grainActivator 统一管理所有 child grain 的 Redis 续约，解决当前只续约第一个 child 的 bug，spawn 时直接注册（失败即停止），批量续约使用 Lua 脚本。

**Architecture:** Redis 注册改用 SETNX 原子语义；spawn 时立即注册，失败即 StopActor；grainActivator 的 locate_tick timer 通过 Lua 脚本批量续约所有 child。

**Tech Stack:** Go, Redis (go-redis + Lua), protoactor-go

---

## 文件变更总览

| 文件 | 操作 |
|------|------|
| `core/gxylocator/gxylocator.go` | 新增 `RegisterBatch` Lua 批量续约方法 |
| `core/gxyactor/grain_manager.go` | spawn 时直接注册、统一 timer、Terminated 立即注销 |

---

## Task 1: Redis 批量续约 — Lua 脚本

**文件:**
- Modify: `core/gxylocator/gxylocator.go`

- [ ] **Step 1: 添加 `RegisterBatch` Lua 批量续约方法**

```go
// RegisterBatch 批量注册/续约多个 key（使用 Lua 脚本保证原子性）
// keys: 格式 "key1", "val1", "key2", "val2", ... (交替)
// expireSeconds: TTL 秒数
func (l *Locator) RegisterBatch(ctx context.Context, keys []string, expireSeconds int64) error {
    if len(keys) == 0 {
        return nil
    }
    redisCli := gxyredis.Redis()
    redisKey := l.formatKey(LocatorTypeNode, "batch")

    // Lua 脚本: 遍历所有 key-val 对，SETEX 每个
    script := redis.NewScript(`
        local keys = KEYS
        local ttl = tonumber(ARGV[1])
        for i = 1, #keys, 2 do
            redis.call('SETEX', keys[i], ttl, keys[i+1])
        end
        return #keys / 2
    `)

    _, err := script.Run(ctx, redisCli, []string{redisKey}, expireSeconds).Result()
    if err != nil {
        return gerror.Wrapf(err, "RegisterBatch failed")
    }
    return nil
}
```

- [ ] **Step 2: 验证编译**

```bash
go build ./core/gxylocator/... 2>&1
```

预期: 通过

- [ ] **Step 3: Commit**

```bash
git add core/gxylocator/gxylocator.go
git commit -m "feat(locator): RegisterBatch Lua脚本批量续约Redis key"
```

---

## Task 2: grainActivator 添加 timerStarted flag

**文件:**
- Modify: `core/gxyactor/grain_manager.go`

- [ ] **Step 1: 给 grainActivator 加 `timerStarted` flag**

在结构体中添加 `timerStarted bool` 字段，防止重复启动 timer：

```go
type grainActivator struct {
    *ActorBase
    kind         string
    manager      *grainManager
    childs       map[PID]string  // child PID → grain ID
    meta         *grainMeta
    timerStarted bool            // 新增: 防止重复启动 timer
}
```

- [ ] **Step 2: 验证编译**

```bash
go build ./core/gxyactor/... 2>&1
```

预期: 通过

- [ ] **Step 3: Commit**

```bash
git add core/gxyactor/grain_manager.go
git commit -m "feat(grain): grainActivator添加timerStarted flag"
```

---

## Task 3: spawn 时直接注册 — 注册失败即 StopActor

**文件:**
- Modify: `core/gxyactor/grain_manager.go`

- [ ] **Step 1: 修改 `ActorActive` 处理 — spawn 时直接 SETNX 注册**

改动点：
1. SpawnNamed 后立即尝试注册（不等 Touch）
2. 注册失败（key 被抢）→ StopActor + 返回 ActorError
3. 注册成功才继续后续流程

```go
case *pb.ActorActive:
    props := a.meta.Props.Clone()
    pid, err := a.SpawnNamed(props.Configure(
        actor.WithContextDecorator(ContextDecorator(msg.Id, a.kind)),
        actor.WithReceiverMiddleware(a.grainReciveMiddleware()),
    ), msg.Id)
    if err != nil {
        if err == actor.ErrNameExists {
            a.Respond(&remote.ActorPidResponse{Pid: pid})
            return nil
        }
        a.Respond(ActorError(err.Error()))
        return nil
    }

    // 立即注册 Redis（SETNX 语义）
    key := getGrainLocateKey(a.kind, msg.Id)
    if err := a.registerGrainNode(a.ctx, key, pid); err != nil {
        // key 已被抢，停止本地 actor
        a.StopActor(pid)
        a.Respond(&pb.ActorError{Reason: "registration failed, key taken by another node"})
        return nil
    }

    // 注册成功，加入 childs
    a.childs[pid] = msg.Id
    a.meta.mgr.Add(msg.Id, pid)

    // Touch 确认（异步，不影响注册流程）
    sender := a.Actx.Sender()
    go func() {
        _, _ = a.Call(pid, &actor.Touch{}, 2*time.Second)
        // Touch 结果不影响注册状态，失败也只是 log
        a.Send(sender, &remote.ActorPidResponse{Pid: pid})
    }()

    // 启动 timer（只启动一次）
    if !a.timerStarted {
        a.timerStarted = true
        act := a.Actor().(IActor)
        act.Timer().AddTick(a.ctx, &gxytimer.Tick{
            Name:     "locate_tick",
            Interval: GrainLocateUpdateInterval,
        }, func(ctx context.Context, _ gxytimer.TimerActiveInfo) {
            a.renewAllGrainNodes(ctx)
        })
    }

    return nil
```

**注意**：`a.Actor()` 来自 `ActorBase`，实现了 `IActor` 接口，可以调用 `Timer()`。

- [ ] **Step 2: 验证编译**

```bash
go build ./core/gxyactor/... 2>&1
```

预期: 通过

- [ ] **Step 3: Commit**

```bash
git add core/gxyactor/grain_manager.go
git commit -m "feat(grain): spawn时直接注册Redis,失败即StopActor"
```

---

## Task 4: Terminated 立即注销 + 批量续约 renewAllGrainNodes

**文件:**
- Modify: `core/gxyactor/grain_manager.go`

- [ ] **Step 1: 修改 `actor.Terminated` 处理 — 立即 DEL Redis key**

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
    // 立即删除，不等 TTL 过期
    if err := a.DeregisterGrainNode(a.ctx, key, child); err != nil {
        glog.Errorf(a.ctx, "deregister grain node %s failed: %v", key, err)
    }
    a.meta.mgr.Remove(id)
    delete(a.childs, child)
    return nil
```

- [ ] **Step 2: 添加 `renewAllGrainNodes` — Lua 批量续约**

```go
// renewAllGrainNodes 批量续约所有 child grain（使用 Lua 脚本）
func (a *grainActivator) renewAllGrainNodes(ctx context.Context) {
    if len(a.childs) == 0 {
        return
    }
    // 构建 key-value 交替数组
    keys := make([]string, 0, len(a.childs)*2)
    for pid, id := range a.childs {
        key := getGrainLocateKey(a.kind, id)
        pidInfo, _ := protojson.Marshal(&pb.ActorPid{
            Address: pid.Address,
            Id:      pid.Id,
        })
        keys = append(keys, key, string(pidInfo))
    }
    // Lua 脚本批量 SETEX
    if err := a.manager.grainLocator.RegisterBatch(ctx, keys, int64(GrainLocateTTL/time.Second)); err != nil {
        glog.Errorf(ctx, "renewAllGrainNodes failed: %v", err)
        // 批量失败不停止所有 grain，只 log
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
git commit -m "feat(grain): Terminated立即注销+renewAllGrainNodes批量Lua续约"
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

- spawn时直接SETNX注册,失败即StopActor
- Terminated时立即DEL Redis key
- renewAllGrainNodes用Lua脚本批量续约
- timer只启动一次"
```

---

## 自查清单

- [ ] `Locator.RegisterBatch` 使用 Lua 脚本批量 SETEX
- [ ] `ActorActive` 中 spawn 后立即注册，失败调用 `StopActor`
- [ ] `Terminated` 收到后立即 `DeregisterGrainNode`
- [ ] `renewAllGrainNodes` 调用 `RegisterBatch` 批量续约
- [ ] `timerStarted` flag 防止 timer 重复启动
- [ ] `go build ./...` 通过
