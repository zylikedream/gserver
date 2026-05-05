package logic

import (
	gamecfg "gserver/gameconfig/gosrc"
	"gserver/protocol/pb"
	"gserver/src/apps/role/internal/event"
)

type RoleTaskState struct {
	CurrentTaskID int32 `gorm:"column:current_task_id"`
	Progress      int32 `gorm:"column:progress"`
	Status        int32 `gorm:"column:status"`
}

type RoleTaskConfig struct {
	ID           int32
	ProgressMode gamecfg.GardenETaskProgressMode
	TargetType   gamecfg.GardenETaskTargetType
	TargetParam  int32
	TargetNum    int32
}

func (s *RoleTaskState) Init(cfg RoleTaskConfig, role *RoleMain) bool {
	s.CurrentTaskID = cfg.ID
	s.Progress = 0
	s.Status = int32(pb.MainTaskStatus_MAIN_TASK_IN_PROGRESS)
	if cfg.ProgressMode == gamecfg.GardenETaskProgressMode_CURRENT_STATE {
		s.Progress = CalcCurrentStateProgress(role, s.Progress, cfg.TargetType, cfg.TargetParam)
		if s.Progress >= cfg.TargetNum {
			s.Progress = cfg.TargetNum
			s.Status = int32(pb.MainTaskStatus_MAIN_TASK_CLAIMABLE)
		}
	}
	return true
}

func (s *RoleTaskState) FinishAll() bool {
	s.CurrentTaskID = 0
	s.Progress = 0
	s.Status = int32(pb.MainTaskStatus_MAIN_TASK_FINISHED)
	return true
}

func (s *RoleTaskState) RefreshCurrentState(cfg RoleTaskConfig, role *RoleMain) bool {
	if s.Status == int32(pb.MainTaskStatus_MAIN_TASK_FINISHED) {
		return false
	}
	if cfg.ProgressMode != gamecfg.GardenETaskProgressMode_CURRENT_STATE {
		return false
	}
	progress := CalcCurrentStateProgress(role, s.Progress, cfg.TargetType, cfg.TargetParam)
	if progress > cfg.TargetNum {
		progress = cfg.TargetNum
	}
	if progress == s.Progress {
		return false
	}
	s.Progress = progress
	if s.Progress >= cfg.TargetNum {
		s.Status = int32(pb.MainTaskStatus_MAIN_TASK_CLAIMABLE)
	} else {
		s.Status = int32(pb.MainTaskStatus_MAIN_TASK_IN_PROGRESS)
	}
	return true
}

func (s *RoleTaskState) ApplyEvent(cfg RoleTaskConfig, role *RoleMain, param event.EventParam) bool {
	if s.Status != int32(pb.MainTaskStatus_MAIN_TASK_IN_PROGRESS) {
		return false
	}
	if cfg.ProgressMode == gamecfg.GardenETaskProgressMode_CURRENT_STATE {
		return s.RefreshCurrentState(cfg, role)
	}
	if cfg.ProgressMode != gamecfg.GardenETaskProgressMode_AFTER_ACCEPT {
		return false
	}
	add := CalcEventProgressAdd(role, s.Progress, cfg.TargetType, cfg.TargetParam, param)
	if add <= 0 {
		return false
	}
	return s.AddProgress(cfg.TargetNum, add)
}

func (s *RoleTaskState) AddProgress(targetNum int32, add int32) bool {
	if add <= 0 {
		return false
	}
	s.Progress += add
	if s.Progress >= targetNum {
		s.Progress = targetNum
		s.Status = int32(pb.MainTaskStatus_MAIN_TASK_CLAIMABLE)
	}
	return true
}
