package gxyactor

import (
	"context"
	"testing"

	"ergo.services/ergo/testing/mock"
)

// orderLease 记录租约释放发生的时机。
type orderLease struct{ order *[]string }

func (l orderLease) acquireNodeLease(context.Context) error { return nil }

func (l orderLease) releaseNodeLease(context.Context) error {
	*l.order = append(*l.order, "release-lease")
	return nil
}

func (l orderLease) startLeaseHeartbeat(context.Context, func(error)) func() { return func() {} }

// TestStopOrderReleasesLeaseAfterActorsStop 停机顺序即语义:先等 actor 终止
// (最终落库与归属释放都在其中),再交还节点租约。
//
// 颠倒会让整台节点的 actor 在终止前就失去"有权持有"的判据:其他节点可以立刻
// 接管,旧实例的最终落库随即被 fence 拒绝——静默丢数据(不变量 #4)。
func TestStopOrderReleasesLeaseAfterActorsStop(t *testing.T) {
	var order []string
	node := mock.NewNode()
	node.OnStop(func() { order = append(order, "stop-node") })

	app := &actorApp{
		node:      node,
		activator: &activatorManager{lease: orderLease{order: &order}},
	}
	if err := app.OnModStop(context.Background()); err != nil {
		t.Fatalf("stop: %v", err)
	}

	if len(order) != 2 || order[0] != "stop-node" || order[1] != "release-lease" {
		t.Fatalf("停机顺序 = %v, want [stop-node release-lease](先终止 actor, 再交还租约)", order)
	}
}
