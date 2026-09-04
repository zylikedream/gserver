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
	return newActorLocator(client, "node-a", "token-a"), newActorLocator(client, "node-b", "token-b"), server
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
		owner    ActorOwner
		acquired bool
		err      error
	}
	results := make(chan claimResult, 2)
	var wg sync.WaitGroup
	for _, locator := range []*actorLocator{first, second} {
		wg.Add(1)
		go func(locator *actorLocator) {
			defer wg.Done()
			owner, acquired, err := locator.claim(ctx, "role", "player-1")
			results <- claimResult{owner: owner, acquired: acquired, err: err}
		}(locator)
	}
	wg.Wait()
	close(results)

	var winners int
	var owner ActorOwner
	for result := range results {
		if result.err != nil {
			t.Fatalf("claim error = %v", result.err)
		}
		if result.acquired {
			winners++
			owner = result.owner
		}
	}
	if winners != 1 {
		t.Fatalf("claim winners = %d, want 1", winners)
	}
	if owner.Epoch == 0 || (owner.NodeID != "node-a" && owner.NodeID != "node-b") {
		t.Fatalf("winner owner = %+v", owner)
	}
}

func TestActorLocatorClaimActiveOwnerReturnsExistingOwner(t *testing.T) {
	first, second, _ := newActorLocatorTestPair(t)
	ctx := context.Background()
	if err := first.acquireNodeLease(ctx); err != nil {
		t.Fatal(err)
	}
	if err := second.acquireNodeLease(ctx); err != nil {
		t.Fatal(err)
	}
	want, acquired, err := first.claim(ctx, "role", "player-1")
	if err != nil || !acquired {
		t.Fatalf("first claim = owner:%+v acquired:%v err:%v", want, acquired, err)
	}

	got, acquired, err := second.claim(ctx, "role", "player-1")
	if err != nil {
		t.Fatal(err)
	}
	if acquired {
		t.Fatal("second claim acquired active owner")
	}
	if got != want {
		t.Fatalf("existing owner = %+v, want %+v", got, want)
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
	want, acquired, err := first.claim(ctx, "role", "player-1")
	if err != nil || !acquired {
		t.Fatalf("first claim = owner:%+v acquired:%v err:%v", want, acquired, err)
	}

	server.FastForward(2 * time.Second)
	got, err := first.locate(ctx, "role", "player-1")
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("owner after node lease interval = %+v, want %+v", got, want)
	}
	_, acquired, err = second.claim(ctx, "role", "player-1")
	if err != nil {
		t.Fatal(err)
	}
	if acquired {
		t.Fatal("second node took over while first node lease was active")
	}
}

func TestActorLocatorClaimTakesOverExpiredOwnerWithNextEpoch(t *testing.T) {
	first, second, server := newActorLocatorTestPair(t)
	ctx := context.Background()
	if err := first.acquireNodeLease(ctx); err != nil {
		t.Fatal(err)
	}
	oldOwner, acquired, err := first.claim(ctx, "role", "player-1")
	if err != nil || !acquired {
		t.Fatalf("first claim = owner:%+v acquired:%v err:%v", oldOwner, acquired, err)
	}
	server.Del(actorLocatorLeaseKey(first.nodeID))
	if err := second.acquireNodeLease(ctx); err != nil {
		t.Fatal(err)
	}

	newOwner, acquired, err := second.claim(ctx, "role", "player-1")
	if err != nil {
		t.Fatal(err)
	}
	if !acquired {
		t.Fatal("takeover did not acquire expired owner")
	}
	if newOwner.NodeID != second.nodeID || newOwner.Epoch <= oldOwner.Epoch {
		t.Fatalf("new owner = %+v, old owner = %+v", newOwner, oldOwner)
	}
}

func TestActorLocatorClaimRejectsInvalidLease(t *testing.T) {
	first, _, _ := newActorLocatorTestPair(t)
	_, _, err := first.claim(context.Background(), "role", "player-1")
	if !errors.Is(err, errActorLocatorLeaseInvalid) {
		t.Fatalf("claim error = %v, want %v", err, errActorLocatorLeaseInvalid)
	}
}

func TestActorLocatorRenewDoesNotRefreshDifferentToken(t *testing.T) {
	first, _, server := newActorLocatorTestPair(t)
	ctx := context.Background()
	if err := first.acquireNodeLease(ctx); err != nil {
		t.Fatal(err)
	}
	stale := newActorLocator(first.redis, first.nodeID, "stale-token")
	server.FastForward(time.Second)
	refreshed, err := stale.renewNodeLease(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if refreshed {
		t.Fatal("stale token renewed active lease")
	}
}

func TestActorLocatorRedisErrorIsNotMiss(t *testing.T) {
	first, _, _ := newActorLocatorTestPair(t)
	if err := first.redis.Close(); err != nil {
		t.Fatal(err)
	}
	_, err := first.locate(context.Background(), "role", "missing")
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
	if _, acquired, err := first.claim(ctx, "role", "player-1"); err != nil || !acquired {
		t.Fatalf("claim acquired=%v err=%v", acquired, err)
	}
	server.Del(actorLocatorLeaseKey(first.nodeID))

	owner, err := first.locate(ctx, "role", "player-1")
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
	oldOwner, acquired, err := first.claim(ctx, "role", "player-1")
	if err != nil || !acquired {
		t.Fatalf("first claim owner=%+v acquired=%v err=%v", oldOwner, acquired, err)
	}
	if err := server.Set(actorLocatorLeaseKey(first.nodeID), "replacement-token"); err != nil {
		t.Fatal(err)
	}

	owner, err := first.locate(ctx, "role", "player-1")
	if err != nil {
		t.Fatal(err)
	}
	if owner.NodeID != "" {
		t.Fatalf("owner with mismatched lease token = %+v, want miss", owner)
	}

	if err := second.acquireNodeLease(ctx); err != nil {
		t.Fatal(err)
	}
	newOwner, acquired, err := second.claim(ctx, "role", "player-1")
	if err != nil || !acquired {
		t.Fatalf("takeover claim owner=%+v acquired=%v err=%v", newOwner, acquired, err)
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
	owner, acquired, err := first.claim(ctx, "role", "player-1")
	if err != nil || !acquired {
		t.Fatalf("claim owner=%+v acquired=%v err=%v", owner, acquired, err)
	}
	released, err := first.release(ctx, "role", "player-1", owner)
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
	oldOwner, acquired, err := first.claim(ctx, "role", "player-1")
	if err != nil || !acquired {
		t.Fatalf("first claim = owner:%+v acquired:%v err:%v", oldOwner, acquired, err)
	}
	server.Del(actorLocatorLeaseKey(first.nodeID))
	if err := second.acquireNodeLease(ctx); err != nil {
		t.Fatal(err)
	}
	newOwner, acquired, err := second.claim(ctx, "role", "player-1")
	if err != nil || !acquired {
		t.Fatalf("second claim = owner:%+v acquired:%v err:%v", newOwner, acquired, err)
	}

	if released, err := first.release(ctx, "role", "player-1", oldOwner); err != nil {
		t.Fatal(err)
	} else if released {
		t.Fatal("stale release deleted the new owner")
	}
	got, err := second.locate(ctx, "role", "player-1")
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
