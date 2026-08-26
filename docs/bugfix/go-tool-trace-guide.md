# go tool trace 使用指南

## 概述

`go tool trace` 是 Go 调度器级别的事件时间线可视化工具，展示每个 goroutine 在任意时刻的状态（运行/阻塞/等待调度），适合排查**延迟高但 CPU 低**的问题。

与 pprof 的区别：

| | pprof CPU profile | go tool trace |
|--|-------------------|---------------|
| 看什么 | 函数消耗了多少 CPU | goroutine 在什么时刻被阻塞/运行/等待 |
| 时间粒度 | 汇总统计（总计秒数） | 精确到微秒的事件流 |
| 适合场景 | CPU 100% 空转 | 延迟高、锁竞争、GC 暂停、调度延迟 |
| 典型问题 | `processMessages` 占 99% CPU | channel 阻塞 500ms、mutex 竞争 |

## 排查流程

### 第一步：生成 trace 文件

```bash
curl -o trace.out http://localhost:9090/debug/pprof/trace?seconds=5
go tool trace trace.out
```

抓取时长要覆盖问题场景（例如响应慢的请求持续几秒就抓几秒）。

### 第二步：看 Goroutine analysis（最快概览）

首页点击 **Goroutine analysis**，表格按 `Total execution time` 排序，执行时间最长的 goroutine 就是可疑目标。

点击某个 goroutine 详情，能看到它在几种阻塞类型上的耗时分布：

- **Execution Time** — 实际执行时间
- **Network Wait** — 网络 I/O 阻塞
- **Sync Block** — 同步原语阻塞（channel/mutex）
- **Scheduling Latency** — 等待被调度到 CPU
- **Syscall** — 系统调用阻塞（文件读写等）
- **GC Sweeping** — GC 扫描暂停

### 第三步：看 blocking profile（定位具体阻塞点）

首页有 4 种阻塞分析，以 pprof 火焰图形式呈现：

| 类型 | URL Path | 用途 |
|------|----------|------|
| **Synchronization blocking** | `/block` | channel/mutex 阻塞（最常用） |
| **Network blocking** | `/io` | 网络 I/O 等待 |
| **Syscall profile** | `/syscall` | 系统调用阻塞（文件/磁盘 I/O） |
| **Scheduler latency** | `/sched` | goroutine 等 CPU 调度 |

每个页面显示 `flat`（自身阻塞时间）和 `cum`（含下游调用的总阻塞时间），用法跟 pprof 一样。

### 第四步：View trace（确认因果关系）

如果前两步不够，去时间线视图（`trace?view=proc`）：

**页面布局：**

- **STATS 区域** — 3 条时间序列曲线：
  - **Goroutines** — 协程总数随时间变化
  - **Heap** — 内存分配曲线（锯齿上升=分配，骤降=GC）
  - **Threads** — 系统线程数
- **PROCS 区域** — 每个 CPU 核心一行（Proc 0~N），时间从左到右：
  - **彩色条** = goroutine 在运行
  - **空白** = 核心空闲（goroutine 在睡眠/阻塞）
  - **紫色 GC 条** = 垃圾回收

**鼠标操作：**
- **1** 键：选择模式，点击彩色条查看 goroutine 详细信息
- **2** 键：平移
- **3** 键：框选放大
- **4** 键：测量时间间隔

## 实战案例

### 场景：生产-消费速率不匹配

观察到的 blocking profile：

```
flat      cum      函数
529ms     529ms     sync.(*WaitGroup).Wait    ← 主 goroutine 等消费者完成
466ms     466ms     runtime.chansend1         ← 生产者往 channel 发数据被阻塞
```

结论：生产者过快而消费者过慢（`time.Sleep(10ms)`），channel 填满后生产者被迫等待。

### 场景：goroutine 泄漏

Goroutine analysis 中看到某个函数的 goroutine count 持续增长不下降，结合 `/block` 查看这些 goroutine 卡在哪个操作上。

### 场景：GC 影响延迟

View trace 中放大时间轴，观察 GC 紫色条出现时，应用 goroutine 是否被暂停。结合 Heap 曲线看 GC 触发频率是否过高。

## 总结

两步骤解决 90% 问题：

1. **Goroutine analysis** — 找到最慢的 goroutine
2. **Synchronization blocking profile** — 看火焰图定位具体代码行

View trace 主要用于分析 goroutine 之间的因果关系和 GC 影响。
