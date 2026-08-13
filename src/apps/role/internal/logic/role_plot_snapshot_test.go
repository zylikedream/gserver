package logic

// rolePlotSnapshot Redis 存储测试:miniredis 注入惰性闭包,验证序列化往返与未命中。

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"gserver/core/gxyredis"
)

func newSnapshotStoreWithMiniRedis(t *testing.T) (redisRolePlotSnapshotStore, *miniredis.Miniredis) {
	t.Helper()
	srv := miniredis.RunT(t)
	cli := redis.NewClient(&redis.Options{Addr: srv.Addr()})
	t.Cleanup(func() { _ = cli.Close() })
	store := redisRolePlotSnapshotStore{
		redis: func() gxyredis.Client { return cli },
	}
	return store, srv
}

func TestRolePlotSnapshotStore_RoundTrip(t *testing.T) {
	store, _ := newSnapshotStoreWithMiniRedis(t)
	ctx := context.Background()

	plots := PlotMap{
		1: &PlotData{FlowerID: 101, State: 2},
		2: &PlotData{FlowerID: 102, State: 1},
	}
	if err := store.Set(ctx, 1001, plots); err != nil {
		t.Fatalf("set: %v", err)
	}

	got, ok := store.Get(ctx, 1001)
	if !ok {
		t.Fatal("expected hit after set")
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 plots, got %d", len(got))
	}
	if got[1].FlowerID != 101 || got[1].State != 2 {
		t.Fatalf("plot 1 mismatch: %+v", got[1])
	}
	if got[2].FlowerID != 102 || got[2].State != 1 {
		t.Fatalf("plot 2 mismatch: %+v", got[2])
	}
}

func TestRolePlotSnapshotStore_CloneOnSet(t *testing.T) {
	store, _ := newSnapshotStoreWithMiniRedis(t)
	ctx := context.Background()

	plots := PlotMap{1: &PlotData{FlowerID: 101, State: 1}}
	if err := store.Set(ctx, 1001, plots); err != nil {
		t.Fatalf("set: %v", err)
	}
	// 修改原 map,不应影响已存快照
	plots[1].State = 99
	delete(plots, 1)

	got, ok := store.Get(ctx, 1001)
	if !ok {
		t.Fatal("expected hit")
	}
	if got[1].State != 1 {
		t.Fatalf("expected cloned state 1, got %d", got[1].State)
	}
}

func TestRolePlotSnapshotStore_Miss(t *testing.T) {
	store, _ := newSnapshotStoreWithMiniRedis(t)
	ctx := context.Background()

	if _, ok := store.Get(ctx, 9999); ok {
		t.Fatal("expected miss for unknown roleID")
	}
}

func TestRolePlotSnapshotStore_Expire(t *testing.T) {
	store, srv := newSnapshotStoreWithMiniRedis(t)
	ctx := context.Background()

	if err := store.Set(ctx, 1001, PlotMap{1: &PlotData{FlowerID: 101}}); err != nil {
		t.Fatalf("set: %v", err)
	}
	// 模拟过期
	srv.FastForward(RolePlotSnapshotCacheExpire + time.Second)

	if _, ok := store.Get(ctx, 1001); ok {
		t.Fatal("expected miss after expire")
	}
}
