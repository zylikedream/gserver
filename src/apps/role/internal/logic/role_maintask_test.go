package logic

import (
	"context"
	"reflect"
	"testing"

	"gserver/gameconfig"
	gamecfg "gserver/gameconfig/gosrc"
	"gserver/protocol/pb"
	"gserver/src/apps/role/internal/event"
	"gserver/src/apps/role/internal/logic/bag"

	"github.com/agiledragon/gomonkey/v2"
	proto "google.golang.org/protobuf/proto"
)

func initMainTaskTestConfig(t *testing.T) {
	t.Helper()
	gc := gameconfig.NewGameConfig()

	items := loadTestTable(t, "garden_tbitem")
	tbItem, err := gamecfg.NewGardenTbItem(items)
	if err != nil {
		t.Fatal(err)
	}
	flowers := loadTestTable(t, "garden_tbflower")
	tbFlower, err := gamecfg.NewGardenTbFlower(flowers)
	if err != nil {
		t.Fatal(err)
	}
	plots := loadTestTable(t, "garden_tbgardenplot")
	tbPlot, err := gamecfg.NewGardenTbGardenPlot(plots)
	if err != nil {
		t.Fatal(err)
	}
	playerLevels := loadTestTable(t, "garden_tbplayerlevel")
	tbPlayerLevel, err := gamecfg.NewGardenTbPlayerLevel(playerLevels)
	if err != nil {
		t.Fatal(err)
	}
	mainTasks := loadTestTable(t, "garden_tbmaintask")
	tbMainTask, err := gamecfg.NewGardenTbMainTask(mainTasks)
	if err != nil {
		t.Fatal(err)
	}

	gc.Tables = &gamecfg.Tables{
		TbItem:        tbItem,
		TbFlower:      tbFlower,
		TbGardenPlot:  tbPlot,
		TbPlayerLevel: tbPlayerLevel,
		TbMainTask:    tbMainTask,
	}
	if gc.GetFirstMainTask() == nil {
		t.Fatal("expected first main task config")
	}
}

func setupTestMainTask(t *testing.T) (*RoleMain, *RoleMainTask, *[]proto.Message) {
	t.Helper()
	initMainTaskTestConfig(t)

	var sent []proto.Message
	patch := gomonkey.ApplyMethod(reflect.TypeOf(&RoleMain{}), "SendClient",
		func(_ *RoleMain, _ context.Context, msg proto.Message) {
			sent = append(sent, msg)
		},
	)
	t.Cleanup(patch.Reset)

	main := &RoleMain{eventBus: event.NewEventBus()}
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
	return main, mainTaskMod, &sent
}

func TestMainTaskInitFirstTask(t *testing.T) {
	_, mt, _ := setupTestMainTask(t)

	if mt.CurrentTaskID != 1001 {
		t.Fatalf("expected first task 1001, got %d", mt.CurrentTaskID)
	}
	if mt.Progress != 0 {
		t.Fatalf("expected progress 0, got %d", mt.Progress)
	}
	if mt.Status != int32(pb.MainTaskStatus_MAIN_TASK_IN_PROGRESS) {
		t.Fatalf("expected in progress, got %d", mt.Status)
	}
}

func TestMainTaskAfterAcceptGoodEvent(t *testing.T) {
	main, mt, _ := setupTestMainTask(t)

	main.PublishRoleEvent(event.EVENT_GOOD_CHANGE, event.GoodChangeEventData{
		Changes: []event.GoodChange{{GoodID: GOLD_ITEM_ID, PreNum: 0, Num: 1, AddNum: 1}},
	})

	if mt.Progress != 1 {
		t.Fatalf("expected progress 1, got %d", mt.Progress)
	}
	if mt.Status != int32(pb.MainTaskStatus_MAIN_TASK_CLAIMABLE) {
		t.Fatalf("expected claimable, got %d", mt.Status)
	}
}

func TestMainTaskClaimAdvancesAndNotifiesNextTask(t *testing.T) {
	_, mt, sent := setupTestMainTask(t)
	mt.Progress = 1
	mt.Status = int32(pb.MainTaskStatus_MAIN_TASK_CLAIMABLE)

	rsp, err := mt.ReqClaimMainTask(context.Background(), &pb.ReqClaimMainTask{})
	if err != nil {
		t.Fatal(err)
	}
	if rsp.Task.TaskId != 1002 {
		t.Fatalf("expected next task 1002, got %d", rsp.Task.TaskId)
	}
	if rsp.Task.Progress != 0 || rsp.Task.Status != pb.MainTaskStatus_MAIN_TASK_IN_PROGRESS {
		t.Fatalf("unexpected task state: %v", rsp.Task)
	}
	if len(*sent) == 0 {
		t.Fatal("expected task update notification")
	}
	if _, ok := (*sent)[len(*sent)-1].(*pb.NotifyMainTaskUpdate); !ok {
		t.Fatalf("expected NotifyMainTaskUpdate, got %T", (*sent)[len(*sent)-1])
	}
}

func TestMainTaskCurrentStateCompletesOnAccept(t *testing.T) {
	_, mt, _ := setupTestMainTask(t)
	mt.Role.Basic.Level = 3
	mt.acceptTask(gameconfig.GameConfig().TbMainTask.Get(1009))

	if mt.Progress != 3 {
		t.Fatalf("expected progress 3, got %d", mt.Progress)
	}
	if mt.Status != int32(pb.MainTaskStatus_MAIN_TASK_CLAIMABLE) {
		t.Fatalf("expected claimable, got %d", mt.Status)
	}
}

func TestMainTaskCurrentStateRefreshesOnEvent(t *testing.T) {
	main, mt, sent := setupTestMainTask(t)
	mt.Role.Basic.Level = 1
	mt.acceptTask(gameconfig.GameConfig().TbMainTask.Get(1009))

	mt.Role.Basic.Level = 3
	main.PublishRoleEvent(event.EVENT_PLAYER_LEVEL, event.PlayerLevelEventData{
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
	if len(*sent) == 0 {
		t.Fatal("expected task update notification")
	}
	if _, ok := (*sent)[len(*sent)-1].(*pb.NotifyMainTaskUpdate); !ok {
		t.Fatalf("expected NotifyMainTaskUpdate, got %T", (*sent)[len(*sent)-1])
	}
}

func TestMainTaskOwnItemCurrentState(t *testing.T) {
	_, mt, _ := setupTestMainTask(t)
	mt.Role.Bag.Goods[10001] = bag.BagGood{GoodID: 10001, Num: 5}
	cfg := &gamecfg.GardenMainTask{
		ProgressMode: gamecfg.GardenETaskProgressMode_CURRENT_STATE,
		TargetType:   gamecfg.GardenETaskTargetType_OWN_ITEM,
		TargetParam:  10001,
		TargetNum:    3,
	}

	if got := CalcCurrentStateProgress(mt.Role, mt.Progress, cfg.TargetType, cfg.TargetParam); got != 5 {
		t.Fatalf("expected own item progress 5, got %d", got)
	}
}
