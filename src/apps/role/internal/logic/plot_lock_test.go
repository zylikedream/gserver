package logic

import (
	"context"
	"testing"

	"gserver/src/pkg/deps"

	"github.com/alicebob/miniredis/v2"
	"github.com/cockroachdb/errors"
	"github.com/redis/go-redis/v9"
)

// 地块锁的测试走真 gxylock + miniredis:断言锁键在持有期存在、结束后消失。
func newPlotLockModule(t *testing.T) (*RoleModule, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	cli := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = cli.Close() })
	return &RoleModule{Role: &RoleMain{RoleID: 1001, deps: deps.Deps{Redis: cli}}}, mr
}

// TestWithPlotLocksAcquiresUniqueKeysAndReleases:withPlotLocks 对 plotID 去重(3,1,2,1 → 3 个锁键),回调返回后全部释放。
// 为什么需要:重复键若不去重,同一 owner 对同一把锁加两次会在解锁时先删掉自己的锁再误删他人抢到的锁,或直接自死锁。
func TestWithPlotLocksAcquiresUniqueKeysAndReleases(t *testing.T) {
	mod, mr := newPlotLockModule(t)

	err := mod.withPlotLocks(context.Background(), 1001, []int32{3, 1, 2, 1}, func() error {
		// 重复的 plotID 只锁一次:持有期应恰好 3 个锁键。
		if got := len(mr.Keys()); got != 3 {
			t.Fatalf("held locks = %d, want 3", got)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := len(mr.Keys()); got != 0 {
		t.Fatalf("locks not released, remaining keys: %v", mr.Keys())
	}
}

// TestWithPlotLocksReturnsBusyAndReleasesPartial:第 2 号锁被他人持有时返回 ErrPlotBusy,且回滚已取得的 1 号锁,只剩他人的键。
// 为什么需要:部分回滚是这块地的偷取还能再被偷的唯一保证。跳过回滚则本次残留的锁键会一直占着,
// 后续所有偷取该地块的请求都返回 busy——该地块永久无法被偷。
func TestWithPlotLocksReturnsBusyAndReleasesPartial(t *testing.T) {
	mod, mr := newPlotLockModule(t)
	if err := mr.Set(plotLockKey(1001, 2), "held-by-someone-else"); err != nil {
		t.Fatal(err)
	}

	err := mod.withPlotLocks(context.Background(), 1001, []int32{1, 2}, func() error {
		t.Fatal("callback should not run")
		return nil
	})
	if !errors.Is(err, ErrPlotBusy) {
		t.Fatalf("expected ErrPlotBusy, got %v", err)
	}
	// 已取得的 1 号锁必须回滚释放,只剩别人持有的 2 号。
	if got := len(mr.Keys()); got != 1 {
		t.Fatalf("partial locks not released, remaining keys: %v", mr.Keys())
	}
}
