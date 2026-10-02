package main

import (
	"testing"

	pb "gserver/client/pb"
)

// TestBotStateUpdates:一张表覆盖五个状态写入口——UpdateBag / UpdateBagNotify / UpdatePlots / UpdateFlowers / UpdateTask。
// 为什么需要:每个入口都要把协议包里的字段写进 BotState 的对应字段;
// 任一入口漏写或写错字段,机器人会带着半空的状态做判断,安静地跳过种植和领奖而不报错。
func TestBotStateUpdates(t *testing.T) {
	tests := []struct {
		name  string
		check func(t *testing.T, s *BotState)
	}{
		{
			name: "bag",
			check: func(t *testing.T, s *BotState) {
				s.UpdateBag(&pb.RspBagInfo{
					Goods: []*pb.PGoodInfo{
						{PropId: 1, Num: 500},
						{PropId: 101, Num: 1},
					},
				})
				if s.Inventory[1] != 500 {
					t.Errorf("gold = %d, want 500", s.Inventory[1])
				}
				if s.Inventory[101] != 1 {
					t.Errorf("seed = %d, want 1", s.Inventory[101])
				}
			},
		},
		{
			name: "bag notify",
			check: func(t *testing.T, s *BotState) {
				s.Inventory[1] = 500
				s.UpdateBagNotify(&pb.NotifyBagUpdate{
					Goods: []*pb.PBagGoodUpdate{
						{PropId: 1, PreNum: 500, Num: 580},
						{PropId: 2001, PreNum: 0, Num: 2},
					},
				})
				if s.Inventory[1] != 580 {
					t.Errorf("gold = %d, want 580", s.Inventory[1])
				}
				if s.Inventory[2001] != 2 {
					t.Errorf("soil = %d, want 2", s.Inventory[2001])
				}
			},
		},
		{
			name: "plots",
			check: func(t *testing.T, s *BotState) {
				s.UpdatePlots([]*pb.PPlotInfo{
					{PlotId: 1, State: pb.PlotState_PLOT_EMPTY},
				})
				empty := s.FindEmptyPlots()
				if len(empty) != 1 || empty[0] != 1 {
					t.Errorf("empty plots = %v, want [1]", empty)
				}

				s.UpdatePlots([]*pb.PPlotInfo{
					{PlotId: 1, State: pb.PlotState_PLOT_PLANTED, FlowerId: 101},
				})
				empty = s.FindEmptyPlots()
				if len(empty) != 0 {
					t.Errorf("empty plots = %v, want []", empty)
				}
			},
		},
		{
			name: "flowers",
			check: func(t *testing.T, s *BotState) {
				s.UpdateFlowers([]*pb.PFlowerInfo{
					{FlowerId: 101, State: pb.FlowerState_FLOWER_BREED_DONE},
				})
				if !s.IsBreedDone(101) {
					t.Error("IsBreedDone(101) = false, want true")
				}
			},
		},
		{
			name: "tasks",
			check: func(t *testing.T, s *BotState) {
				s.UpdateTask(&pb.PMainTaskInfo{
					TaskId: 1003, Status: pb.MainTaskStatus_MAIN_TASK_CLAIMABLE,
				})
				if s.MainTask.ID != 1003 || s.MainTask.Status != 1 {
					t.Error("task 1003 should be claimable (status=1)")
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.check(t, NewBotState())
		})
	}
}
