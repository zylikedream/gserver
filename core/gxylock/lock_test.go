package gxylock

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/cockroachdb/errors"
)

type memoryManager struct {
	held    map[string]string
	order   []string
	blocked map[string]bool
	// releaseCtxErr 记录 Release 被调用时 ctx 的错误状态,用于验证释放不依赖调用方 ctx
	releaseCtxErr []error
}

func newMemoryManager() *memoryManager {
	return &memoryManager{held: make(map[string]string), blocked: make(map[string]bool)}
}

func (m *memoryManager) Acquire(_ context.Context, key string, _ time.Duration) (string, bool, error) {
	m.order = append(m.order, key)
	if m.blocked[key] {
		return "", false, nil
	}
	if _, ok := m.held[key]; ok {
		return "", false, nil
	}
	token := key + ":token"
	m.held[key] = token
	return token, true, nil
}

func (m *memoryManager) Release(ctx context.Context, key string, token string) {
	m.releaseCtxErr = append(m.releaseCtxErr, ctx.Err())
	if m.held[key] == token {
		delete(m.held, key)
	}
}

// TestWithSortsAndReleases:With 必须同时满足三个契约,断言分散在回调前/内/后三处——
//   - 回调**执行期间**:4 个 key 去重成 3 把锁且全部同时持有(不是依次释放);
//   - 加锁顺序:按字典序 1→2→3(由 memoryManager 记录实际获取序列并断言);
//   - 回调**返回后**:一把都不剩。
//
// 为什么需要:防死锁依赖"全局统一顺序 + 全程持有"这一组合。只排序不同时持有,
// 等于两把锁之间开了窗口;只持有不排序,反向调用方仍会互持。缺任一条都能构造出真实死锁。
func TestWithSortsAndReleases(t *testing.T) {
	mem := newMemoryManager()
	err := With(context.Background(), mem, []string{"3", "1", "2", "1"}, time.Second, func() error {
		if len(mem.held) != 3 {
			t.Fatalf("expected 3 held locks, got %d", len(mem.held))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	wantOrder := []string{"1", "2", "3"}
	if !reflect.DeepEqual(mem.order, wantOrder) {
		t.Fatalf("unexpected lock order: got %v want %v", mem.order, wantOrder)
	}
	if len(mem.held) != 0 {
		t.Fatalf("expected locks released, still held: %v", mem.held)
	}
}

// TestWithReturnsBusyAndReleasesPartial:任一键拿不到时返回 ErrBusy,已取得的那几把锁
// 必须全部回滚(断言 mem.held 归零),且回调不得执行。
// 为什么需要:不回滚会永久泄漏已持有的锁,调用方再也拿不到该键——
// 表现为后续所有并发操作永久阻塞,不报错。
func TestWithReturnsBusyAndReleasesPartial(t *testing.T) {
	mem := newMemoryManager()
	mem.blocked["2"] = true
	err := With(context.Background(), mem, []string{"1", "2"}, time.Second, func() error {
		t.Fatal("callback should not run")
		return nil
	})
	// 断言哨兵本体:补栈包装会让 %+v 打两层栈(见 error-handling.md 第 6 条),
	// 这里用同一性而非 errors.Is 卡住"不包装"这一契约
	if err != ErrBusy {
		t.Fatalf("expected ErrBusy 本体, got %v", err)
	}
	if len(mem.held) != 0 {
		t.Fatalf("expected partial lock released, still held: %v", mem.held)
	}
}

// TestSleepBeforeRetryHonorsContextCancel:ctx 已取消时,sleepBeforeRetry 必须立即返回
// context.Canceled,不睡完请求的重试间隔。
// 为什么需要:关服与超时路径会走这里,真去睡满 30ms 退避会让停机被无谓拖慢,
// 大量并发重试时尤其明显。:85 的 20ms 是**时延上界,不是精度要求**——
// 取 20ms 是为容忍 CI 调度抖动。
func TestSleepBeforeRetryHonorsContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	start := time.Now()
	err := sleepBeforeRetry(ctx, 30*time.Millisecond, 0)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
	if time.Since(start) > 20*time.Millisecond {
		t.Fatalf("expected immediate cancel, took %s", time.Since(start))
	}
}

// TestWithReleasesWithDetachedContext 释放不能依赖调用方 ctx 的存活状态。
// 为什么需要:With 的 ctx 通常就是请求 ctx,超时/关服时已取消;若把它透传给
// Release,redis 命令发不出去、锁一直悬到 TTL —— 与"返回后释放"的契约相悖,
// 而 Release 不返回错误,失败完全无声。
func TestWithReleasesWithDetachedContext(t *testing.T) {
	mem := newMemoryManager()
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // 模拟请求已超时/关服

	err := With(ctx, mem, []string{"1", "2"}, time.Second, func() error { return nil })
	if err != nil {
		t.Fatalf("With: %v", err)
	}
	if len(mem.releaseCtxErr) != 2 {
		t.Fatalf("Release 调用 %d 次, want 2", len(mem.releaseCtxErr))
	}
	for i, got := range mem.releaseCtxErr {
		if got != nil {
			t.Fatalf("第 %d 次 Release 收到已取消的 ctx(%v):释放命令会发不出去", i, got)
		}
	}
}
