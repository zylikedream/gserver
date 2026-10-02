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

func (m *memoryManager) Release(_ context.Context, key string, token string) {
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
	if !errors.Is(err, ErrBusy) {
		t.Fatalf("expected ErrBusy, got %v", err)
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
