# gxymq CPU 100% 忙等待问题排查

## 现象

启动 gate 节点后不做任何操作，CPU 占用持续 300%+（多核满载）。

## 排查过程

### 第一步：Grafana Metrics Dashboard 发现异常

在 GServer Metrics Dashboard 中观察到：

- **CPU Usage** 面板显示 gate 节点 CPU 303.9%，远超正常水平（<2%）
- **Goroutines** 数量正常，未泄漏
- **Actor Messages / sec** 速率为 0，无业务流量

初步判断：CPU 高但无业务处理，属于空转。

### 第二步：pprof CPU 火焰图定位函数

```bash
go tool pprof -http=:8080 'http://localhost:9090/debug/pprof/profile?seconds=10'
```

浏览器打开 pprof 页面，VIEW → Flame Graph，发现 `processMessages` 函数占据 **99.90%** CPU。

调用链：

```
processMessages (99.90%)
  └─ selectnbrecv (77.39%)        # 非阻塞 channel select
       └─ chanrecv (61.91%)        # channel 接收
            └─ empty (19.31%)      # 检查 channel 是否为空
```

### 第三步：分析根因

`processMessages` 中使用了 `select-default` 模式：

```go
for {
    select {
    case msg := <-ch1:
        // 处理消息
    case msg := <-ch2:
        // 处理消息
    default:
        // 什么都不做，立刻进入下一轮循环
        continue
    }
}
```

`default` 分支在所有 channel 都为空时立即返回，导致 for 循环永不停歇地空转，CPU 100%。

## 修复方案

使用 `reflect.Select` 实现阻塞式动态 channel 等待，替代 `select-default`：

```go
func (mq *messageQueueApp) processMessages() {
    cases := []reflect.SelectCase{
        {Dir: reflect.SelectRecv, Chan: reflect.ValueOf(ch1)},
        {Dir: reflect.SelectRecv, Chan: reflect.ValueOf(ch2)},
    }
    for {
        chosen, value, ok := reflect.Select(cases)
        if !ok {
            continue
        }
        // 根据 chosen 处理对应 channel 的消息
    }
}
```

`reflect.Select` 在所有 channel 都为空时会阻塞当前 goroutine，直到有消息到达才唤醒，CPU 占用从 300% 降至 <2%。

## 修复后验证

修复后重新抓取 pprof，调用图中只剩下 Go runtime 调度器函数（`schedule`、`findRunnable`、`netpoll`、`futexsleep`），不再有业务代码出现，CPU 恢复到 1% 左右。

## 排查工具总结

| 工具 | 用途 | 命令 |
|------|------|------|
| Grafana Dashboard | 宏观发现异常（CPU/内存/GC 飙高） | `http://localhost:3000` |
| pprof CPU Profile | 定位消耗 CPU 的具体函数 | `go tool pprof -http=:8080 http://localhost:9090/debug/pprof/profile?seconds=10` |
| pprof Flame Graph | 可视化查看 CPU 热点 | pprof UI → VIEW → Flame Graph |
| pprof Goroutine | goroutine 泄漏排查 | `go tool pprof -http=:8080 http://localhost:9090/debug/pprof/goroutine` |
| pprof Heap | 内存分配排查 | `go tool pprof -http=:8080 http://localhost:9090/debug/pprof/heap` |
| go tool trace | 事件级时间线（调度/阻塞/GC） | `curl -o trace.out http://localhost:9090/debug/pprof/trace?seconds=5 && go tool trace trace.out` |

## pprof 调用图指标含义

每个节点有两个时间：

- **flat**（左侧）：该函数**自身代码**消耗的 CPU 时间
- **cum**（of 右侧）：该函数及其**所有下游调用**的总 CPU 时间

排查时先看 cum 最高定位入口函数，再看 flat 最高定位具体瓶颈行。
