package gxyactor

import (
	"context"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

// orderingActor 在终止回调里读取所有权记录是否仍然存在,
// 用于验证"释放发生在最终落盘之后"这条顺序(不变量 4)。
type orderingActor struct {
	*EntityActor

	ownerKey string
	client   redis.UniversalClient

	ownerStillPresentAtTerminate bool
	terminateCalled              bool
}

func newOrderingActor(ownerKey string, client redis.UniversalClient) *orderingActor {
	a := &orderingActor{ownerKey: ownerKey, client: client}
	a.EntityActor = NewEntityActor("ordering", a)
	return a
}

// Terminate 代表"最终落盘"的位置:此处所有权必须仍然由本实例持有,
// 因此先验证、再交给基类释放。
func (a *orderingActor) Terminate(reason error) {
	a.terminateCalled = true
	n, err := a.client.Exists(context.Background(), a.ownerKey).Result()
	a.ownerStillPresentAtTerminate = err == nil && n > 0
	a.EntityActor.Terminate(reason)
}

// TestOwnershipReleasedAfterTerminate 释放必须晚于落盘:
// 先释放会让新持有者推进世代,使旧实例的最后一次落盘被拒。
func TestOwnershipReleasedAfterTerminate(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })

	locator := newActorLocator(client, "node-a")
	if err := locator.acquireNodeLease(context.Background()); err != nil {
		t.Fatal(err)
	}
	owner, acquired, err := locator.claim(context.Background(), "ordering", "1")
	if err != nil || !acquired {
		t.Fatalf("claim owner=%+v acquired=%v err=%v", owner, acquired, err)
	}

	// 接线:门面通过全局 app 访问定位器。
	prev := app
	app = &actorApp{activator: &activatorManager{locator: locator}}
	t.Cleanup(func() { app = prev })

	biz := newOrderingActor(getActorLocateKey("ordering", "1"), client)
	biz.owner = owner
	biz.ownedID = "1"

	biz.Terminate(nil)

	if !biz.terminateCalled {
		t.Fatal("business Terminate must run")
	}
	if !biz.ownerStillPresentAtTerminate {
		t.Fatal("ownership must still be held while the terminate callback runs")
	}

	// 终止回调返回后,所有权必须已释放。
	exists, err := client.Exists(context.Background(), getActorLocateKey("ordering", "1")).Result()
	if err != nil {
		t.Fatal(err)
	}
	if exists != 0 {
		t.Fatal("ownership must be released after the terminate callback returns")
	}
	_ = server
}
