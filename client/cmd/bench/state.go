package main

import (
	pb "hy_client/pb"
)

type BotState struct {
	Inventory map[int32]int64         // prop_id -> count
	Tasks     map[int32]int32         // task_id -> status (0=progress, 1=claimable, 2=finished)
	Plots     map[int32]*PlotState    // plot_id -> plot state
	Flowers   map[int32]*FlowerState  // flower_id -> flower state
}

type PlotState struct {
	PlotID       int32
	FlowerID     int32
	State        int32  // 0=empty, 1=planted, 2=growing, 3=harvestable
	HarvestCount int32
	StateTime    int64
}

type FlowerState struct {
	FlowerID  int32
	State     int32  // 0=unlocked, 1=breeding, 2=breed_done, 3=harvested
	StateTime int64
	Level     int32
}

func NewBotState() *BotState {
	return &BotState{
		Inventory: make(map[int32]int64),
		Tasks:     make(map[int32]int32),
		Plots:     make(map[int32]*PlotState),
		Flowers:   make(map[int32]*FlowerState),
	}
}

func (s *BotState) UpdateBag(rsp *pb.RspBagInfo) {
	for _, g := range rsp.Goods {
		s.Inventory[g.PropId] = g.Num
	}
}

func (s *BotState) UpdateBagNotify(notify *pb.NotifyBagUpdate) {
	for _, g := range notify.Goods {
		if g.Num == 0 {
			delete(s.Inventory, g.PropId)
		} else {
			s.Inventory[g.PropId] = g.Num
		}
	}
}

func (s *BotState) UpdateTask(task *pb.PMainTaskInfo) {
	s.Tasks[task.TaskId] = int32(task.Status)
}

func (s *BotState) UpdatePlots(plots []*pb.PPlotInfo) {
	for _, p := range plots {
		s.Plots[p.PlotId] = &PlotState{
			PlotID:       p.PlotId,
			FlowerID:     p.FlowerId,
			State:        int32(p.State),
			HarvestCount: p.HarvestCount,
			StateTime:    p.StateTime,
		}
	}
}

func (s *BotState) FindEmptyPlots() []int32 {
	var ids []int32
	for id, p := range s.Plots {
		if p.State == 0 { // PLOT_EMPTY
			ids = append(ids, id)
		}
	}
	return ids
}

func (s *BotState) FindHarvestablePlots() []int32 {
	var ids []int32
	for id, p := range s.Plots {
		if p.State == 3 { // PLOT_HARVESTABLE
			ids = append(ids, id)
		}
	}
	return ids
}

func (s *BotState) UpdateFlowers(flowers []*pb.PFlowerInfo) {
	for _, f := range flowers {
		s.Flowers[f.FlowerId] = &FlowerState{
			FlowerID:  f.FlowerId,
			State:     int32(f.State),
			StateTime: f.StateTime,
			Level:     f.Level,
		}
	}
}

func (s *BotState) IsBreedDone(flowerID int32) bool {
	f, ok := s.Flowers[flowerID]
	return ok && f.State >= 2 // BREED_DONE or HARVESTED
}
