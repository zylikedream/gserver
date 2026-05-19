package main

import (
	"fmt"
	"math/rand"
	"time"

	"hy_client/pb"
	"hy_client/pkg/client"
	"google.golang.org/protobuf/proto"
)

type BotActions struct {
	client *client.Client
	state  *BotState
	log    *BotLogger
}

func NewBotActions(cl *client.Client, state *BotState, log *BotLogger) *BotActions {
	return &BotActions{client: cl, state: state, log: log}
}

// === Arg helpers ===

func getStringArg(args map[string]interface{}, key string) string {
	if v, ok := args[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func getIntArg(args map[string]interface{}, key string) int32 {
	if v, ok := args[key]; ok {
		switch n := v.(type) {
		case int:
			return int32(n)
		case int32:
			return n
		case float64:
			return int32(n)
		}
	}
	return 0
}

func getIntSliceArg(args map[string]interface{}, key string) []int32 {
	if v, ok := args[key]; ok {
		if list, ok := v.([]interface{}); ok {
			result := make([]int32, len(list))
			for i, item := range list {
				switch n := item.(type) {
				case int:
					result[i] = int32(n)
				case int32:
					result[i] = n
				case int64:
					result[i] = int32(n)
				case float64:
					result[i] = int32(n)
				}
			}
			return result
		}
	}
	return nil
}

func getFloatArg(args map[string]interface{}, key string) float64 {
	if v, ok := args[key]; ok {
		switch n := v.(type) {
		case float64:
			return n
		case int:
			return float64(n)
		}
	}
	return 0
}

// === Action handlers ===

func (a *BotActions) Login(args map[string]interface{}) error {
	if err := a.client.Connect(); err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	if _, err := a.client.Handshake(); err != nil {
		return fmt.Errorf("handshake: %w", err)
	}
	if _, err := a.client.Login(); err != nil {
		return fmt.Errorf("login: %w", err)
	}
	a.pullInitialState()
	return nil
}

func (a *BotActions) pullInitialState() {
	if rsp, err := a.client.RequestWithResponse(&pb.ReqBagInfo{}); err == nil {
		if bag, ok := rsp.(*pb.RspBagInfo); ok {
			a.state.UpdateBag(bag)
		}
	}
	if rsp, err := a.client.RequestWithResponse(&pb.ReqFlowerInfo{}); err == nil {
		if f, ok := rsp.(*pb.RspFlowerInfo); ok {
			a.state.UpdateFlowers(f.Flowers)
		}
	}
	if rsp, err := a.client.RequestWithResponse(&pb.ReqPlotInfo{}); err == nil {
		if p, ok := rsp.(*pb.RspPlotInfo); ok {
			a.state.UpdatePlots(p.Plots)
		}
	}
	if rsp, err := a.client.RequestWithResponse(&pb.ReqMainTaskInfo{}); err == nil {
		if t, ok := rsp.(*pb.RspMainTaskInfo); ok {
			a.state.UpdateTask(t.Task)
		}
	}
}

func (a *BotActions) Breed(args map[string]interface{}) error {
	flowerID := getIntArg(args, "flower_id")
	rsp, err := a.client.RequestWithResponse(&pb.ReqFlowerStartBreed{FlowerId: flowerID})
	if err != nil {
		return fmt.Errorf("start_breed flower=%d: %w", flowerID, err)
	}
	if f, ok := rsp.(*pb.RspFlowerStartBreed); ok {
		a.state.UpdateFlowers([]*pb.PFlowerInfo{f.Flower})
	}
	return nil
}

func (a *BotActions) WaitForBreed(args map[string]interface{}) error {
	extra := getIntArg(args, "extra_max")
	base := 10 * time.Second
	jitter := time.Duration(0)
	if extra > 0 {
		jitter = time.Duration(rand.Int63n(int64(extra)+1)) * time.Second
	}
	time.Sleep(base + jitter)
	return nil
}

func (a *BotActions) FinishBreed(args map[string]interface{}) error {
	flowerID := getIntArg(args, "flower_id")
	rsp, err := a.client.RequestWithResponse(&pb.ReqFlowerFinishBreed{FlowerId: flowerID})
	if err != nil {
		return fmt.Errorf("finish_breed flower=%d: %w", flowerID, err)
	}
	if f, ok := rsp.(*pb.RspFlowerFinishBreed); ok {
		a.state.UpdateFlowers([]*pb.PFlowerInfo{f.Flower})
	}
	return nil
}

func (a *BotActions) EnsureBreed(args map[string]interface{}) error {
	flowerID := getIntArg(args, "flower_id")
	if a.state.IsBreedDone(flowerID) {
		return nil
	}
	// If already breeding, just wait and finish
	if a.state.IsFlowerBreeding(flowerID) {
		a.log.Printf("flower %d already breeding, waiting for completion", flowerID)
		time.Sleep(10*time.Second + time.Duration(rand.Int63n(3))*time.Second)
		return a.FinishBreed(args)
	}
	if err := a.Breed(args); err != nil {
		return err
	}
	time.Sleep(10*time.Second + time.Duration(rand.Int63n(3))*time.Second)
	return a.FinishBreed(args)
}

func (a *BotActions) ClaimTask(args map[string]interface{}) error {
	_, err := a.client.RequestWithResponse(&pb.ReqMainTaskClaim{})
	if err != nil {
		return fmt.Errorf("claim_task: %w", err)
	}
	return nil
}

func (a *BotActions) Plant(args map[string]interface{}) error {
	plotIDs := getIntSliceArg(args, "plot_ids")
	flowerID := getIntArg(args, "flower_id")
	if len(plotIDs) == 0 {
		return nil
	}
	_, err := a.client.RequestWithResponse(&pb.ReqPlotPlant{PlotIds: plotIDs, FlowerId: flowerID})
	if err != nil {
		return fmt.Errorf("plant plots=%v flower=%d: %w", plotIDs, flowerID, err)
	}
	return nil
}

func (a *BotActions) Water(args map[string]interface{}) error {
	plotIDs := getIntSliceArg(args, "plot_ids")
	if len(plotIDs) == 0 {
		return nil
	}
	_, err := a.client.RequestWithResponse(&pb.ReqPlotWater{PlotIds: plotIDs})
	if err != nil {
		return fmt.Errorf("water plots=%v: %w", plotIDs, err)
	}
	return nil
}

func (a *BotActions) WaitForHarvest(args map[string]interface{}) error {
	extra := getIntArg(args, "extra_max")
	base := 10 * time.Second
	jitter := time.Duration(0)
	if extra > 0 {
		jitter = time.Duration(rand.Int63n(int64(extra)+1)) * time.Second
	}
	time.Sleep(base + jitter)
	return nil
}

func (a *BotActions) Harvest(args map[string]interface{}) error {
	plotIDs := getIntSliceArg(args, "plot_ids")
	if len(plotIDs) == 0 {
		return nil
	}
	rsp, err := a.client.RequestWithResponse(&pb.ReqPlotHarvest{PlotIds: plotIDs})
	if err != nil {
		return fmt.Errorf("harvest plots=%v: %w", plotIDs, err)
	}
	if h, ok := rsp.(*pb.RspPlotHarvest); ok {
		a.state.UpdatePlots(h.Plots)
	}
	return nil
}

func (a *BotActions) PlantCycle(args map[string]interface{}) error {
	plotMax := int(getIntArg(args, "plot_max"))
	if plotMax <= 0 {
		plotMax = 1
	}

	harvestable := a.state.FindHarvestablePlots()
	for i := 0; i < len(harvestable) && i < plotMax; i++ {
		a.Harvest(map[string]interface{}{"plot_ids": []interface{}{harvestable[i]}})
	}

	empty := a.state.FindEmptyPlots()
	planted := 0
	for _, pid := range empty {
		if planted >= plotMax {
			break
		}
		err := a.Plant(map[string]interface{}{
			"plot_ids":  []interface{}{pid},
			"flower_id": 101,
		})
		if err != nil {
			continue
		}
		planted++
	}

	for i := 0; i < planted && i < len(empty); i++ {
		a.Water(map[string]interface{}{"plot_ids": []interface{}{empty[i]}})
	}

	return nil
}

func (a *BotActions) CheckOrders(args map[string]interface{}) error {
	_, err := a.client.RequestWithResponse(&pb.ReqResidentOrderInfo{})
	return err
}

func (a *BotActions) SubmitOrders(args map[string]interface{}) error {
	rsp, err := a.client.RequestWithResponse(&pb.ReqResidentOrderInfo{})
	if err != nil {
		return fmt.Errorf("check_orders: %w", err)
	}
	orderRsp, ok := rsp.(*pb.RspResidentOrderInfo)
	if !ok {
		return nil
	}
	for _, slot := range orderRsp.Slots {
		if slot.CoolDownEnd > time.Now().Unix() {
			continue
		}
		affordable := true
		for _, demand := range slot.Demands {
			if a.state.Inventory[demand.PropId] < demand.Num {
				affordable = false
				break
			}
		}
		if !affordable {
			continue
		}
		_, err := a.client.RequestWithResponse(&pb.ReqResidentOrderSubmit{SlotId: slot.SlotId})
		if err != nil {
			a.log.Printf("submit_order slot=%d: %v", slot.SlotId, err)
		}
	}
	return nil
}

func (a *BotActions) WaitRange(args map[string]interface{}) error {
	min := getFloatArg(args, "min")
	max := getFloatArg(args, "max")
	if max <= min {
		time.Sleep(time.Duration(min) * time.Second)
		return nil
	}
	d := time.Duration((min + rand.Float64()*(max-min)) * float64(time.Second))
	time.Sleep(d)
	return nil
}

func (a *BotActions) RegisterOnMessage() {
	a.client.OnMessage(func(msg proto.Message) {
		switch m := msg.(type) {
		case *pb.NotifyBagUpdate:
			a.state.UpdateBagNotify(m)
		case *pb.NotifyMainTaskUpdate:
			a.state.UpdateTask(m.Task)
		}
	})
}
