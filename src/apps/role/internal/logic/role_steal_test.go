package logic

import (
	"context"
	"reflect"
	"testing"
	"time"

	"gserver/protocol/pb"

	"github.com/agiledragon/gomonkey/v2"
	proto "google.golang.org/protobuf/proto"
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

	patchSend := gomonkey.ApplyMethod(reflect.TypeOf(&RoleMain{}), "SendClient",
		func(_ *RoleMain, _ context.Context, _ proto.Message) {},
	)
	t.Cleanup(patchSend.Reset)
	patchFriend := gomonkey.ApplyFunc(isFriend,
		func(_ context.Context, _, _ int64) bool { return true },
	)
	t.Cleanup(patchFriend.Reset)
	patchStolen := gomonkey.ApplyFunc(countPlotStolen,
		func(_ context.Context, _ int64, _ int32) (int64, error) { return 0, nil },
	)
	t.Cleanup(patchStolen.Reset)
	patchHasStolen := gomonkey.ApplyFunc(hasStealRecord,
		func(_ context.Context, _, _ int64, _ int32) bool { return false },
	)
	t.Cleanup(patchHasStolen.Reset)
	patchCreate := gomonkey.ApplyFunc(createStealRecord,
		func(_ context.Context, _ *StealRecord) error { return nil },
	)
	t.Cleanup(patchCreate.Reset)

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
