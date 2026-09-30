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

func TestWithPlotLocksReturnsBusyAndReleasesPartial(t *testing.T) {
	mod, mr := newPlotLockModule(t)
	mr.Set(plotLockKey(1001, 2), "held-by-someone-else")

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
