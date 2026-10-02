package gxyactor

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func newActorLocatorTestPair(t *testing.T) (*actorLocator, *actorLocator, *miniredis.Miniredis) {
	t.Helper()
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	return newActorLocator(client, "node-a"), newActorLocator(client, "node-b"), server
}

// TestActorLocatorClaimConcurrentHasSingleWinner:同一 actor 被两个节点同时 Claim 时,
// 仲裁脚本必须只让一个成功,落败方拿到的必须是 ErrNotOwner(竞争结局)而非技术错误。
// 为什么需要:两个节点都认为自己持有 → 同一个 actor 被并发激活,写同一份状态不报错,
// 只在数据里表现为互相覆盖。
func TestActorLocatorClaimConcurrentHasSingleWinner(t *testing.T) {
	first, second, _ := newActorLocatorTestPair(t)
	ctx := context.Background()
	if err := first.acquireNodeLease(ctx); err != nil {
		t.Fatal(err)
	}
	if err := second.acquireNodeLease(ctx); err != nil {
		t.Fatal(err)
	}

	type claimResult struct {
		owner ActorOwner
		err   error
	}
	results := make(chan claimResult, 2)
	var wg sync.WaitGroup
	for _, locator := range []*actorLocator{first, second} {
		wg.Add(1)
		go func(locator *actorLocator) {
			defer wg.Done()
			owner, err := locator.Claim(ctx, "role", "player-1")
			results <- claimResult{owner: owner, err: err}
		}(locator)
	}
	wg.Wait()
	close(results)

	var winners int
	var owner ActorOwner
	for result := range results {
		if result.err == nil {
			winners++
			owner = result.owner
			continue
		}
		// 落败是正常的竞争结局,不能是技术故障。
		if !errors.Is(result.err, ErrNotOwner) {
			t.Fatalf("claim error = %v, want ErrNotOwner", result.err)
		}
	}
	if winners != 1 {
		t.Fatalf("claim winners = %d, want 1", winners)
	}
	if owner.Epoch == 0 || (owner.NodeID != "node-a" && owner.NodeID != "node-b") {
		t.Fatalf("winner owner = %+v", owner)
	}
}

// TestActorLocatorClaimRejectsActiveOwner:owner 记录存在且原节点租约仍匹配时,
// 脚本返回 owned_by_other,Claim 转成 ErrNotOwner——未取得是正常结局,不是故障。
// 为什么需要:把"仍被活着的节点持有"误判成"无人持有"就会立刻抢走 actor,
// 双写静默发生;把竞争当故障上报则会让正常重试路径退化成报警。
func TestActorLocatorClaimRejectsActiveOwner(t *testing.T) {
	first, second, _ := newActorLocatorTestPair(t)
	ctx := context.Background()
	if err := first.acquireNodeLease(ctx); err != nil {
		t.Fatal(err)
	}
	if err := second.acquireNodeLease(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := first.Claim(ctx, "role", "player-1"); err != nil {
		t.Fatalf("first claim: %v", err)
	}

	// 记录在别的节点且它是活的:本次未取得,但不是技术故障。
	if _, err := second.Claim(ctx, "role", "player-1"); !errors.Is(err, ErrNotOwner) {
		t.Fatalf("claim on active owner = %v, want ErrNotOwner", err)
	}
}

// TestActorLocatorOwnerPersistsWhileNodeLeaseIsActive:租约 TTL 是"多久算过期",
// 不是"多久必须移交";只要租约还在,owner 记录就不能被别人接管,Locate 也要返回原 owner。
// 为什么需要:租约宽限期被误当过期时间 → 每次心跳间隔后归属就被漂移,
// 同一个 actor 在各节点间反复重建,状态在漂移窗口里丢失。
func TestActorLocatorOwnerPersistsWhileNodeLeaseIsActive(t *testing.T) {
	first, second, server := newActorLocatorTestPair(t)
	ctx := context.Background()
	if err := first.acquireNodeLease(ctx); err != nil {
		t.Fatal(err)
	}
	if err := second.acquireNodeLease(ctx); err != nil {
		t.Fatal(err)
	}
	want, err := first.Claim(ctx, "role", "player-1")
	if err != nil {
		t.Fatalf("first claim = owner:%+v err:%v", want, err)
	}

	server.FastForward(2 * time.Second)
	got, err := first.Locate(ctx, "role", "player-1")
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("owner after node lease interval = %+v, want %+v", got, want)
	}
	// 记录在别的节点且它是活的:本次必然未取得,且必须是 ErrNotOwner(不是故障)。
	if _, err = second.Claim(ctx, "role", "player-1"); !errors.Is(err, ErrNotOwner) {
		t.Fatalf("second node took over while first node lease was active: %v", err)
	}
}

// TestActorLocatorClaimTakesOverExpiredOwnerWithNextEpoch:原 owner 的租约失效后,
// 接管必须 INCR 全局 epoch,而不是复用旧 epoch——世代是围栏比对的依据。
// 为什么需要:epoch 不递增时,新旧持有者的 owner 值相同,围栏检查无法区分二者,
// 迟到的旧实例的续租/释放会被当成新持有者的合法操作。
func TestActorLocatorClaimTakesOverExpiredOwnerWithNextEpoch(t *testing.T) {
	first, second, server := newActorLocatorTestPair(t)
	ctx := context.Background()
	if err := first.acquireNodeLease(ctx); err != nil {
		t.Fatal(err)
	}
	oldOwner, err := first.Claim(ctx, "role", "player-1")
	if err != nil {
		t.Fatalf("first claim = owner:%+v err:%v", oldOwner, err)
	}
	server.Del(actorLocatorLeaseKey(first.nodeID))
	if err := second.acquireNodeLease(ctx); err != nil {
		t.Fatal(err)
	}

	newOwner, err := second.Claim(ctx, "role", "player-1")
	if err != nil {
		t.Fatal(err)
	}
	if newOwner.NodeID != second.nodeID || newOwner.Epoch <= oldOwner.Epoch {
		t.Fatalf("new owner = %+v, old owner = %+v", newOwner, oldOwner)
	}
}

// TestActorLocatorClaimRejectsInvalidLease:本节点从未取得租约(本地 deadline 为零值)时,
// Claim 必须在调用 Lua 前就拒绝,返回类型化的 errActorLocatorLeaseInvalid。
// 为什么需要:未持租约就写 owner 记录 = 把归属挂在一条别人随时能删的租约上,
// 后续 Locate 会把它当过期记录丢弃,actor 反复失联。
func TestActorLocatorClaimRejectsInvalidLease(t *testing.T) {
	first, _, _ := newActorLocatorTestPair(t)
	_, err := first.Claim(context.Background(), "role", "player-1")
	if !errors.Is(err, errActorLocatorLeaseInvalid) {
		t.Fatalf("claim error = %v, want %v", err, errActorLocatorLeaseInvalid)
	}
}

// TestActorLocatorClaimReportsInvalidLeaseFromScript:本地 deadline 尚有效,但脚本执行时
// Redis 里的租约令牌已被换掉——这条 fail-closed 分支必须映射成类型化租约错误,
// 而不是 decode/未知状态错误。
// 为什么需要:这里的错误类型决定调用方走"租约丢失"自栅还是走"解析失败"重试;
// 归错类会让节点带着已被接管的租约继续写数据。
func TestActorLocatorClaimReportsInvalidLeaseFromScript(t *testing.T) {
	first, _, server := newActorLocatorTestPair(t)
	ctx := context.Background()
	if err := first.acquireNodeLease(ctx); err != nil {
		t.Fatal(err)
	}
	// 本地 deadline 仍有效,但脚本执行时节点 lease 已被替换:
	// Lua 返回 invalid_lease,必须得到类型化的 lease 错误而非 decode 错误。
	if err := server.Set(actorLocatorLeaseKey(first.nodeID), "other-token"); err != nil {
		t.Fatal(err)
	}
	_, err := first.Claim(ctx, "role", "player-1")
	if !errors.Is(err, errActorLocatorLeaseInvalid) {
		t.Fatalf("claim error = %v, want typed invalid lease", err)
	}
}

// TestActorLocatorRenewDoesNotRefreshDifferentToken:续租脚本以令牌比对决定是否 PEXPIRE,
// 同节点名的另一个实例持有不同令牌,必须续租失败(不能刷新别人的租约)。
// 为什么需要:令牌比对一旦失效,同名新实例就能延长旧实例的租约,两个实例都认为
// 租约有效而都不围栏——静默的双持有。
func TestActorLocatorRenewDoesNotRefreshDifferentToken(t *testing.T) {
	first, _, server := newActorLocatorTestPair(t)
	ctx := context.Background()
	if err := first.acquireNodeLease(ctx); err != nil {
		t.Fatal(err)
	}
	// 同一节点名的另一个实例:租约令牌每实例随机,因此它不持有当前租约。
	other := newActorLocator(first.redis, first.nodeID)
	server.FastForward(time.Second)
	refreshed, err := other.renewNodeLease(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if refreshed {
		t.Fatal("a different instance renewed the active lease")
	}
}

// TestActorLocatorRedisErrorIsNotMiss:Redis 连接断开时 Locate 必须返回错误,
// 而不是把它报告成"无归属"的零值。
// 为什么需要:调用方对 miss 与 error 的处置不同(miss 触发重建,fail closed 或重试);
// 把 Redis 抖动当 miss 会在全 Redis 故障时把所有 actor 同时重建、放大故障。
func TestActorLocatorRedisErrorIsNotMiss(t *testing.T) {
	first, _, _ := newActorLocatorTestPair(t)
	if err := first.redis.Close(); err != nil {
		t.Fatal(err)
	}
	_, err := first.Locate(context.Background(), "role", "missing")
	if err == nil {
		t.Fatal("locate returned nil error for Redis failure")
	}
}

// TestActorLocatorLocateExpiredOwnerReturnsMiss:owner 记录还在但其节点租约已失效时,
// Locate 脚本必须把它当不存在返回——记录存在不等于归属有效。
// 为什么需要:把过期记录当真会把消息路由到已死的节点,actor 永久失联且无人重建。
func TestActorLocatorLocateExpiredOwnerReturnsMiss(t *testing.T) {
	first, _, server := newActorLocatorTestPair(t)
	ctx := context.Background()
	if err := first.acquireNodeLease(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := first.Claim(ctx, "role", "player-1"); err != nil {
		t.Fatalf("claim err=%v", err)
	}
	server.Del(actorLocatorLeaseKey(first.nodeID))

	owner, err := first.Locate(ctx, "role", "player-1")
	if err != nil {
		t.Fatal(err)
	}
	if owner.NodeID != "" {
		t.Fatalf("expired owner = %+v, want miss", owner)
	}
}

// TestActorLocatorRejectsOwnerWhenLeaseTokenDiffers:owner 值里的令牌与该节点当前租约不符时,
// 归属视为无效(miss);这条路径走完再接管,epoch 仍必须继续递增。
// 为什么需要:只校验"记录存在"而不校验"租约仍是同一次实例",会让已经交接过的 actor
// 继续被判给旧世代,围栏与接管两端的世代比较都失去意义。
func TestActorLocatorRejectsOwnerWhenLeaseTokenDiffers(t *testing.T) {
	first, second, server := newActorLocatorTestPair(t)
	ctx := context.Background()
	if err := first.acquireNodeLease(ctx); err != nil {
		t.Fatal(err)
	}
	oldOwner, err := first.Claim(ctx, "role", "player-1")
	if err != nil {
		t.Fatalf("first claim owner=%+v err=%v", oldOwner, err)
	}
	if err := server.Set(actorLocatorLeaseKey(first.nodeID), "replacement-token"); err != nil {
		t.Fatal(err)
	}

	owner, err := first.Locate(ctx, "role", "player-1")
	if err != nil {
		t.Fatal(err)
	}
	if owner.NodeID != "" {
		t.Fatalf("owner with mismatched lease token = %+v, want miss", owner)
	}

	if err := second.acquireNodeLease(ctx); err != nil {
		t.Fatal(err)
	}
	newOwner, err := second.Claim(ctx, "role", "player-1")
	if err != nil {
		t.Fatalf("takeover claim owner=%+v err=%v", newOwner, err)
	}
	if newOwner.Epoch <= oldOwner.Epoch {
		t.Fatalf("takeover epoch = %d, want greater than %d", newOwner.Epoch, oldOwner.Epoch)
	}
}

// TestActorLocatorReleaseReportsMatch:持有者释放自己的归属时脚本返回 1,
// Release 必须如实报告"确实删掉了",供调用方区分释放失败。
// 为什么需要:返回值恒为 false 会让调用方无法判断记录是否残留,
// 残留的 owner 记录会让下一个接管者一直拿到 ErrNotOwner。
func TestActorLocatorReleaseReportsMatch(t *testing.T) {
	first, _, _ := newActorLocatorTestPair(t)
	ctx := context.Background()
	if err := first.acquireNodeLease(ctx); err != nil {
		t.Fatal(err)
	}
	owner, err := first.Claim(ctx, "role", "player-1")
	if err != nil {
		t.Fatalf("claim owner=%+v err=%v", owner, err)
	}
	released, err := first.Release(ctx, "role", "player-1", owner)
	if err != nil {
		t.Fatal(err)
	}
	if !released {
		t.Fatal("matching owner was not released")
	}
}

// TestActorLocatorReleaseDoesNotDeleteNewOwner:过期实例的迟到 Release 带着自己的旧 owner
// 值,脚本比对不相等 → 返回 false 且**不得**删掉新持有者的记录。
// 为什么需要:release 一旦无条件,旧实例停机收尾就会删掉新持有者的所有权记录,
// 新持有者随即被围栏、actor 永久失联。
func TestActorLocatorReleaseDoesNotDeleteNewOwner(t *testing.T) {
	first, second, server := newActorLocatorTestPair(t)
	ctx := context.Background()
	if err := first.acquireNodeLease(ctx); err != nil {
		t.Fatal(err)
	}
	oldOwner, err := first.Claim(ctx, "role", "player-1")
	if err != nil {
		t.Fatalf("first claim = owner:%+v err:%v", oldOwner, err)
	}
	server.Del(actorLocatorLeaseKey(first.nodeID))
	if err := second.acquireNodeLease(ctx); err != nil {
		t.Fatal(err)
	}
	newOwner, err := second.Claim(ctx, "role", "player-1")
	if err != nil {
		t.Fatalf("second claim = owner:%+v err:%v", newOwner, err)
	}

	if released, err := first.Release(ctx, "role", "player-1", oldOwner); err != nil {
		t.Fatal(err)
	} else if released {
		t.Fatal("stale release deleted the new owner")
	}
	got, err := second.Locate(ctx, "role", "player-1")
	if err != nil {
		t.Fatal(err)
	}
	if got != newOwner {
		t.Fatalf("owner after stale release = %+v, want %+v", got, newOwner)
	}
}

// TestActorLocatorTokenMismatchFencesImmediately:Redis 里的租约被换成别的令牌后,
// 下一次续租返回 refreshed=false,租约心跳必须**立即**围栏(errActorLocatorLeaseInvalid,
// 见 actor_locator.go 的 finishRenew:refreshed 为假即围栏),而不是等租约 TTL 到期。
// 为什么需要:令牌不符意味着另一个实例已接管本节点。留着 fenced=false 继续服务会与
// 新持有者并发写同一 actor,造成静默的重复落库——不报错,只丢数据。
func TestActorLocatorTokenMismatchFencesImmediately(t *testing.T) {
	locator, _, server := newActorLocatorTestPair(t)
	locator.heartbeatInterval = 5 * time.Millisecond
	locator.leaseTTL = time.Second
	ctx := context.Background()
	if err := locator.acquireNodeLease(ctx); err != nil {
		t.Fatal(err)
	}
	if err := server.Set(actorLocatorLeaseKey(locator.nodeID), "different-token"); err != nil {
		t.Fatal(err)
	}

	lost := make(chan error, 1)
	stop := locator.startLeaseHeartbeat(ctx, func(err error) { lost <- err })
	defer stop()

	select {
	case err := <-lost:
		if !errors.Is(err, errActorLocatorLeaseInvalid) {
			t.Fatalf("lease loss error = %v, want invalid lease", err)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("token mismatch did not fence locator")
	}
}

// TestActorLocatorRenewalErrorsFenceAtDeadlineOnce:续租出错(如 Redis 断开)时
// finishRenew 只记录 lastErr、**不**围栏,心跳继续;围栏推迟到已确认的租约 deadline,
// 由 deadlineTimer 分支在 run 循环里触发。本测试断言两个契约:围栏确实发生了,
// 且丢失回调**恰好触发一次**——重复回调会让每个调用方各自处置一次"租约已丢",
// 产生重复终止。
func TestActorLocatorRenewalErrorsFenceAtDeadlineOnce(t *testing.T) {
	locator, _, _ := newActorLocatorTestPair(t)
	locator.heartbeatInterval = 5 * time.Millisecond
	locator.leaseTTL = 40 * time.Millisecond
	ctx := context.Background()
	if err := locator.acquireNodeLease(ctx); err != nil {
		t.Fatal(err)
	}
	if err := locator.redis.Close(); err != nil {
		t.Fatal(err)
	}

	var callbacks atomic.Int32
	lost := make(chan error, 1)
	stop := locator.startLeaseHeartbeat(ctx, func(err error) {
		callbacks.Add(1)
		lost <- err
	})
	defer stop()

	select {
	case <-lost:
	case <-time.After(250 * time.Millisecond):
		t.Fatal("renewal errors did not fence at the confirmed deadline")
	}
	time.Sleep(30 * time.Millisecond)
	if got := callbacks.Load(); got != 1 {
		t.Fatalf("lease loss callbacks = %d, want 1", got)
	}
}

// TestActorLocatorBlockedRenewalCannotDelayDeadlineFence:续租 goroutine 被卡住
// (阻塞到 ctx 结束)时,deadlineTimer 必须独立触发围栏。renewing 标志位使后续 tick
// 跳过再发起续租(startRenew 在已有续租在飞时直接返回),若围栏依赖续租返回,
// 一个卡住的 Redis 会让本节点无限期持有过期租约继续服务。错误是
// errActorLocatorLeaseDeadline——围栏来自 deadlineTimer 分支而非续租结果。
func TestActorLocatorBlockedRenewalCannotDelayDeadlineFence(t *testing.T) {
	locator, _, _ := newActorLocatorTestPair(t)
	locator.heartbeatInterval = 5 * time.Millisecond
	locator.leaseTTL = 40 * time.Millisecond
	ctx := context.Background()
	if err := locator.acquireNodeLease(ctx); err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	locator.renewLease = func(ctx context.Context) (bool, error) {
		close(started)
		<-ctx.Done()
		return false, ctx.Err()
	}

	lost := make(chan error, 1)
	stop := locator.startLeaseHeartbeat(ctx, func(err error) { lost <- err })
	defer stop()
	select {
	case <-started:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("renewal did not start")
	}
	select {
	case err := <-lost:
		if !errors.Is(err, errActorLocatorLeaseDeadline) {
			t.Fatalf("lease loss error = %v, want deadline exceeded", err)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("blocked renewal delayed lease deadline fence")
	}
}

// TestActorLocatorSuccessfulRenewalsExtendDeadline:续租成功要按发起时刻重设本地 deadline,
// 连续成功跨越多个 TTL 也不得围栏——租约靠心跳续命,不是一次性快照。
// 为什么需要:deadline 不顺延(或按完成时刻顺延,飞快的续租还会原地把租约锁死过期),
// 正常运行的节点会被自己的心跳判定为租约丢失并停服。
func TestActorLocatorSuccessfulRenewalsExtendDeadline(t *testing.T) {
	locator, _, _ := newActorLocatorTestPair(t)
	locator.heartbeatInterval = 5 * time.Millisecond
	locator.leaseTTL = 40 * time.Millisecond
	ctx := context.Background()
	if err := locator.acquireNodeLease(ctx); err != nil {
		t.Fatal(err)
	}

	lost := make(chan error, 1)
	stop := locator.startLeaseHeartbeat(ctx, func(err error) { lost <- err })
	defer stop()
	time.Sleep(100 * time.Millisecond)

	select {
	case err := <-lost:
		t.Fatalf("successful renewals fenced locator: %v", err)
	default:
	}
	if !locator.leaseValid(time.Now()) {
		t.Fatal("successful renewals did not extend local lease deadline")
	}
}

// TestActorLocatorGracefulStopDoesNotFence:心跳经 stop() 主动取消属于正常停机,
// 不触发 lost 回调;停机路径与围栏路径必须区分开。
// 为什么需要:把正常停机当租约丢失上报,会让每次优雅停机都走一遍"租约已丢"的
// 兜底处置(告警、重复清理),把计划内重启变成故障。
func TestActorLocatorGracefulStopDoesNotFence(t *testing.T) {
	locator, _, _ := newActorLocatorTestPair(t)
	locator.heartbeatInterval = 5 * time.Millisecond
	locator.leaseTTL = 40 * time.Millisecond
	ctx := context.Background()
	if err := locator.acquireNodeLease(ctx); err != nil {
		t.Fatal(err)
	}

	lost := make(chan error, 1)
	stop := locator.startLeaseHeartbeat(ctx, func(err error) { lost <- err })
	stop()
	time.Sleep(50 * time.Millisecond)

	select {
	case err := <-lost:
		t.Fatalf("graceful stop fenced locator: %v", err)
	default:
	}
}
