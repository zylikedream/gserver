package gxyactor

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"ergo.services/ergo"
	"ergo.services/ergo/gen"
	"github.com/alicebob/miniredis/v2"
	"github.com/cockroachdb/errors"
	"github.com/redis/go-redis/v9"

	"gserver/core/gxyregistery"
)

// localServiceLookup 让能力目录始终指向本节点,把激活路径固定在"本节点创建"分支。
type localServiceLookup struct {
	nodeName string
}

func (l localServiceLookup) GetServiceInfo(context.Context, string, string, gxyregistery.ServiceSelector) *gxyregistery.ServiceInfo {
	return gxyregistery.NewServiceInfo("race", l.nodeName, "127.0.0.1:0", "v1.0.0", 1)
}

// gatedStore 在第 gateAt 次 Locate 之前执行一次 gate,其余行为直通。
//
// 用途是把"竞争落败者重试到第几轮"变成可控事件:并发用例不必赌时序,
// 而是等到落败者试完前几轮再放行持有者。生产里的等待来自 Redis 往返,
// 这里只是把它压到确定的一刻。
type gatedStore struct {
	OwnershipStore

	locates atomic.Int32
	gateAt  int32
	gate    func()
}

func (s *gatedStore) Locate(ctx context.Context, kind string, id string) (ActorOwner, error) {
	if s.locates.Add(1) == s.gateAt {
		s.gate()
	}
	return s.OwnershipStore.Locate(ctx, kind, id)
}

// raceActor 在同步初始化段阻塞,把"注册名已被占住、归属记录还没写"这个中间窗口
// 固定下来——并发激活时输的一方正好落在这个窗口里。
//
// 初始化段排在取归属之前(见 runtime_actor.go 的 Init),阻塞它即同时阻塞归属写入。
type raceActor struct {
	*EntityActor

	release <-chan struct{}
}

func (a *raceActor) Init(_ ...any) error {
	<-a.release
	return nil
}

type activateResult struct {
	pid PID
	err error
}

// newRaceFixture 搭出"本节点创建"分支所需的全部依赖:真实节点 + miniredis 归属载体。
func newRaceFixture(t *testing.T) (*activatorManager, gen.Node, *gatedStore, func()) {
	t.Helper()

	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })

	node, err := ergo.StartNode("racetest@localhost", gen.NodeOptions{})
	if err != nil {
		t.Fatalf("start node: %v", err)
	}
	t.Cleanup(node.Stop)
	nodeID := string(node.Name())

	locator := newActorLocator(client, nodeID)
	if err := locator.acquireNodeLease(context.Background()); err != nil {
		t.Fatalf("acquire node lease: %v", err)
	}

	release := make(chan struct{})
	var closeOnce sync.Once
	open := func() { closeOnce.Do(func() { close(release) }) }
	t.Cleanup(open)

	// 落败者第 3 次(最后一次)定位之前:放行持有者,并等它把归属记录写下去。
	// 前两次定位发生在持有者仍被阻塞时,因此必然落空。
	gate := &gatedStore{OwnershipStore: locator, gateAt: 4}
	gate.gate = func() {
		open()
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			owner, err := locator.Locate(context.Background(), "race", "1")
			if err == nil && !owner.IsZero() {
				return
			}
			time.Sleep(2 * time.Millisecond)
		}
		t.Error("持有者没有写下归属记录")
	}

	// 与生产同一条路径:节点句柄负责创建,激活协调层的 store 负责归属。
	prev := app
	app = &actorApp{
		node: node,
		activator: &activatorManager{
			nodeID: nodeID,
			kinds:  make(map[string]gen.ProcessFactory),
			store:  gate,
		},
	}
	t.Cleanup(func() { app = prev })

	mgr := &activatorManager{
		nodeID:        nodeID,
		kinds:         make(map[string]gen.ProcessFactory),
		store:         gate,
		serviceLookup: localServiceLookup{nodeName: nodeID},
	}
	if err := mgr.RegisterActorKind("race", func() Business {
		return &raceActor{EntityActor: NewEntityActor(), release: release}
	}); err != nil {
		t.Fatalf("register kind: %v", err)
	}
	return mgr, node, gate, open
}

// 并发激活同一个 actor 时,输的一方撞上的是"注册名已占用",不是故障:
// 它必须重新定位并拿到胜者的地址,而不是把 gen.ErrTaken 上抛给业务。
//
// 这是登录高峰的真实形态:同一批玩家同时进同一个世界频道,先到者占住注册名,
// 后到者在归属记录写下去之前就发起了创建。
func TestActivateActorRaceOnSameKeyYieldsSamePID(t *testing.T) {
	mgr, node, _, _ := newRaceFixture(t)
	ctx := context.Background()
	key := actorKey{kind: "race", id: "1"}

	first := make(chan activateResult, 1)
	go func() {
		pid, err := mgr.activateActor(ctx, key, true)
		first <- activateResult{pid: pid, err: err}
	}()

	// 等胜者占住注册名:此刻它的初始化段还阻塞着,归属记录尚未写入。
	waitForRegistration(t, node, key, time.Second, first)

	second := make(chan activateResult, 1)
	go func() {
		pid, err := mgr.activateActor(ctx, key, true)
		second <- activateResult{pid: pid, err: err}
	}()

	firstRes := <-first
	secondRes := <-second
	if firstRes.err != nil {
		t.Fatalf("胜者激活失败: %v", firstRes.err)
	}
	if secondRes.err != nil {
		t.Fatalf("并发激活失败(应重试后拿到同一实例): %v", secondRes.err)
	}
	if !PidEqual(firstRes.pid, secondRes.pid) {
		t.Fatalf("两次激活拿到不同实例: %v / %v", firstRes.pid, secondRes.pid)
	}
}

func waitForRegistration(t *testing.T, node gen.Node, key actorKey, timeout time.Duration, early chan activateResult) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if _, err := node.ProcessPID(key.name()); err == nil {
			return
		}
		select {
		case res := <-early:
			t.Fatalf("激活在占用注册名之前就返回了: pid=%v err=%v", res.pid, res.err)
		default:
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("等不到 %s 的注册名出现", key)
}

// 只有竞争落败才允许重试:其它失败重试会被磨成"重试耗尽",盖掉真正的原因。
func TestIsRaceLost(t *testing.T) {
	if !isRaceLost(gen.ErrTaken) {
		t.Fatal("注册名被占用应判为竞争落败")
	}
	if !isRaceLost(errors.Wrap(ErrNotOwner, "claim")) {
		t.Fatal("归属已被本节点抢到应判为竞争落败")
	}
	if isRaceLost(errors.New("actor kind race not registered")) {
		t.Fatal("未登记的 kind 不是竞争,不得重试")
	}
	if isRaceLost(nil) {
		t.Fatal("无错误不是竞争")
	}
}
