package logic

import (
	"context"
	"testing"
	"time"

	"gserver/protocol/pb"

	"gorm.io/gorm"
)

func setupTestSteal(t *testing.T) *RoleSteal {
	t.Helper()
	plotCfgInited = false
	initPlotTestConfig(t)

	oldSnapshotStore := rolePlotSnapshots
	rolePlotSnapshots = newMemoryRolePlotSnapshotStore()
	t.Cleanup(func() { rolePlotSnapshots = oldSnapshotStore })
	oldLocks := plotLocks
	plotLocks = newMemoryPlotLockManager()
	t.Cleanup(func() { plotLocks = oldLocks })

	origIsFriend := isFriend
	isFriend = func(_ context.Context, _ *gorm.DB, _, _ int64) bool { return true }
	t.Cleanup(func() { isFriend = origIsFriend })
	origCountStolen := countPlotStolen
	countPlotStolen = func(_ context.Context, _ *gorm.DB, _ int64, _ int32) (int64, error) { return 0, nil }
	t.Cleanup(func() { countPlotStolen = origCountStolen })
	origHasStolen := hasStealRecord
	hasStealRecord = func(_ context.Context, _ *gorm.DB, _, _ int64, _ int32) bool { return false }
	t.Cleanup(func() { hasStealRecord = origHasStolen })
	origCreateSteal := createStealRecord
	createStealRecord = func(_ context.Context, _ *gorm.DB, _ *StealRecord) error { return nil }
	t.Cleanup(func() { createStealRecord = origCreateSteal })

	main := &RoleMain{RoleID: 1001}
	bagMod := &RoleBag{
		RoleModule:   RoleModule{RoleID: main.RoleID, Role: main},
		RoleBagState: RoleBagState{Goods: make(GoodsMap)},
	}
	stealMod := &RoleSteal{
		RoleModule:     RoleModule{RoleID: main.RoleID, Role: main},
		RoleStealState: RoleStealState{RolePersistState: RolePersistState{RoleID: main.RoleID}},
	}
	main.Bag = bagMod
	main.Steal = stealMod
	if err := stealMod.OnModInit(context.Background()); err != nil {
		t.Fatal(err)
	}
	return stealMod
}

func publishHarvestablePlotSnapshot(roleID int64) {
	publishRolePlotSnapshot(context.Background(), roleID, PlotMap{
		plotTestID: {
			PlotID:       plotTestID,
			FlowerID:     plotTestFlower,
			State:        int32(pb.PlotState_PLOT_GROWING),
			HarvestCount: 0,
			StateTime:    time.Now().Add(-time.Minute),
		},
	})
}

func TestReqPlotFriendInfoReadsSnapshot(t *testing.T) {
	steal := setupTestSteal(t)
	friendID := int64(2002)
	publishHarvestablePlotSnapshot(friendID)

	rsp, err := steal.ReqPlotFriendInfo(context.Background(), &pb.ReqPlotFriendInfo{FriendId: friendID})
	if err != nil {
		t.Fatal(err)
	}
	if len(rsp.Plots) != 1 {
		t.Fatalf("expected 1 plot, got %d", len(rsp.Plots))
	}
	if rsp.Plots[0].State != pb.PlotState_PLOT_HARVESTABLE {
		t.Fatalf("expected harvestable, got %v", rsp.Plots[0].State)
	}
	if !rsp.Plots[0].CanSteal {
		t.Fatal("expected can_steal from snapshot state")
	}
}

func TestReqPlotStealUsesPlotLock(t *testing.T) {
	steal := setupTestSteal(t)
	friendID := int64(2002)
	publishHarvestablePlotSnapshot(friendID)

	rsp, err := steal.ReqPlotSteal(context.Background(), &pb.ReqPlotSteal{FriendId: friendID, PlotId: plotTestID})
	if err != nil {
		t.Fatal(err)
	}
	if !rsp.Success {
		t.Fatal("expected success")
	}
	mem := plotLocks.(*memoryPlotLockManager)
	wantKey := plotLockKey(friendID, plotTestID)
	if len(mem.order) != 1 || mem.order[0] != wantKey {
		t.Fatalf("expected steal to lock %s, got %v", wantKey, mem.order)
	}
	if len(mem.held) != 0 {
		t.Fatalf("expected lock released, still held: %v", mem.held)
	}
}
