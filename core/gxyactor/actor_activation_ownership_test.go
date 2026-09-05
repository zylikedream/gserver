package gxyactor

import (
	"context"
	"errors"
	"testing"

	"gserver/core/gxyregistery"

	"github.com/asynkron/protoactor-go/actor"
)

type activationTestLookup struct {
	addresses map[string]string
	candidate *gxyregistery.ServiceInfo
}

func (l *activationTestLookup) GetAddressByNodeName(_ context.Context, _ string, nodeInstanceName string) string {
	return l.addresses[nodeInstanceName]
}

func (l *activationTestLookup) GetServiceInfo(context.Context, string, string, gxyregistery.ServiceSelector) *gxyregistery.ServiceInfo {
	return l.candidate
}

func TestActivationClaimLostRetries(t *testing.T) {
	action := decideActivation(ActorOwner{NodeID: "node-b", Epoch: 7}, false, "node-a", nil, true)
	if action != activationRetry {
		t.Fatalf("action = %v, want retry", action)
	}
}

func TestActivationExistingLocalActorReturnsPID(t *testing.T) {
	pid := actor.NewPID("node-a:1", "player-1")
	action := decideActivation(ActorOwner{NodeID: "node-a", Epoch: 7}, false, "node-a", pid, false)
	if action != activationReturnLocal {
		t.Fatalf("action = %v, want return local", action)
	}
}

func TestActivationMissingLocalActorReleasesAndRetries(t *testing.T) {
	action := decideActivation(ActorOwner{NodeID: "node-a", Epoch: 7}, false, "node-a", nil, false)
	if action != activationReleaseAndRetry {
		t.Fatalf("action = %v, want release and retry", action)
	}
}

func TestActivationClaimedResolveOnlyReleasesAndRetries(t *testing.T) {
	action := decideActivation(ActorOwner{NodeID: "node-a", Epoch: 7}, true, "node-a", nil, false)
	if action != activationReleaseAndRetry {
		t.Fatalf("action = %v, want release and retry", action)
	}
}

func TestActivationClaimWinnerCanSpawn(t *testing.T) {
	action := decideActivation(ActorOwner{NodeID: "node-a", Epoch: 7}, true, "node-a", nil, true)
	if action != activationSpawn {
		t.Fatalf("action = %v, want spawn", action)
	}
}

func TestActivationClaimedWithExistingLocalActorConflicts(t *testing.T) {
	pid := actor.NewPID("node-a:1", "player-1")
	action := decideActivation(ActorOwner{NodeID: "node-a", Epoch: 7}, true, "node-a", pid, true)
	if action != activationConflict {
		t.Fatalf("action = %v, want conflict", action)
	}
}

func TestGetActorRetryRelocatesOfflineActor(t *testing.T) {
	ownerLocator, callerLocator, _ := newActorLocatorTestPair(t)
	ctx := context.Background()
	if err := ownerLocator.acquireNodeLease(ctx); err != nil {
		t.Fatal(err)
	}
	owner, acquired, err := ownerLocator.claim(ctx, "role", "player-1")
	if err != nil || !acquired {
		t.Fatalf("claim owner=%+v acquired=%v err=%v", owner, acquired, err)
	}

	mgr := NewActivatorManager("node-b", "node-b")
	mgr.locator = callerLocator
	mgr.serviceLookup = &activationTestLookup{
		addresses: map[string]string{"node-a": "node-a:1001"},
		candidate: gxyregistery.NewServiceInfo("role", "node-b", "node-b:1002", "test", 1),
	}
	var calls []bool
	mgr.requestActorFunc = func(ctx context.Context, node, kind, id string, allowSpawn bool) (PID, bool, error) {
		calls = append(calls, allowSpawn)
		if !allowSpawn {
			_, err := ownerLocator.release(ctx, kind, id, owner)
			return nil, true, err
		}
		return actor.NewPID(node, id), false, nil
	}

	pid, err := mgr.getActor(ctx, "role", "player-1", true)
	if err != nil {
		t.Fatal(err)
	}
	if pid == nil || pid.Address != "node-b:1002" {
		t.Fatalf("pid = %v, want node-b:1002", pid)
	}
	if len(calls) != 2 || calls[0] || !calls[1] {
		t.Fatalf("allowSpawn calls = %v, want [false true]", calls)
	}
}

func TestGetActorWithoutSpawnReturnsNotFoundAfterStaleCleanup(t *testing.T) {
	ownerLocator, callerLocator, _ := newActorLocatorTestPair(t)
	ctx := context.Background()
	if err := ownerLocator.acquireNodeLease(ctx); err != nil {
		t.Fatal(err)
	}
	owner, acquired, err := ownerLocator.claim(ctx, "role", "player-1")
	if err != nil || !acquired {
		t.Fatalf("claim owner=%+v acquired=%v err=%v", owner, acquired, err)
	}

	mgr := NewActivatorManager("node-b", "node-b")
	mgr.locator = callerLocator
	mgr.serviceLookup = &activationTestLookup{addresses: map[string]string{"node-a": "node-a:1001"}}
	requests := 0
	mgr.requestActorFunc = func(ctx context.Context, _, kind, id string, allowSpawn bool) (PID, bool, error) {
		requests++
		if allowSpawn {
			t.Fatal("spawn request sent for spawn=false lookup")
		}
		_, err := ownerLocator.release(ctx, kind, id, owner)
		return nil, true, err
	}

	if _, err := mgr.getActor(ctx, "role", "player-1", false); err == nil {
		t.Fatal("getActor returned nil error for missing actor")
	}
	if requests != 1 {
		t.Fatalf("requests = %d, want 1", requests)
	}
}

func TestGetActorDoesNotStealWhenOwnerAddressUnavailable(t *testing.T) {
	ownerLocator, callerLocator, _ := newActorLocatorTestPair(t)
	ctx := context.Background()
	if err := ownerLocator.acquireNodeLease(ctx); err != nil {
		t.Fatal(err)
	}
	owner, acquired, err := ownerLocator.claim(ctx, "role", "player-1")
	if err != nil || !acquired {
		t.Fatalf("claim owner=%+v acquired=%v err=%v", owner, acquired, err)
	}

	mgr := NewActivatorManager("node-b", "node-b")
	mgr.locator = callerLocator
	mgr.serviceLookup = &activationTestLookup{
		addresses: map[string]string{},
		candidate: gxyregistery.NewServiceInfo("role", "node-b", "node-b:1002", "test", 1),
	}
	mgr.requestActorFunc = func(context.Context, string, string, string, bool) (PID, bool, error) {
		t.Fatal("requestActor called without an address for the live owner")
		return nil, false, nil
	}

	if _, err := mgr.getActor(ctx, "role", "player-1", true); err == nil {
		t.Fatal("getActor accepted an unavailable address for a live owner")
	}
	got, err := callerLocator.locate(ctx, "role", "player-1")
	if err != nil {
		t.Fatal(err)
	}
	if got != owner {
		t.Fatalf("owner after unavailable address = %+v, want %+v", got, owner)
	}
}

func TestGetActorRetryIsBounded(t *testing.T) {
	ownerLocator, callerLocator, _ := newActorLocatorTestPair(t)
	ctx := context.Background()
	if err := ownerLocator.acquireNodeLease(ctx); err != nil {
		t.Fatal(err)
	}
	if _, acquired, err := ownerLocator.claim(ctx, "role", "player-1"); err != nil || !acquired {
		t.Fatalf("claim acquired=%v err=%v", acquired, err)
	}

	mgr := NewActivatorManager("node-b", "node-b")
	mgr.locator = callerLocator
	mgr.serviceLookup = &activationTestLookup{addresses: map[string]string{"node-a": "node-a:1001"}}
	requests := 0
	mgr.requestActorFunc = func(context.Context, string, string, string, bool) (PID, bool, error) {
		requests++
		return nil, true, nil
	}

	if _, err := mgr.getActor(ctx, "role", "player-1", true); !errors.Is(err, errActorLocateRetryExhausted) {
		t.Fatalf("getActor error = %v, want retry exhausted", err)
	}
	if requests != actorLocateMaxAttempts {
		t.Fatalf("requests = %d, want %d", requests, actorLocateMaxAttempts)
	}
}
