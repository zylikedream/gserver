package gxyactor

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

// These regressions port the ADR 0006/0008 ownership contract onto the
// runtime-neutral activation seam. Each test injects failures exactly at the
// boundaries the Activator must defend: Claim, spawn, init confirmation,
// termination cleanup, lease expiry, and Redis availability.

// activationSpawner is the private seam the Activator uses to create actor
// processes after a successful Claim. The legacy legacy runtime bridge and the Ergo
// adapter both implement it; tests use it to observe and fail the spawn step.
type activationSpawner interface {
	spawnActivatorActor(kind, id string, owner ActorOwner) (PID, error)
	confirmActivatorActor(kind, id string, pid PID) error
	stopActivatorActor(pid PID) error
}

// fakeActivationSpawner records spawn requests and can be programmed to fail
// spawn or confirmation for specific actor ids.
type fakeActivationSpawner struct {
	mu          sync.Mutex
	spawned     []string
	confirmErrs map[string]error
	spawnErrs   map[string]error
	confirmed   []string
	stopped     []PID
	local       map[string]PID
}

func newFakeActivationSpawner() *fakeActivationSpawner {
	return &fakeActivationSpawner{
		confirmErrs: make(map[string]error),
		spawnErrs:   make(map[string]error),
		local:       make(map[string]PID),
	}
}

func (f *fakeActivationSpawner) spawnActivatorActor(kind, id string, owner ActorOwner) (PID, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err, ok := f.spawnErrs[id]; ok {
		return PID{}, err
	}
	f.spawned = append(f.spawned, id)
	pid := PID{Runtime: "ergo-v1", Node: "node-a", ID: kind + "/" + id, Creation: "41"}
	f.local[kind+"/"+id] = pid
	return pid, nil
}

func (f *fakeActivationSpawner) confirmActivatorActor(kind, id string, pid PID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err, ok := f.confirmErrs[id]; ok {
		return err
	}
	f.confirmed = append(f.confirmed, id)
	return nil
}

func (f *fakeActivationSpawner) stopActivatorActor(pid PID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stopped = append(f.stopped, pid)
	return nil
}

func (f *fakeActivationSpawner) spawnedOnce() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.spawned...)
}

func (f *fakeActivationSpawner) confirmCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.confirmed)
}

func (f *fakeActivationSpawner) stoppedPIDs() []PID {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]PID(nil), f.stopped...)
}

// newActivationTestHarness wires an actorActivator to a real miniredis-backed
// locator and a fake spawner, without starting the global actor application.
func newActivationTestHarness(t *testing.T) (*actorActivator, *activatorManager, *fakeActivationSpawner, *actorLocator, *miniredis.Miniredis) {
	t.Helper()
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	locator := newActorLocator(client, "node-a", "node-a")
	if err := locator.acquireNodeLease(context.Background()); err != nil {
		t.Fatal(err)
	}
	mgr := NewActivatorManager("node-a", "node-a")
	mgr.locator = locator
	mgr.serviceLookup = &activationTestLookup{addresses: map[string]string{"node-a": "node-a:1001"}}
	mgr.activatorMetas["role"] = &activatorMeta{Kind: "role", mgr: NewActorMgr("test")}
	// Replace the spawn path with the fake spawner so regressions can observe
	// and fail the Claim -> Spawn -> Confirm sequence deterministically.
	spawner := newFakeActivationSpawner()
	mgr.spawner = spawner
	activator := NewActorActivator("role", mgr)
	activator.meta = mgr.activatorMetas["role"]
	return activator, mgr, spawner, locator, server
}

// requestActivation drives the activator activation path synchronously so the
// regressions can assert on the returned PID or error.
func requestActivation(t *testing.T, activator *actorActivator, id string, allowSpawn bool) (PID, error) {
	t.Helper()
	return activator.requestLocal(context.Background(), id, allowSpawn)
}

// TestActivationConcurrentRequestsHaveSingleWinner verifies concurrent
// activation requests for the same kind/id produce exactly one spawned actor
// and all callers receive the same published PID.
func TestActivationConcurrentRequestsHaveSingleWinner(t *testing.T) {
	activator, _, spawner, _, _ := newActivationTestHarness(t)

	const callers = 8
	results := make(chan struct {
		pid PID
		err error
	}, callers)
	var wg sync.WaitGroup
	for range callers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			pid, err := requestActivation(t, activator, "player-1", true)
			results <- struct {
				pid PID
				err error
			}{pid, err}
		}()
	}
	wg.Wait()
	close(results)

	for result := range results {
		if result.err != nil {
			t.Fatalf("concurrent activation error: %v", result.err)
		}
		if result.pid.IsZero() {
			t.Fatal("concurrent activation returned zero PID")
		}
		if result.pid.ID != "role/player-1" {
			t.Fatalf("concurrent activation PID = %v, want role/player-1", result.pid)
		}
	}
	if got := len(spawner.spawnedOnce()); got != 1 {
		t.Fatalf("spawned actors = %d, want 1 (single winner)", got)
	}
	if got := spawner.confirmCount(); got != 1 {
		t.Fatalf("confirmation requests = %d, want 1", got)
	}
}

// TestActivationDuplicateLocalActivationFailsClosed verifies that when the
// local ActorMgr already holds the actor, a fresh claim that should spawn is
// rejected instead of creating a second local writer.
func TestActivationDuplicateLocalActivationFailsClosed(t *testing.T) {
	activator, mgr, spawner, _, _ := newActivationTestHarness(t)

	// A previous activation published the PID.
	first, err := requestActivation(t, activator, "player-1", true)
	if err != nil {
		t.Fatal(err)
	}
	if got := len(spawner.spawnedOnce()); got != 1 {
		t.Fatalf("first activation spawns = %d, want 1", got)
	}

	// Simulate the ownership directory losing the entry (stale delete), so the
	// next request performs a fresh claim that would allow spawn. The local
	// activation must still fail closed instead of double-spawning.
	if _, err := mgr.locator.release(context.Background(), "role", "player-1", ActorOwner{NodeID: "node-a", Epoch: 1}); err != nil {
		t.Fatal(err)
	}
	_ = first

	_, err = requestActivation(t, activator, "player-1", true)
	if err == nil {
		t.Fatal("duplicate local activation succeeded; second writer possible")
	}
	if got := len(spawner.spawnedOnce()); got != 1 {
		t.Fatalf("spawned actors after duplicate = %d, want 1", got)
	}
}

// TestActivationInitFailureReleasesOnlyMatchingOwner verifies a failed
// init confirmation stops the actor, conditionally releases the exact owner,
// and fails every waiter with ActorInitFailed.
func TestActivationInitFailureReleasesOnlyMatchingOwner(t *testing.T) {
	activator, _, spawner, locator, _ := newActivationTestHarness(t)

	spawner.confirmErrs["player-1"] = errTestActivationInitFailure

	pid, err := requestActivation(t, activator, "player-1", true)
	if err == nil {
		_ = pid
		t.Fatal("activation succeeded despite init failure")
	}
	if got := len(spawner.stoppedPIDs()); got != 1 {
		t.Fatalf("stopped actors after init failure = %d, want 1", got)
	}
	// Owner must have been conditionally released for the failed activation.
	owner, err := locator.locate(context.Background(), "role", "player-1")
	if err != nil {
		t.Fatal(err)
	}
	if owner.NodeID != "" {
		t.Fatalf("owner after init failure = %+v, want released", owner)
	}
}

// TestActivationTerminationCleanupCannotDeleteNewOwner verifies that a stale
// termination (old epoch) cannot release a directory entry now owned by a
// newer epoch after takeover.
func TestActivationTerminationCleanupCannotDeleteNewOwner(t *testing.T) {
	activator, _, _, locator, server := newActivationTestHarness(t)

	oldOwner, err := requestActivationOwner(t, activator, "player-1")
	if err != nil {
		t.Fatal(err)
	}

	// Lease expiry lets a new node take over with a greater epoch.
	server.Del(actorLocatorLeaseKey("node-a"))
	secondServerClient := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = secondServerClient.Close() })
	newLocator := newActorLocator(secondServerClient, "node-b", "node-b")
	if err := newLocator.acquireNodeLease(context.Background()); err != nil {
		t.Fatal(err)
	}
	newOwner, acquired, err := newLocator.claim(context.Background(), "role", "player-1")
	if err != nil || !acquired {
		t.Fatalf("takeover claim owner=%+v acquired=%v err=%v", newOwner, acquired, err)
	}
	if newOwner.Epoch <= oldOwner.Epoch {
		t.Fatalf("takeover epoch = %d, want > %d", newOwner.Epoch, oldOwner.Epoch)
	}

	// The old activation's termination cleanup must not delete the new owner.
	if deleted, err := locator.release(context.Background(), "role", "player-1", oldOwner); err != nil {
		t.Fatal(err)
	} else if deleted {
		t.Fatal("old epoch release deleted the new owner")
	}

	got, err := newLocator.locate(context.Background(), "role", "player-1")
	if err != nil {
		t.Fatal(err)
	}
	if got != newOwner {
		t.Fatalf("owner after stale cleanup = %+v, want %+v", got, newOwner)
	}
}

// TestActivationLeaseExpiryPermitsTakeoverWithGreaterEpoch verifies the
// end-to-end activation flow accepts a takeover only after the owner lease
// expired, and the new owner has a strictly greater epoch.
func TestActivationLeaseExpiryPermitsTakeoverWithGreaterEpoch(t *testing.T) {
	_, _, _, locator, server := newActivationTestHarness(t)

	oldOwner, acquired, err := locator.claim(context.Background(), "role", "player-1")
	if err != nil || !acquired {
		t.Fatalf("initial claim owner=%+v acquired=%v err=%v", oldOwner, acquired, err)
	}
	// Takeover while lease alive must fail.
	secondClient := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = secondClient.Close() })
	second := newActorLocator(secondClient, "node-b", "node-b")
	if err := second.acquireNodeLease(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, acquired, err := second.claim(context.Background(), "role", "player-1"); err != nil || acquired {
		t.Fatalf("claim with live owner acquired=%v err=%v", acquired, err)
	}
	// Lease expiry allows takeover with greater epoch.
	server.Del(actorLocatorLeaseKey("node-a"))
	newOwner, acquired, err := second.claim(context.Background(), "role", "player-1")
	if err != nil || !acquired {
		t.Fatalf("takeover claim owner=%+v acquired=%v err=%v", newOwner, acquired, err)
	}
	if newOwner.Epoch <= oldOwner.Epoch {
		t.Fatalf("takeover epoch = %d, want > %d", newOwner.Epoch, oldOwner.Epoch)
	}
}

// TestActivationRedisErrorFailsClosed verifies that a Redis error during
// activation returns an error and never spawns a local actor as fallback.
func TestActivationRedisErrorFailsClosed(t *testing.T) {
	activator, _, spawner, _, server := newActivationTestHarness(t)

	server.SetError("redis unavailable")

	if _, err := requestActivation(t, activator, "player-1", true); err == nil {
		t.Fatal("activation succeeded during Redis error")
	}
	if got := len(spawner.spawnedOnce()); got != 0 {
		t.Fatalf("spawned actors during Redis error = %d, want 0", got)
	}
}

// TestActivationAllowSpawnFalseNeverSpawns verifies the locate-only path can
// validate ownership but never creates a process even when no local actor
// exists and the claim would otherwise be acquireable.
func TestActivationAllowSpawnFalseNeverSpawns(t *testing.T) {
	activator, _, spawner, _, _ := newActivationTestHarness(t)
	if _, acquired, err := activator.manager.locator.claim(context.Background(), "role", "player-1"); err != nil || !acquired {
		t.Fatalf("seed locate-only owner acquired=%v err=%v", acquired, err)
	}

	if _, err := requestActivation(t, activator, "player-1", false); err == nil {
		t.Fatal("locate-only activation returned success without an actor")
	}
	if got := len(spawner.spawnedOnce()); got != 0 {
		t.Fatalf("locate-only activation spawned = %d, want 0", got)
	}
	if owner, err := activator.manager.locator.locate(context.Background(), "role", "player-1"); err != nil {
		t.Fatal(err)
	} else if owner.NodeID != "" {
		t.Fatalf("owner after locate-only request = %+v, want released", owner)
	}
}

// requestActivationOwner returns the owner under which the activation ran so
// tests can drive stale cleanup with the exact old epoch.
func requestActivationOwner(t *testing.T, activator *actorActivator, id string) (ActorOwner, error) {
	t.Helper()
	pid, err := requestActivation(t, activator, id, true)
	if err != nil {
		return ActorOwner{}, err
	}
	_ = pid
	return activator.owners[pid], nil
}

var errTestActivationInitFailure = errors.New("actor init failed or actor died")
