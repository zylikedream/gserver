package gxyactor

import (
	"context"
	"os"
	"testing"

	"ergo.services/ergo/gen"

	"gserver/core/gxyredis"
	"gserver/protocol/pb"
)

// ========== ActorMgr ==========
// ActorMgr 是通用的"id → 进程引用"登记表,gateway 用它跟踪在线会话。

// TestActorMgr_AddAndGet: 登记表的核心往返——Add 写入的 PID,Get 必须按同一键原样取回同一身份。
// 为什么需要: 未命中时 Get 静默返回零值 PID,消费方用 PIDIsZero 判定"查无此会话";
// 取回失配会退化成同一条路径,把在线会话当成查无此会话而停止投递。
func TestActorMgr_AddAndGet(t *testing.T) {
	mgr := NewActorMgr("test")
	pid := pidFromLocal(gen.PID{Node: "node1", ID: 1})
	mgr.Add("id1", pid)
	got := mgr.Get("id1")
	if !PidEqual(got, pid) {
		t.Fatalf("expected %v, got %v", pid, got)
	}
}

// TestActorMgr_GetNotFound: 未注册的键必须回零值 PID(哨兵),不得残留上次值或 panic。
// 为什么需要: 哨兵是"无会话"的唯一表达;返回脏值会让已下线的角色被当成在线,
// 消息继续投给已销毁的 PID。
func TestActorMgr_GetNotFound(t *testing.T) {
	mgr := NewActorMgr("test")
	if got := mgr.Get("missing"); !PIDIsZero(got) {
		t.Fatalf("expected zero pid for missing key, got %v", got)
	}
}

// TestActorMgr_Remove: 会话终止后必须从表中摘净,Get 回零值 PID。
// 为什么需要: 漏摘一条,OnlinePlayers 计数永久偏高,gateway 停机遍历 All 时
// 还会对同一个已死 PID 重复调用 stopSession。
func TestActorMgr_Remove(t *testing.T) {
	mgr := NewActorMgr("test")
	mgr.Add("id1", pidFromLocal(gen.PID{Node: "node1", ID: 1}))
	mgr.Remove("id1")
	if got := mgr.Get("id1"); !PIDIsZero(got) {
		t.Fatalf("expected zero pid after remove, got %v", got)
	}
}

// TestActorMgr_Count: Count 等于当前登记条数,随 Add 递增、随 Remove 递减。
// 为什么需要: 会话建立与终止都把 Count 直接写进 OnlinePlayers 指标
// (见 session.go),计错会静默错报在线人数,扩容判断随之失真。
func TestActorMgr_Count(t *testing.T) {
	mgr := NewActorMgr("test")
	if mgr.Count() != 0 {
		t.Fatalf("expected 0, got %d", mgr.Count())
	}
	mgr.Add("a", pidFromLocal(gen.PID{Node: "n", ID: 1}))
	mgr.Add("b", pidFromLocal(gen.PID{Node: "n", ID: 2}))
	if mgr.Count() != 2 {
		t.Fatalf("expected 2, got %d", mgr.Count())
	}
	mgr.Remove("a")
	if mgr.Count() != 1 {
		t.Fatalf("expected 1, got %d", mgr.Count())
	}
}

// TestActorMgr_All: 返回当时登记的全部 PID 集合(本用例只断言条数)。
// 为什么需要: gateway 停机靠 All 逐个 stopSession 关闭会话,漏一个就有连接活过停机。
// 注意: All 用 make 新建切片返回副本,调用方遍历期间并发 Add/Remove 才安全,
// 这个"副本"性质当前没有任何用例断言——All 改成返回底层视图不会让测试失败。
func TestActorMgr_All(t *testing.T) {
	mgr := NewActorMgr("test")
	mgr.Add("a", pidFromLocal(gen.PID{Node: "n", ID: 1}))
	mgr.Add("b", pidFromLocal(gen.PID{Node: "n", ID: 2}))
	if all := mgr.All(); len(all) != 2 {
		t.Fatalf("expected 2, got %d", len(all))
	}
}

// TestActorMgr_AllEmpty: 空表必须返回长度为 0 的切片,不 panic。
// 为什么需要: 启动后从未有人登录的 gateway 停机时也要走完 All,
// 这里是那条路径上唯一的边界。
func TestActorMgr_AllEmpty(t *testing.T) {
	mgr := NewActorMgr("test")
	if all := mgr.All(); len(all) != 0 {
		t.Fatalf("expected 0, got %d", len(all))
	}
}

// TestActorMgr_Overwrite: 同一键二次 Add 是替换而非追加:Count 仍为 1,Get 命中新 PID。
// 为什么需要: 同一角色重连会复用同一个 RoleID 键;变成追加则旧 PID 仍留在表里,
// 停机时被重复关闭,而当前在线的会话没有对应条目。
func TestActorMgr_Overwrite(t *testing.T) {
	mgr := NewActorMgr("test")
	newPid := pidFromLocal(gen.PID{Node: "n2", ID: 9})
	mgr.Add("id", pidFromLocal(gen.PID{Node: "n1", ID: 1}))
	mgr.Add("id", newPid)
	if mgr.Count() != 1 {
		t.Fatalf("expected 1, got %d", mgr.Count())
	}
	if !PidEqual(mgr.Get("id"), newPid) {
		t.Fatal("expected overwritten pid")
	}
}

// ========== PidEqual ==========
// 进程标识是值类型:节点、序号、创建时刻三者共同决定身份。

// TestPidEqual_Same: 节点、序号、创建时刻三者全同的本机引用即同一身份。
// 为什么需要: PidEqual 是"是不是同一个会话/角色进程"的唯一判据,被 role 重复登录踢旧线
// (role_main.go)、会话下线摘角色 (gateway session.go 的 HandleDown) 两处依赖;
// 误判为不等 → 重复登录踢不掉旧连接,同一角色两条连接并存。
func TestPidEqual_Same(t *testing.T) {
	a := pidFromLocal(gen.PID{Node: "host", ID: 1, Creation: 100})
	b := pidFromLocal(gen.PID{Node: "host", ID: 1, Creation: 100})
	if !PidEqual(a, b) {
		t.Fatal("expected equal")
	}
}

// TestPidEqual_DifferentId: 序号不同即不同身份(同一节点上两个 actor 进程)。
// 为什么需要: 同节点重启后序号会重用到旧值,只有连同创建时刻一起比才不误判;
// 若序号被忽略,新进程会被当成旧进程仍在线,下线通知打不到正确对象。
func TestPidEqual_DifferentId(t *testing.T) {
	a := pidFromLocal(gen.PID{Node: "host", ID: 1})
	b := pidFromLocal(gen.PID{Node: "host", ID: 2})
	if PidEqual(a, b) {
		t.Fatal("expected not equal")
	}
}

// TestPidEqual_DifferentHost: 节点名不同的本机引用不是同一身份,即便序号相同。
// 为什么需要: 多节点部署时序号只在节点内唯一,漏比节点名会让另一台机器上的同序号
// actor 被误认为同一个,下线/踢线动作打到别人的进程上。
func TestPidEqual_DifferentHost(t *testing.T) {
	a := pidFromLocal(gen.PID{Node: "host1", ID: 1})
	b := pidFromLocal(gen.PID{Node: "host2", ID: 1})
	if PidEqual(a, b) {
		t.Fatal("expected not equal")
	}
}

// 创建时刻不同即视为不同实例:同一节点重启后,旧引用不得命中新进程。
func TestPidEqual_DifferentCreation(t *testing.T) {
	a := pidFromLocal(gen.PID{Node: "host", ID: 1, Creation: 100})
	b := pidFromLocal(gen.PID{Node: "host", ID: 1, Creation: 200})
	if PidEqual(a, b) {
		t.Fatal("expected not equal for different creation")
	}
}

// TestPidEqual_ZeroA: 零值 PID("尚无角色")与任何真实引用都不相等。
// 为什么需要: gateway HandleDown 先比 PidEqual(pid, s.sessionInfo.RolePid) 再摘角色,
// RolePid 为零(玩家还没进角色)时若判等,任何一次角色下线通知都会把尚未进角色的
// 连接直接停掉。
func TestPidEqual_ZeroA(t *testing.T) {
	if PidEqual(PID{}, pidFromLocal(gen.PID{Node: "h", ID: 1})) {
		t.Fatal("expected not equal with zero a")
	}
}

// TestPidEqual_ZeroB: 同上,零值在任一侧结果一致——PidEqual 必须对称。
// 为什么需要: 判据被两侧以不同参数顺序调用(HandleDown 与 role_main 相反),
// 不对称时只有一侧会误判成相等,踢线行为随调用点而变,极难复现。
func TestPidEqual_ZeroB(t *testing.T) {
	if PidEqual(pidFromLocal(gen.PID{Node: "h", ID: 1}), PID{}) {
		t.Fatal("expected not equal with zero b")
	}
}

// TestPidEqual_BothZero: 两个零值相等,使"零 == 零"与"零 == 真实"的行为可预测。
// 为什么需要: 依赖方靠零值相等来表示"本来就没有角色/会话",不需要额外分支;
// 若改成不等,HandleDown 里"尚未绑定角色"的哨兵就失去了可比较的语义。
func TestPidEqual_BothZero(t *testing.T) {
	if !PidEqual(PID{}, PID{}) {
		t.Fatal("expected equal for two zero pids")
	}
}

// ========== PIDIsZero ==========

func TestPIDIsZero(t *testing.T) {
	if !PIDIsZero(PID{}) {
		t.Fatal("zero pid must be reported as zero")
	}
	if PIDIsZero(pidFromLocal(gen.PID{Node: "h", ID: 1})) {
		t.Fatal("non-zero pid must not be reported as zero")
	}
}

// ========== actorKey ==========
// 所有权键的格式是对外契约(跨版本要读同一条记录),因此固定断言。

func TestActorKeyLocateKey(t *testing.T) {
	key := actorKey{kind: "role", id: "123"}.locateKey()
	expected := "gserver:locate:node:actor:role:123"
	if key != expected {
		t.Fatalf("expected %s, got %s", expected, key)
	}
}

func TestActorKeyLocateKey_EmptyKind(t *testing.T) {
	key := actorKey{kind: "", id: "456"}.locateKey()
	expected := "gserver:locate:node:actor::456"
	if key != expected {
		t.Fatalf("expected %s, got %s", expected, key)
	}
}

// 注册名带 kind 前缀,避免不同 kind 的相同 id 冲突。
func TestActorKeyName(t *testing.T) {
	k := actorKey{kind: "role", id: "7"}
	if got := k.name(); got != "role/7" {
		t.Fatalf("name = %q, want role/7", got)
	}
}

// 跨节点引用按"节点 + 注册名"构造,应答同样如此。
func TestActorKeyRemoteAddressing(t *testing.T) {
	k := actorKey{kind: "role", id: "7"}
	if got := k.remoteRef("node-b").Name(); got != "role/7" {
		t.Fatalf("remoteRef name = %q, want role/7", got)
	}
	if got := k.remoteRef("node-b").Node(); got != "node-b" {
		t.Fatalf("remoteRef node = %q, want node-b", got)
	}
	reply := k.actorPid("node-b")
	if reply.GetAddress() != "node-b" || reply.GetName() != "role/7" {
		t.Fatalf("reply = %+v, want {node-b role/7}", reply)
	}
}

// 应答与引用的名字必须一致:它们是同一个身份在两种通道上的表示。
func TestActorKeyReplyMatchesRemoteRef(t *testing.T) {
	k := actorKey{kind: "guild", id: "42"}
	if k.actorPid("n1").GetName() != string(k.name()) || k.remoteRef("n1").Name() != string(k.name()) {
		t.Fatal("应答与引用必须由同一个身份派生")
	}
}

// ========== 所有权获取（需要 Redis）==========

func TestClaimAndLocate(t *testing.T) {
	if os.Getenv("RUN_REDIS_TESTS") != "1" {
		t.Skip("set RUN_REDIS_TESTS=1 to run Redis integration tests")
	}
	redisApp := gxyredis.NewRedisApp()
	if err := redisApp.OnModInit(context.Background()); err != nil {
		t.Skipf("redis test config unavailable: %v", err)
	}
	if err := redisApp.OnModStart(context.Background()); err != nil {
		t.Skipf("redis unavailable: %v", err)
	}
	t.Cleanup(func() {
		_ = redisApp.OnModStop(context.Background())
	})

	mgr := NewActivatorManager("node@1")
	if err := mgr.lease.acquireNodeLease(context.Background()); err != nil {
		t.Fatalf("acquireNodeLease() error = %v", err)
	}
	owner, err := mgr.store.Claim(context.Background(), "role", "player-1")
	if err != nil {
		t.Fatalf("claim() error = %v", err)
	}
	t.Cleanup(func() {
		_, _ = mgr.store.Release(context.Background(), "role", "player-1", owner)
		_ = mgr.lease.releaseNodeLease(context.Background())
	})

	got, err := mgr.store.Locate(context.Background(), "role", "player-1")
	if err != nil {
		t.Fatalf("locate() error = %v", err)
	}
	if got != owner {
		t.Fatalf("owner = %+v, want %+v", got, owner)
	}
}

// ========== ActorError ==========

func TestActorError(t *testing.T) {
	err := ActorError("something failed")
	if err == nil {
		t.Fatal("expected non-nil")
	}
	if err.GetReason() != "something failed" {
		t.Fatalf("reason = %q, want %q", err.GetReason(), "something failed")
	}
}

var _ = pb.ActorError{}
