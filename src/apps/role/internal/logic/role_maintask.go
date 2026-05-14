package logic

import (
	"context"

	"gserver/src/pkg/gameconfig"
	gamecfg "gserver/gameconfig/gosrc"
	"gserver/protocol/pb"
	"gserver/src/apps/role/internal/event"
	"gserver/src/apps/role/internal/logic/bag"

	"github.com/pkg/errors"
)

var (
	ErrMainTaskNotClaimable = errors.New("main task not claimable")
	ErrMainTaskFinished     = errors.New("main task finished")
)

type RoleMainTaskState struct {
	RolePersistState
	RoleTaskState
}

func (RoleMainTaskState) TableName() string { return "role_main_task" }

type RoleMainTask struct {
	RoleModule
	RoleMainTaskState
}

var _ IRoleModule = (*RoleMainTask)(nil)

func (r *RoleMainTask) PersistState() IPersistState {
	return &r.RoleMainTaskState
}

func (r *RoleMainTask) OnCreate(ctx context.Context) {
	r.acceptTask(gameconfig.GameConfig().GetFirstMainTask())
}

func (r *RoleMainTask) OnModInit(ctx context.Context) error {
	return nil
}

func (r *RoleMainTask) OnModStart(ctx context.Context) error {
	r.subscribeEvents()
	if r.CurrentTaskID == 0 && r.Status != int32(pb.MainTaskStatus_MAIN_TASK_FINISHED) {
		r.acceptTask(gameconfig.GameConfig().GetFirstMainTask())
	}
	return nil
}

func (r *RoleMainTask) subscribeEvents() {
	if r.Role == nil {
		return
	}
	for _, eventType := range RoleTaskEventTypes() {
		r.Role.SubscribeRoleEvent(eventType, r.onRoleEvent)
	}
}

func (r *RoleMainTask) ReqMainTaskInfo(ctx context.Context, req *pb.ReqMainTaskInfo) (*pb.RspMainTaskInfo, error) {
	return &pb.RspMainTaskInfo{Task: r.PMainTaskInfo()}, nil
}

func (r *RoleMainTask) ReqMainTaskClaim(ctx context.Context, req *pb.ReqMainTaskClaim) (*pb.RspMainTaskClaim, error) {
	if r.Status == int32(pb.MainTaskStatus_MAIN_TASK_FINISHED) {
		return nil, ErrMainTaskFinished
	}
	cfg := r.currentTaskConfig()
	if cfg == nil {
		return nil, ErrMainTaskFinished
	}
	if r.Status != int32(pb.MainTaskStatus_MAIN_TASK_CLAIMABLE) {
		return nil, ErrMainTaskNotClaimable
	}
	if len(cfg.Reward) > 0 {
		if err := r.Role.Bag.SaveGoods(ctx, nil, cfg.Reward, "main_task", bag.OptNotifyReward()); err != nil {
			return nil, err
		}
	}
	claimedTask := &pb.PMainTaskInfo{
		TaskId:   cfg.Id,
		Progress: cfg.TargetNum,
		Status:   pb.MainTaskStatus_MAIN_TASK_FINISHED,
	}
	r.acceptTask(gameconfig.GameConfig().GetNextMainTask(cfg.Id))
	r.notifyMainTaskUpdate(ctx)
	return &pb.RspMainTaskClaim{Task: claimedTask}, nil
}

func (r *RoleMainTask) PMainTaskInfo() *pb.PMainTaskInfo {
	return &pb.PMainTaskInfo{
		TaskId:   r.CurrentTaskID,
		Progress: r.Progress,
		Status:   pb.MainTaskStatus(r.Status),
	}
}

func (r *RoleMainTask) onRoleEvent(ctx context.Context, param event.EventParam) {
	cfg := r.currentTaskConfig()
	if cfg == nil {
		return
	}
	state := r.snapshotState()
	if state.ApplyEvent(taskConfigFromMainTask(cfg), r.Role, param) {
		r.applyState(state)
		r.MarkDirty()
		r.notifyMainTaskUpdate(ctx)
	}
}

func (r *RoleMainTask) acceptTask(cfg *gamecfg.GardenMainTask) {
	state := r.snapshotState()
	if cfg == nil {
		state.FinishAll()
		r.applyState(state)
		r.MarkDirty()
		return
	}
	state.Init(taskConfigFromMainTask(cfg), r.Role)
	r.applyState(state)
	r.MarkDirty()
}

func (r *RoleMainTask) refreshCurrentStateTask() bool {
	cfg := r.currentTaskConfig()
	if cfg == nil {
		return false
	}
	state := r.snapshotState()
	if state.RefreshCurrentState(taskConfigFromMainTask(cfg), r.Role) {
		r.applyState(state)
		r.MarkDirty()
		return true
	}
	return false
}

func (r *RoleMainTask) currentTaskConfig() *gamecfg.GardenMainTask {
	if r.CurrentTaskID == 0 || gameconfig.GameConfig() == nil || gameconfig.GameConfig().TbMainTask == nil {
		return nil
	}
	return gameconfig.GameConfig().TbMainTask.Get(r.CurrentTaskID)
}

func (r *RoleMainTask) notifyMainTaskUpdate(ctx context.Context) {
	if r.Role == nil {
		return
	}
	r.Role.SendClient(ctx, &pb.NotifyMainTaskUpdate{Task: r.PMainTaskInfo()})
}

func (r *RoleMainTask) snapshotState() RoleTaskState {
	return RoleTaskState{
		CurrentTaskID: r.CurrentTaskID,
		Progress:      r.Progress,
		Status:        r.Status,
	}
}

func (r *RoleMainTask) applyState(state RoleTaskState) {
	r.CurrentTaskID = state.CurrentTaskID
	r.Progress = state.Progress
	r.Status = state.Status
}

func taskConfigFromMainTask(cfg *gamecfg.GardenMainTask) RoleTaskConfig {
	return RoleTaskConfig{
		ID:           cfg.Id,
		ProgressMode: cfg.ProgressMode,
		TargetType:   cfg.TargetType,
		TargetParam:  cfg.TargetParam,
		TargetNum:    cfg.TargetNum,
	}
}
