package gxyregistery

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gogf/gf/v2/container/gmap"
	"github.com/gogf/gf/v2/net/gsvc"

	"gserver/core/gxyregistery/types"
)

// scriptedWatcher 按序返回预设成员,取完后阻塞(模拟无新事件)。
// 多个 StartWatch 协程可能并发消费同一实例,故 idx 用锁保护。
type scriptedWatcher struct {
	mu       sync.Mutex
	idx      int
	payloads [][]gsvc.Service
	block    chan struct{}
}

func (w *scriptedWatcher) Proceed() ([]gsvc.Service, error) {
	w.mu.Lock()
	if w.idx >= len(w.payloads) {
		w.mu.Unlock()
		<-w.block
		return nil, context.Canceled
	}
	svcs := w.payloads[w.idx]
	w.idx++
	w.mu.Unlock()
	return svcs, nil
}

func (w *scriptedWatcher) Close() error { close(w.block); return nil }

// fakeRegistry 只提供 StartWatch 需要的最小能力。
type fakeRegistry struct {
	watcher    gsvc.Watcher
	current    []gsvc.Service
	watchCall  atomic.Int64  // Watch 被调用次数,用于验证协程只启动一次
	watchDelay time.Duration // 撑开"检查→写入"窗口,暴露非原子占位
}

func (f *fakeRegistry) Register(ctx context.Context, s gsvc.Service) (gsvc.Service, error) {
	return s, nil
}
func (f *fakeRegistry) Deregister(ctx context.Context, s gsvc.Service) error { return nil }
func (f *fakeRegistry) Search(ctx context.Context, in gsvc.SearchInput) ([]gsvc.Service, error) {
	return f.current, nil
}
func (f *fakeRegistry) Watch(ctx context.Context, key string) (gsvc.Watcher, error) {
	f.watchCall.Add(1)
	if f.watchDelay > 0 {
		time.Sleep(f.watchDelay)
	}
	return f.watcher, nil
}

func member(host string) gsvc.Service {
	return types.NewServiceInfo("role", "node-0", host, "v1.0.0", 1)
}

// TestStartWatchAppliesEveryChange 每个真实变更都必须写入缓存。
// 这条不变量曾被打断过:StartWatch 原有 2s 防抖,当事件间隔小于窗口时
// 定时器在触发前被逐个取消,缓存永不更新;而 GetHashServices 在缓存非空后
// 不再回源查询,于是发现数据冻结。
//
// watch 协程由 Search 触发(registery.go:85 的 go r.StartWatch),不在此处手动启动,
// 否则会有两个协程消费同一 watcher。
func TestStartWatchAppliesEveryChange(t *testing.T) {
	payloads := [][]gsvc.Service{
		{member("10.0.0.1:19001")},
		{member("10.0.0.2:19001")},
		{member("10.0.0.3:19001")},
		{member("10.0.0.4:19001")},
	}
	fw := &scriptedWatcher{payloads: payloads, block: make(chan struct{})}
	fr := &fakeRegistry{watcher: fw, current: []gsvc.Service{member("10.0.0.9:19001")}}

	r := &registery{
		regist:   fr,
		services: gmap.NewStrAnyMap(true),
		seqs:     gmap.NewStrIntMap(true),
	}

	// 缓存为空 → Search → 触发 watch 协程
	want := "10.0.0.4:19001"
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		hs, err := r.GetHashServices(context.Background(), "role")
		if err != nil {
			t.Fatalf("GetHashServices: %v", err)
		}
		if len(hs.ServiceInfos) == 1 && hs.ServiceInfos[0].NodeHost == want {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("缓存未收敛到最后一次变更的成员 %s", want)
}

// TestStartWatchSkipsUnchanged 无实际变化的重复推送不应递增版本号,
// 否则每次空推都会让所有消费方重建一致性哈希环。
func TestStartWatchSkipsUnchanged(t *testing.T) {
	same := []gsvc.Service{member("10.0.0.1:19001")}
	fw := &scriptedWatcher{
		payloads: [][]gsvc.Service{same, same, same},
		block:    make(chan struct{}),
	}
	fr := &fakeRegistry{watcher: fw, current: same}

	r := &registery{
		regist:   fr,
		services: gmap.NewStrAnyMap(true),
		seqs:     gmap.NewStrIntMap(true),
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		hs, err := r.GetHashServices(context.Background(), "role")
		if err != nil {
			t.Fatalf("GetHashServices: %v", err)
		}
		if len(hs.ServiceInfos) == 1 && hs.ServiceInfos[0].NodeHost == "10.0.0.1:19001" {
			// 缓存已就位,等一个轮次确认 seq 不再增长
			seq := r.seqs.GetOrSet("role", 0)
			time.Sleep(200 * time.Millisecond)
			if after := r.seqs.GetOrSet("role", 0); after != seq {
				t.Fatalf("seq 由 %d 增至 %d:无变化的推送也触发了更新", seq, after)
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("缓存未就位")
}

// TestConcurrentStartWatchStartsOneWatcher 并发调用 StartWatch 只能建一个 watcher。
// 直接并发调 StartWatch 而非经 Search:Search 的 Contains 检查会串行化调用方,
// 掩盖占位本身的非原子性。
func TestConcurrentStartWatchStartsOneWatcher(t *testing.T) {
	fw := &scriptedWatcher{payloads: [][]gsvc.Service{{member("10.0.0.1:19001")}}, block: make(chan struct{})}
	// 延迟 50ms:撑开"检查→写入"窗口,让所有调用方都能越过检查
	fr := &fakeRegistry{watcher: fw, current: []gsvc.Service{member("10.0.0.1:19001")}, watchDelay: 50 * time.Millisecond}

	r := &registery{
		regist:   fr,
		services: gmap.NewStrAnyMap(true),
		seqs:     gmap.NewStrIntMap(true),
	}

	const callers = 16
	start := make(chan struct{})
	for range callers {
		go func() {
			<-start // 齐步走
			r.StartWatch("role")
		}()
	}
	close(start)

	// StartWatch 成功后会永久阻塞在 watch 循环里,不能等它返回;
	// 等 300ms(远大于 50ms 的 Watch 延迟)让所有调用方都越过占位检查。
	time.Sleep(300 * time.Millisecond)
	if got := fr.watchCall.Load(); got != 1 {
		t.Fatalf("Watch 调用次数 = %d, want 1 (占位必须是原子的)", got)
	}
}

// TestWatchStartedIsPerName 哨兵必须按 name 隔离:一个名字已 watch 不影响其他名字。
func TestWatchStartedIsPerName(t *testing.T) {
	fw := &scriptedWatcher{payloads: [][]gsvc.Service{{member("10.0.0.1:19001")}}, block: make(chan struct{})}
	fr := &fakeRegistry{watcher: fw, current: []gsvc.Service{member("10.0.0.1:19001")}}

	r := &registery{
		regist:   fr,
		services: gmap.NewStrAnyMap(true),
		seqs:     gmap.NewStrIntMap(true),
	}

	for _, name := range []string{"role", "chat"} {
		if _, err := r.Search(context.Background(), name); err != nil {
			t.Fatalf("Search(%s): %v", name, err)
		}
	}

	// 轮询等待两个协程都调起 Watch,不用固定 sleep
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if fr.watchCall.Load() == 2 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("Watch 调用次数 = %d, want 2 (每个名字一次)", fr.watchCall.Load())
}

// failingWatcher 的 Proceed 立即返回错误,模拟 watcher 死亡。
type failingWatcher struct{ closed atomic.Int64 }

func (w *failingWatcher) Proceed() ([]gsvc.Service, error) { return nil, errors.New("watch died") }
func (w *failingWatcher) Close() error                     { w.closed.Add(1); return nil }

func waitUntil(t *testing.T, timeout time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("条件在超时内未成立")
}

// TestStartWatchInvalidatesCacheOnWatcherExit watcher 退出时缓存条目必须一起失效。
// 不清缓存的话条目永远"有效",Search 里 !Contains(name) 再不成立,该服务名就永久
// 没有 watcher 了 —— 缓存冻在最后一次快照上,且没有任何路径能重建 watcher。
func TestStartWatchInvalidatesCacheOnWatcherExit(t *testing.T) {
	fw := &failingWatcher{}
	fr := &fakeRegistry{watcher: fw, current: []gsvc.Service{member("10.0.0.1:19001")}}

	r := &registery{
		regist:   fr,
		services: gmap.NewStrAnyMap(true),
		seqs:     gmap.NewStrIntMap(true),
	}

	// 首次查询:写缓存 + 起 watcher,watcher 立刻失败退出
	if _, err := r.Search(context.Background(), "role"); err != nil {
		t.Fatalf("Search: %v", err)
	}

	waitUntil(t, 5*time.Second, func() bool {
		_, ok := r.services.Get("role").([]*types.ServiceInfo)
		return !ok
	})
	if got := fr.watchCall.Load(); got != 1 {
		t.Fatalf("Watch 调用次数 = %d, want 1", got)
	}
	if got := fw.closed.Load(); got != 1 {
		t.Fatalf("watcher.Close 调用次数 = %d, want 1", got)
	}

	// 下一次查询必须能重建 watcher
	if _, err := r.Search(context.Background(), "role"); err != nil {
		t.Fatalf("Search again: %v", err)
	}
	waitUntil(t, 5*time.Second, func() bool { return fr.watchCall.Load() >= 2 })
}
