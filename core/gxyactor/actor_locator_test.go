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

func TestActorLocatorClaimRejectsInvalidLease(t *testing.T) {
	first, _, _ := newActorLocatorTestPair(t)
	_, err := first.Claim(context.Background(), "role", "player-1")
	if !errors.Is(err, errActorLocatorLeaseInvalid) {
		t.Fatalf("claim error = %v, want %v", err, errActorLocatorLeaseInvalid)
	}
}

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
