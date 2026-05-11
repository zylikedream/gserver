# go tool pprof 使用指南

## pprof 两种使用方式

### 方式一：直接在线分析（无需本地文件）

```bash
# CPU profile（默认 30 秒）
go tool pprof -http=:8080 'http://localhost:9091/debug/pprof/profile?seconds=30'

# 堆内存（当前存活对象）
go tool pprof -http=:8080 'http://localhost:9091/debug/pprof/heap'

# 所有分配历史
go tool pprof -http=:8080 'http://localhost:9091/debug/pprof/allocs'

# goroutine 堆栈
go tool pprof -http=:8080 'http://localhost:9091/debug/pprof/goroutine'

# 同步阻塞（需先调用 runtime.SetBlockProfileRate）
go tool pprof -http=:8080 'http://localhost:9091/debug/pprof/block'

# mutex 锁竞争（需先调用 runtime.SetMutexProfileFraction）
go tool pprof -http=:8080 'http://localhost:9091/debug/pprof/mutex'
```

`-http=:8080` 会在浏览器打开可视化界面，支持火焰图、调用图、源代码视图等。

### 方式二：生成本地文件，离线分析

```bash
# 1. 生成 profile 文件
curl -o /tmp/cpu.pprof 'http://localhost:9091/debug/pprof/profile?seconds=30'
curl -o /tmp/heap.pprof 'http://localhost:9091/debug/pprof/heap'
curl -o /tmp/goroutine.pprof 'http://localhost:9091/debug/pprof/goroutine'

# 2. 分析本地文件
go tool pprof -http=:8080 /tmp/cpu.pprof
```

生成文件的场景：

- 现场问题要留存归档
- 环境没有图形界面（服务器）
- 需要将 profile 发给其他人分析
- 对比不同时间点的 profile

## pprof 视图类型

打开 `-http` 界面后，顶部 **VIEW** 菜单可切换：

| 视图 | 用途 |
|------|------|
| **Graph** | 默认调用图，方框+箭头，节点大小表示 CPU 占比 |
| **Flame Graph** | 火焰图，最宽的就是最热的函数 |
| **Top** | 列表，按 flat/cum 排序，适合精确看数值 |
| **Source** | 源码标注模式，直接显示每行代码消耗了多少 CPU |
| **Peek** | 查看某个函数在哪些地方被调用 |

### Top 视图怎么看

```
flat  flat%   sum%        cum   cum%
6.85s 22.41% 22.41%   30.53s 99.90%  gserver/core/gxymq.(*messageQueueApp).processMessages
4.73s 15.48% 37.89%   23.65s 77.39%  runtime.selectnbrecv
```

- **flat** — 该函数自身的 CPU 时间（不计子调用）
- **flat%** — flat 占总采样时间的百分比
- **sum%** — 到当前行累计的 flat%
- **cum** — 该函数 + 所有子调用的总 CPU 时间
- **cum%** — cum 占总采样时间的百分比

排查时先看 **cum** 最高的函数确定入口，再看 **flat** 最高的确定具体瓶颈。

## 实战：保存 profile 后用 Flame Graph 排查

```bash
# 1. 服务在线，抓取 30 秒 CPU
curl -o /tmp/cpu.pprof 'http://localhost:9091/debug/pprof/profile?seconds=30'

# 2. 打开火焰图
go tool pprof -http=:8081 /tmp/cpu.pprof

# 3. 浏览器切换到 VIEW → Flame Graph
# 观察最宽的色块，点击展开查看子调用链
```

## pprof 与 trace 的选择

| 场景 | 用什么 | 表现特征 |
|------|--------|---------|
| CPU 100% 空转 | `pprof profile` | 某函数 cum 接近 100% |
| 内存泄漏 | `pprof heap` | heap 持续增长不回落 |
| goroutine 泄漏 | `pprof goroutine` | 某函数 goroutine count 持续增加 |
| 请求延迟高 | `pprof trace` | CPU 正常但请求慢 |
| channel/mutex 阻塞 | `pprof block` | 业务处理慢，CPU 低 |
| 锁竞争激烈 | `pprof mutex` | mutex 等待时间长 |
