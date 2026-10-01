package logic

import (
	"context"
	"testing"
	"time"

	gamecfg "gserver/gameconfig/gosrc"
	"gserver/protocol/pb"
	"gserver/src/apps/role/internal/event"
	"gserver/src/apps/role/internal/logic/bag"
	"gserver/src/pkg/gameconfig"

	proto "google.golang.org/protobuf/proto"

	"ergo.services/ergo/testing/unit"
)

func initMainTaskTestConfig(t *testing.T) {
	t.Helper()
	initAllTestConfig(t)
}

func setupTestMainTask(t *testing.T) (*RoleMain, *unit.Subject, *RoleMainTask) {
	t.Helper()
	initMainTaskTestConfig(t)

	main, subj, _ := spawnTestRole(t, 1001)
	main.eventBus = event.NewEventBus()
	basicMod := &RoleBasic{
		RoleModule:     RoleModule{Role: main},
		RoleBasicState: RoleBasicState{Level: 1},
	}
	bagMod := &RoleBag{
		RoleModule:   RoleModule{Role: main},
		RoleBagState: RoleBagState{Goods: make(GoodsMap)},
	}
	flowerMod := &RoleFlower{
		RoleModule:      RoleModule{Role: main},
		RoleFlowerState: RoleFlowerState{Flowers: make(FlowerMap)},
	}
	plotMod := &RolePlot{
		RoleModule:    RoleModule{Role: main},
		RolePlotState: RolePlotState{Plots: make(PlotMap)},
	}
	mainTaskMod := &RoleMainTask{
		RoleModule: RoleModule{Role: main},
	}
	main.Basic = basicMod
	main.Bag = bagMod
	main.Flower = flowerMod
	main.Plot = plotMod
	main.MainTask = mainTaskMod
	if err := mainTaskMod.OnModInit(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := mainTaskMod.OnModStart(context.Background()); err != nil {
		t.Fatal(err)
	}
	return main, subj, mainTaskMod
}

func TestMainTaskInitFirstTask(t *testing.T) {
	_, _, mt := setupTestMainTask(t)

	if mt.CurrentTaskID != 1003 {
		t.Fatalf("expected first task 1003, got %d", mt.CurrentTaskID)
	}
	if mt.Progress != 0 {
		t.Fatalf("expected progress 0, got %d", mt.Progress)
	}
	if mt.Status != int32(pb.MainTaskStatus_MAIN_TASK_IN_PROGRESS) {
		t.Fatalf("expected in progress, got %d", mt.Status)
	}
}

func TestMainTaskAfterAcceptGoodEvent(t *testing.T) {
	main, _, mt := setupTestMainTask(t)
	mt.acceptTask(gameconfig.Get().TbMainTask.Get(1007))

	main.PublishRoleEvent(context.Background(), event.EVENT_GOOD_CHANGE, event.GoodChangeEventData{
		Changes: []event.GoodChange{{GoodID: 10001, PreNum: 0, Num: 2, AddNum: 2}},
	})

	if mt.Progress != 2 {
		t.Fatalf("expected progress 2, got %d", mt.Progress)
	}
	if mt.Status != int32(pb.MainTaskStatus_MAIN_TASK_CLAIMABLE) {
		t.Fatalf("expected claimable, got %d", mt.Status)
	}
}

func TestMainTaskClaimAdvancesAndNotifiesNextTask(t *testing.T) {
	_, subj, mt := setupTestMainTask(t)
	mt.Progress = 1
	mt.Status = int32(pb.MainTaskStatus_MAIN_TASK_CLAIMABLE)

	rsp, err := mt.ReqMainTaskClaim(context.Background(), &pb.ReqMainTaskClaim{})
	if err != nil {
		t.Fatal(err)
	}
	if rsp.Task.TaskId != 1003 {
		t.Fatalf("expected claimed task 1003, got %d", rsp.Task.TaskId)
	}
	if rsp.Task.Progress != 1 || rsp.Task.Status != pb.MainTaskStatus_MAIN_TASK_FINISHED {
		t.Fatalf("unexpected task state: %v", rsp.Task)
	}
	subj.ShouldSend().Where(clientMsgMatcher(func(m proto.Message) bool {
		notify, ok := m.(*pb.NotifyMainTaskUpdate)
		if !ok || notify.Task == nil {
			return false
		}
		return notify.Task.TaskId == 1004 && notify.Task.Status == pb.MainTaskStatus_MAIN_TASK_IN_PROGRESS
	})).Assert()
}

func TestMainTaskCurrentStateCompletesOnAccept(t *testing.T) {
	_, _, mt := setupTestMainTask(t)
	mt.Role.Basic.Level = 3
	mt.acceptTask(gameconfig.Get().TbMainTask.Get(1009))

	if mt.Progress != 3 {
		t.Fatalf("expected progress 3, got %d", mt.Progress)
	}
	if mt.Status != int32(pb.MainTaskStatus_MAIN_TASK_CLAIMABLE) {
		t.Fatalf("expected claimable, got %d", mt.Status)
	}
}

func TestMainTaskCurrentStateRefreshesOnEvent(t *testing.T) {
	main, subj, mt := setupTestMainTask(t)
	mt.Role.Basic.Level = 1
	mt.acceptTask(gameconfig.Get().TbMainTask.Get(1009))

	mt.Role.Basic.Level = 3
	main.PublishRoleEvent(context.Background(), event.EVENT_PLAYER_LEVEL, event.PlayerLevelEventData{
		OldLevel: 1,
		NewLevel: 3,
		Reason:   "test",
	})

	if mt.Progress != 3 {
		t.Fatalf("expected progress 3, got %d", mt.Progress)
	}
	if mt.Status != int32(pb.MainTaskStatus_MAIN_TASK_CLAIMABLE) {
		t.Fatalf("expected claimable, got %d", mt.Status)
	}
	subj.ShouldSend().Where(clientMsgMatcher(func(m proto.Message) bool {
		_, ok := m.(*pb.NotifyMainTaskUpdate)
		return ok
	})).Assert()
}

func TestMainTaskOwnItemCurrentState(t *testing.T) {
	_, _, mt := setupTestMainTask(t)
	mt.Role.Bag.Goods[10001] = bag.BagGood{GoodID: 10001, Num: 5}
	cfg := &gamecfg.GardenMainTask{
		TargetType:  gamecfg.GardenETaskTargetType_OWN_ITEM,
		TargetParam: 10001,
	}

	if got := CalcCurrentStateProgress(mt.Role, mt.Progress, cfg.TargetType, cfg.TargetParam); got != 5 {
		t.Fatalf("expected own item progress 5, got %d", got)
	}
}

func TestMainTaskBreedFinishCurrentStateHarvested(t *testing.T) {
	_, _, mt := setupTestMainTask(t)
	mt.Role.Flower.Flowers[101] = &FlowerData{
		FlowerID:  101,
		State:     int32(pb.FlowerState_FLOWER_HARVESTED),
		StateTime: time.Now(),
	}
	cfg := &gamecfg.GardenMainTask{
		TargetType:  gamecfg.GardenETaskTargetType_BREED_FINISH,
		TargetParam: 101,
	}

	if got := CalcCurrentStateProgress(mt.Role, mt.Progress, cfg.TargetType, cfg.TargetParam); got != 1 {
		t.Fatalf("expected breed finish progress 1, got %d", got)
	}
}

func TestMainTaskBreedFinishCurrentStateBreedDone(t *testing.T) {
	_, _, mt := setupTestMainTask(t)
	mt.Role.Flower.Flowers[101] = &FlowerData{
		FlowerID:  101,
		State:     int32(pb.FlowerState_FLOWER_BREEDING),
		StateTime: time.Now().Add(-time.Minute),
	}
	cfg := &gamecfg.GardenMainTask{
		TargetType:  gamecfg.GardenETaskTargetType_BREED_FINISH,
		TargetParam: 101,
	}

	if got := CalcCurrentStateProgress(mt.Role, mt.Progress, cfg.TargetType, cfg.TargetParam); got != 1 {
		t.Fatalf("expected breed finish progress 1, got %d", got)
	}
}
