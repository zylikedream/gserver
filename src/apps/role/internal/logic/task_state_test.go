package logic

import (
	"testing"

	gamecfg "gserver/gameconfig/gosrc"
)

func TestRoleTaskStateInitSetsTaskID(t *testing.T) {
	state := RoleTaskState{}
	state.Init(RoleTaskConfig{
		ID:           1001,
		ProgressMode: gamecfg.GardenETaskProgressMode_AFTER_ACCEPT,
		TargetType:   gamecfg.GardenETaskTargetType_GET_ITEM,
		TargetParam:  1,
		TargetNum:    1,
	}, &RoleMain{})

	if state.CurrentTaskID != 1001 {
		t.Fatalf("expected task id 1001, got %d", state.CurrentTaskID)
	}
}
